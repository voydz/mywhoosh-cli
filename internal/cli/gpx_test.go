package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

// Entirely synthetic; never derived from a private activity.
const cliGPX = "\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?><gpx xmlns="http://www.topografix.com/GPX/1/1" version="1.1" creator="synthetic-test"><trk><name>Example</name><trkseg><trkpt lat="0" lon="0"><extensions><power>195</power></extensions></trkpt><trkpt lat="0" lon="0"/></trkseg><trkseg/></trk></gpx>`

func gpxCommandClient(t *testing.T, data []byte) (*mywhoosh.Client, map[string]int) {
	t.Helper()
	calls := map[string]int{}
	c := mywhoosh.New()
	c.ActivitiesURL, c.DownloadURL = "https://fixture.test/list", "https://fixture.test/download"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/list":
			return response([]byte(`{"data":{"results":[{"id":"target","name":"Synthetic Ride","activityFileId":"source-file"}],"currentPage":1,"totalPages":1}}`)), nil
		case "/download":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["fileId"] != "source-file" {
				t.Error("wrong reference")
			}
			return response([]byte(`{"data":"https://fixture.test/file?signature=private-signed-value"}`)), nil
		case "/file":
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
				t.Error("file credentials leaked")
			}
			return response(data), nil
		}
		t.Error("unexpected request")
		return response(nil), nil
	})}
	c.HTTP, c.DownloadHTTP = client, client
	return c, calls
}

func TestGPXGetRecordedPointsAndCompactTable(t *testing.T) {
	c, calls := gpxCommandClient(t, []byte(cliGPX))
	dir := savedSession(t)
	code, out, stderr := invoke(t, c, dir, "", "activities", "get", "target", "--format", "json")
	var detail struct {
		Source   string              `json:"source"`
		Sessions []any               `json:"sessions"`
		Laps     []any               `json:"laps"`
		Tracks   []mywhoosh.GPXTrack `json:"tracks"`
	}
	if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &detail) != nil || detail.Source != "gpx" || len(detail.Tracks) != 1 || detail.Sessions == nil || detail.Laps == nil || len(detail.Sessions) != 0 || len(detail.Laps) != 0 {
		t.Fatalf("details code=%d out=%q err=%q", code, out, stderr)
	}
	points := detail.Tracks[0].Segments[0].Points
	if *points[0].PowerWatts != 195 || points[1].PowerWatts != nil || calls["/file"] != 1 || calls["/download"] != 1 || calls["/list"] != 1 {
		t.Fatalf("incorrect points or repeated retrieval: %v", calls)
	}
	code, out, stderr = invoke(t, c, dir, "", "activities", "get", "target")
	if code != 0 || stderr != "" || !strings.Contains(out, "gpx") || !strings.Contains(out, "points") || !strings.Contains(out, "--format json") || strings.Count(out, "\n") > 20 {
		t.Fatalf("table: %d %q %q", code, out, stderr)
	}
}

func TestGPXDownloadExplicitOriginalAndLegacyFITRefusal(t *testing.T) {
	c, calls := gpxCommandClient(t, []byte(cliGPX))
	dir := savedSession(t)
	root := t.TempDir()
	target := filepath.Join(root, "original.GPX")
	code, out, stderr := invoke(t, c, dir, "", "activities", "download", "target", "--file-format", "gpx", "--output", target, "--format", "json")
	raw, err := os.ReadFile(target)
	var receipt map[string]any
	_ = json.Unmarshal([]byte(out), &receipt)
	info, _ := os.Stat(target)
	if code != 0 || stderr != "" || err != nil || !bytes.Equal(raw, []byte(cliGPX)) || info.Mode().Perm() != 0600 || len(receipt) != 3 || receipt["activity_id"] != "target" || receipt["bytes"] != float64(len(raw)) || calls["/file"] != 1 {
		t.Fatalf("download: %d %q %q %v", code, out, stderr, err)
	}
	code, _, _ = invoke(t, c, dir, "", "activities", "download", "target", "--file-format", "gpx", "-o", target)
	if code != 5 || calls["/file"] != 1 {
		t.Fatal("existing file fetched or clobbered")
	}
	legacy := filepath.Join(root, "legacy.fit")
	code, out, stderr = invoke(t, c, dir, "", "activities", "download", "target", "-o", legacy, "--format", "json")
	if code != 1 || out != "" || !strings.Contains(stderr, "unsupported_format") || !strings.Contains(stderr, "--file-format gpx") {
		t.Fatalf("FIT request: %d %q %q", code, out, stderr)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("GPX was published as FIT")
	}
	files, _ := filepath.Glob(filepath.Join(root, ".mywhoosh-*"))
	if len(files) != 0 {
		t.Fatal("temporary files left behind")
	}
}

func TestGPXFailureKeepsDestinationAndNoPartialJSON(t *testing.T) {
	for _, data := range [][]byte{[]byte(cliGPX + "<extra/>"), fitFixture()} {
		c, _ := gpxCommandClient(t, data)
		dir := savedSession(t)
		root := t.TempDir()
		target := filepath.Join(root, "original.gpx")
		if err := os.WriteFile(target, []byte("previous"), 0600); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := invoke(t, c, dir, "", "activities", "download", "target", "--file-format", "gpx", "--output", target, "--overwrite", "--format", "json")
		raw, _ := os.ReadFile(target)
		if code != 1 || out != "" || string(raw) != "previous" || strings.Contains(stderr, "private-signed-value") {
			t.Fatalf("unsafe failure: %d %q %q", code, out, stderr)
		}
		files, _ := filepath.Glob(filepath.Join(root, ".mywhoosh-*"))
		if len(files) != 0 {
			t.Fatal("temporary files left behind")
		}
	}
	c, _ := gpxCommandClient(t, []byte(cliGPX+"private-text"))
	code, out, stderr := invoke(t, c, savedSession(t), "", "activities", "get", "target", "--format", "json")
	if code != 1 || out != "" || !strings.Contains(stderr, "invalid_gpx") || strings.Contains(stderr, "private-text") {
		t.Fatalf("partial details: %d %q %q", code, out, stderr)
	}
}

func TestGPXInvalidPrologsReachValidation(t *testing.T) {
	for name, data := range map[string]string{
		"doctype":           strings.Replace(cliGPX, "?><gpx", "?><!DOCTYPE gpx><gpx", 1),
		"encoding":          strings.Replace(cliGPX, `encoding="UTF-8"`, `encoding="ISO-8859-1"`, 1),
		"underscore_number": strings.Replace(cliGPX, "<power>195</power>", "<power>1_95</power>", 1),
		"hex_coordinate":    strings.Replace(cliGPX, `lat="0"`, `lat="0x1p+0"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := gpxCommandClient(t, []byte(data))
			dir := savedSession(t)
			code, out, diagnostic := invoke(t, c, dir, "", "activities", "get", "target", "--format", "json")
			if code != 1 || out != "" || !strings.Contains(diagnostic, "invalid_gpx") {
				t.Fatalf("get classification: %d %q %q", code, out, diagnostic)
			}
			target := filepath.Join(t.TempDir(), "original.gpx")
			if err := os.WriteFile(target, []byte("previous"), 0600); err != nil {
				t.Fatal(err)
			}
			code, out, diagnostic = invoke(t, c, dir, "", "activities", "download", "target", "--file-format", "gpx", "--output", target, "--overwrite", "--format", "json")
			raw, _ := os.ReadFile(target)
			if code != 1 || out != "" || !strings.Contains(diagnostic, "invalid_gpx") || string(raw) != "previous" {
				t.Fatalf("download classification: %d %q %q", code, out, diagnostic)
			}
		})
	}
}

func TestGPXUsageIsCheckedBeforeNetwork(t *testing.T) {
	c, calls := gpxCommandClient(t, []byte(cliGPX))
	dir := savedSession(t)
	for _, args := range [][]string{{"--file-format", "gpx", "-o", filepath.Join(t.TempDir(), "wrong.fit")}, {"--file-format", "tcx", "-o", "ride.tcx"}} {
		full := append([]string{"activities", "download", "target"}, args...)
		code, out, _ := invoke(t, c, dir, "", full...)
		if code != 2 || out != "" || len(calls) != 0 {
			t.Fatalf("usage triggered network: %v %d", args, code)
		}
	}
}
