package mywhoosh

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/voydz/mywhoosh-cli/internal/fault"
)

const (
	gpxNamespace              = "http://www.topografix.com/GPX/1/1"
	garminTrackPointNamespace = "http://www.garmin.com/xmlschemas/TrackPointExtension/v1"
	maxGPXPoints              = 100_000
	maxGPXContainers          = 10_000
	maxGPXDepth               = 64
)

// GPX types expose recorded fields only. Segments are not FIT laps or sessions.
type GPXTrack struct {
	Name     *string      `json:"name"`
	Segments []GPXSegment `json:"segments"`
}
type GPXSegment struct {
	Points []GPXPoint `json:"points"`
}
type GPXPoint struct {
	Timestamp        *time.Time `json:"timestamp"`
	LatitudeDegrees  *float64   `json:"latitude_degrees"`
	LongitudeDegrees *float64   `json:"longitude_degrees"`
	ElevationMeters  *float64   `json:"elevation_meters"`
	PowerWatts       *float64   `json:"power_watts"`
	HeartRateBPM     *float64   `json:"heart_rate_bpm"`
	CadenceRPM       *float64   `json:"cadence_rpm"`
}

// hasGPXRoot identifies the XML root, not a MIME type or an arbitrary tag in text.
// Callers still validate the complete document before accepting GPX content.
func hasGPXRoot(r io.Reader) bool {
	d := xml.NewDecoder(r)
	// Recognition only: inspect an ASCII-compatible root even when the
	// declaration names an unsupported encoding. decodeGPX remains strict
	// and rejects that declaration before any content is accepted.
	d.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	first := true
	for {
		token, err := d.Token()
		if err != nil {
			return false
		}
		switch t := token.(type) {
		case xml.StartElement:
			return t.Name == (xml.Name{Space: gpxNamespace, Local: "gpx"})
		case xml.CharData:
			text := string(t)
			if first {
				text = strings.TrimPrefix(text, "\ufeff")
			}
			if strings.TrimSpace(text) != "" {
				return false
			}
		}
		first = false
	}
}

func unsupportedFormat(expected, actual string) error {
	message := "activity file format is unsupported; no output published"
	if actual == "gpx" && expected == "fit" {
		message = "MyWhoosh returned GPX, not FIT; use --file-format gpx with a .gpx destination; no output published"
	}
	return fault.New("unsupported_format", 1, message)
}
func invalidGPX() error {
	return fault.New("invalid_gpx", 1, "activity GPX is invalid or exceeds supported parsing limits; no output published")
}

type gpxElement struct {
	name   xml.Name
	field  string
	text   []byte
	nested bool
}

func gpxCorePath(path []gpxElement, names ...string) bool {
	if len(path) != len(names) {
		return false
	}
	for i, name := range names {
		if path[i].name != (xml.Name{Space: gpxNamespace, Local: name}) {
			return false
		}
	}
	return true
}

func gpxField(path []gpxElement) string {
	n := len(path)
	if n == 3 && gpxCorePath(path, "gpx", "trk", "name") {
		return "name"
	}
	if n == 5 && gpxCorePath(path[:4], "gpx", "trk", "trkseg", "trkpt") && path[4].name.Space == gpxNamespace {
		switch path[4].name.Local {
		case "time", "ele":
			return path[4].name.Local
		}
	}
	if n == 6 && gpxCorePath(path, "gpx", "trk", "trkseg", "trkpt", "extensions", "power") {
		return "power"
	}
	if n == 7 && gpxCorePath(path[:5], "gpx", "trk", "trkseg", "trkpt", "extensions") && path[5].name == (xml.Name{Space: garminTrackPointNamespace, Local: "TrackPointExtension"}) && path[6].name.Space == garminTrackPointNamespace {
		switch path[6].name.Local {
		case "hr", "cad":
			return path[6].name.Local
		}
	}
	return ""
}

var gpxNumberPattern = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)

func gpxNumber(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if !gpxNumberPattern.MatchString(text) {
		return 0, false
	}
	v, err := strconv.ParseFloat(text, 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}
func gpxCoordinates(attrs []xml.Attr) (GPXPoint, bool) {
	p := GPXPoint{}
	for _, a := range attrs {
		if a.Name.Space != "" {
			continue
		}
		var slot **float64
		var bound float64
		switch a.Name.Local {
		case "lat":
			slot, bound = &p.LatitudeDegrees, 90
		case "lon":
			slot, bound = &p.LongitudeDegrees, 180
		default:
			continue
		}
		value, ok := gpxNumber(a.Value)
		if !ok || math.Abs(value) > bound || *slot != nil {
			return GPXPoint{}, false
		}
		*slot = &value
	}
	return p, p.LatitudeDegrees != nil && p.LongitudeDegrees != nil
}

// time.Parse accepts some non-RFC3339 inputs; screen the lexical form first.
var gpxTimestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func setGPXField(field, text string, track *GPXTrack, p *GPXPoint) bool {
	if field == "name" {
		if track.Name != nil {
			return false
		}
		track.Name = &text
		return true
	}
	if field == "time" {
		text = strings.TrimSpace(text)
		if !gpxTimestampPattern.MatchString(text) {
			return false
		}
		value, err := time.Parse(time.RFC3339Nano, text)
		if err != nil || p.Timestamp != nil {
			return false
		}
		value = value.UTC()
		p.Timestamp = &value
		return true
	}
	var slot **float64
	switch field {
	case "ele":
		slot = &p.ElevationMeters
	case "power":
		slot = &p.PowerWatts
	case "hr":
		slot = &p.HeartRateBPM
	case "cad":
		slot = &p.CadenceRPM
	default:
		return false
	}
	value, ok := gpxNumber(text)
	if !ok || (field != "ele" && value < 0) || *slot != nil {
		return false
	}
	*slot = &value
	return true
}

// decodeGPX uses XML names and exact paths. It never fills, reorders or derives samples.
func decodeGPX(ctx context.Context, data []byte) ([]GPXTrack, error) {
	if len(data) == 0 || len(data) > MaxFITSize {
		return nil, invalidGPX()
	}
	d := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	tracks := []GPXTrack{}
	stack := []gpxElement{}
	var track *GPXTrack
	var segment *GPXSegment
	var point *GPXPoint
	points, containers := 0, 0
	seenRoot, closedRoot, seenDeclaration := false, false, false
	for {
		if ctx.Err() != nil {
			return nil, fault.New("cancelled", 1, "request cancelled")
		}
		token, err := d.Token()
		if err == io.EOF {
			if !closedRoot || len(stack) != 0 {
				return nil, invalidGPX()
			}
			return tracks, nil
		}
		if err != nil {
			return nil, invalidGPX()
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, invalidGPX()
		case xml.ProcInst:
			if strings.EqualFold(t.Target, "xml") {
				if t.Target != "xml" || seenRoot || seenDeclaration {
					return nil, invalidGPX()
				}
				seenDeclaration = true
			}
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(t)) != "" {
					return nil, invalidGPX()
				}
			} else if f := &stack[len(stack)-1]; f.field != "" {
				f.text = append(f.text, t...)
			}
		case xml.StartElement:
			if len(stack) >= maxGPXDepth {
				return nil, invalidGPX()
			}
			if len(stack) == 0 {
				if seenRoot || t.Name != (xml.Name{Space: gpxNamespace, Local: "gpx"}) {
					return nil, invalidGPX()
				}
				version := ""
				seenVersion := false
				for _, a := range t.Attr {
					if a.Name == (xml.Name{Local: "version"}) {
						if seenVersion {
							return nil, invalidGPX()
						}
						seenVersion = true
						version = a.Value
					}
				}
				if version != "1.1" {
					return nil, invalidGPX()
				}
				seenRoot = true
			} else {
				stack[len(stack)-1].nested = true
			}
			stack = append(stack, gpxElement{name: t.Name})
			switch {
			case gpxCorePath(stack, "gpx", "trk"):
				containers++
				tracks = append(tracks, GPXTrack{Segments: []GPXSegment{}})
				track = &tracks[len(tracks)-1]
			case gpxCorePath(stack, "gpx", "trk", "trkseg"):
				containers++
				track.Segments = append(track.Segments, GPXSegment{Points: []GPXPoint{}})
				segment = &track.Segments[len(track.Segments)-1]
			case gpxCorePath(stack, "gpx", "trk", "trkseg", "trkpt"):
				points++
				if points > maxGPXPoints {
					return nil, invalidGPX()
				}
				value, ok := gpxCoordinates(t.Attr)
				if !ok {
					return nil, invalidGPX()
				}
				segment.Points = append(segment.Points, value)
				point = &segment.Points[len(segment.Points)-1]
			}
			if containers > maxGPXContainers {
				return nil, invalidGPX()
			}
			stack[len(stack)-1].field = gpxField(stack)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, invalidGPX()
			}
			f := stack[len(stack)-1]
			if f.field != "" && (f.nested || !setGPXField(f.field, string(f.text), track, point)) {
				return nil, invalidGPX()
			}
			switch {
			case gpxCorePath(stack, "gpx", "trk", "trkseg", "trkpt"):
				point = nil
			case gpxCorePath(stack, "gpx", "trk", "trkseg"):
				segment = nil
			case gpxCorePath(stack, "gpx", "trk"):
				track = nil
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				closedRoot = true
			}
		}
	}
}
