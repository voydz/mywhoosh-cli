package mywhoosh

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const recordedGPX = `<gpx xmlns="http://www.topografix.com/GPX/1/1" xmlns:a="http://www.garmin.com/xmlschemas/TrackPointExtension/v1" version="1.1" creator="synthetic-test"><trk><name>Synthetic</name><trkseg><trkpt lat="0" lon="0"><time>2000-01-01T02:00:00+02:00</time><ele>-3</ele><extensions><power>0</power><a:TrackPointExtension><a:hr>0</a:hr><a:cad>55</a:cad></a:TrackPointExtension></extensions></trkpt><trkpt lat="1" lon="2"/></trkseg><trkseg/></trk></gpx>`

func TestGPXRecordedPointsPreservePresenceAndSegments(t *testing.T) {
	for _, data := range []string{recordedGPX, "\xef\xbb\xbf" + recordedGPX, strings.ReplaceAll(strings.ReplaceAll(recordedGPX, "xmlns:a=", "xmlns:other="), "a:", "other:")} {
		tracks, err := decodeGPX(context.Background(), []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if len(tracks) != 1 || tracks[0].Name == nil || *tracks[0].Name != "Synthetic" || len(tracks[0].Segments) != 2 || len(tracks[0].Segments[1].Points) != 0 {
			t.Fatalf("tracks: %+v", tracks)
		}
		points := tracks[0].Segments[0].Points
		if len(points) != 2 || points[0].Timestamp == nil || points[0].Timestamp.Format("2006-01-02T15:04:05Z07:00") != "2000-01-01T00:00:00Z" || points[0].PowerWatts == nil || *points[0].PowerWatts != 0 || points[0].HeartRateBPM == nil || *points[0].HeartRateBPM != 0 || points[0].CadenceRPM == nil || *points[0].CadenceRPM != 55 || *points[0].LatitudeDegrees != 0 || *points[0].ElevationMeters != -3 {
			t.Fatalf("points: %+v", points)
		}
		if points[1].PowerWatts != nil || points[1].HeartRateBPM != nil || points[1].CadenceRPM != nil || points[1].Timestamp != nil {
			t.Fatal("missing samples were filled")
		}
		raw, _ := json.Marshal(points[1])
		if !strings.Contains(string(raw), `"power_watts":null`) {
			t.Fatal(string(raw))
		}
	}
}

func TestGPXRejectsInvalidRecordedValuesAndDocuments(t *testing.T) {
	cases := map[string]string{
		"empty": "", "truncated": recordedGPX[:len(recordedGPX)-6],
		"second_root": recordedGPX + recordedGPX, "trailing_text": recordedGPX + "private-text",
		"doctype":                 `<!DOCTYPE gpx [<!ENTITY e "secret">]>` + recordedGPX,
		"wrong_version":           strings.Replace(recordedGPX, `version="1.1"`, `version="1.0"`, 1),
		"power_nan":               strings.Replace(recordedGPX, "<power>0</power>", "<power>NaN</power>", 1),
		"power_negative":          strings.Replace(recordedGPX, "<power>0</power>", "<power>-1</power>", 1),
		"power_empty":             strings.Replace(recordedGPX, "<power>0</power>", "<power/>", 1),
		"power_nested":            strings.Replace(recordedGPX, "<power>0</power>", "<power><value>0</value></power>", 1),
		"duplicate_power":         strings.Replace(recordedGPX, "<power>0</power>", "<power>0</power><power>1</power>", 1),
		"duplicate_power_reverse": strings.Replace(recordedGPX, "<power>0</power>", "<power>1</power><power>0</power>", 1),
		"duplicate_hr":            strings.Replace(recordedGPX, "<a:hr>0</a:hr>", "<a:hr>0</a:hr><a:hr>2</a:hr>", 1),
		"bad_cadence":             strings.Replace(recordedGPX, "<a:cad>55</a:cad>", "<a:cad>invalid</a:cad>", 1),
		"bad_latitude":            strings.Replace(recordedGPX, `lat="0"`, `lat="91"`, 1),
		"missing_longitude":       strings.Replace(recordedGPX, ` lon="0"`, "", 1),
		"nonfinite_elevation":     strings.Replace(recordedGPX, "<ele>-3</ele>", "<ele>+Inf</ele>", 1),
		"unzoned_time":            strings.Replace(recordedGPX, "2000-01-01T02:00:00+02:00", "2000-01-01T02:00:00", 1),
		"empty_time":              strings.Replace(recordedGPX, "2000-01-01T02:00:00+02:00", "", 1),
		"deep_unknown":            strings.Replace(recordedGPX, "<trk>", "<trk>"+strings.Repeat("<unknown>", 65)+strings.Repeat("</unknown>", 65), 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeGPX(context.Background(), []byte(data))
			if err == nil || !strings.Contains(err.Error(), "invalid_gpx") || strings.Contains(err.Error(), "private-text") {
				t.Fatalf("expected safe invalid_gpx, got %v", err)
			}
		})
	}
}

func TestGPXLimitsAndCancellation(t *testing.T) {
	for _, inner := range []string{`<trk><trkseg>` + strings.Repeat(`<trkpt lat="0" lon="0"/>`, 100001) + `</trkseg></trk>`, strings.Repeat(`<trk><trkseg/></trk>`, 5001)} {
		_, err := decodeGPX(context.Background(), []byte(`<gpx xmlns="http://www.topografix.com/GPX/1/1" version="1.1">`+inner+`</gpx>`))
		if err == nil {
			t.Fatal("limit ignored")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := decodeGPX(ctx, []byte(recordedGPX))
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestGPXNamespaceAndPathAreNotPrefixAliases(t *testing.T) {
	data := strings.ReplaceAll(recordedGPX, "http://www.garmin.com/xmlschemas/TrackPointExtension/v1", "urn:wrong")
	data = strings.Replace(data, "<power>0</power>", `<power xmlns="urn:wrong">9</power><other><power>2</power></other>`, 1)
	tracks, err := decodeGPX(context.Background(), []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	p := tracks[0].Segments[0].Points[0]
	if p.PowerWatts != nil || p.HeartRateBPM != nil || p.CadenceRPM != nil {
		t.Fatal("foreign URI or wrong path accepted")
	}
	data = strings.Replace(recordedGPX, `<a:TrackPointExtension><a:hr>0</a:hr><a:cad>55</a:cad></a:TrackPointExtension>`, `<TrackPointExtension xmlns="http://www.garmin.com/xmlschemas/TrackPointExtension/v1"><hr>0</hr><cad>55</cad></TrackPointExtension>`, 1)
	tracks, err = decodeGPX(context.Background(), []byte(data))
	if err != nil || tracks[0].Segments[0].Points[0].CadenceRPM == nil {
		t.Fatalf("default namespace failed: %v", err)
	}
}
