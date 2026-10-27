package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/proto"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body []byte) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), ContentLength: int64(len(body))}
}
func TestCoachingCommandsOffline(t *testing.T) {
	var fit bytes.Buffer
	session := mesgdef.NewSession(nil).SetAvgPower(0).SetTotalTimerTime(2500)
	if err := encoder.New(&fit).Encode(&proto.FIT{Messages: []proto.Message{session.ToMesg(nil)}}); err != nil {
		t.Fatal(err)
	}
	calls := []string{}
	c := mywhoosh.New()
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.Path)
		switch r.URL.Path {
		case "/player":
			if r.Header.Get("Authorization") != "Bearer cached-token" {
				t.Error("missing bearer")
			}
			return response([]byte(`{"PlayerDataStruct":{"PlayerProfileStruct":{"fullName":"A\u001b[31m\tName","HeightCm":0,"email":"hidden"},"PlayerPersonalStruct":{"FtpPlayer":0,"TotalKilometers":12,"TotalRideTime":123}}}`)), nil
		case "/list":
			return response([]byte(`{"data":{"results":[{"id":"00target","title":"Ride\u001b[31m","activityFileId":"file"}],"totalPages":1,"currentPage":1}}`)), nil
		case "/download":
			var p map[string]string
			_ = json.NewDecoder(r.Body).Decode(&p)
			if p["fileId"] != "file" {
				t.Error("wrong file reference")
			}
			return response([]byte(`{"data":"https://fixture.test/file?secret=hidden"}`)), nil
		case "/file":
			if r.Header.Get("Authorization") != "" {
				t.Error("leaked bearer")
			}
			return response(fit.Bytes()), nil
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		return response([]byte(`{}`)), nil
	})}
	c.HTTP, c.DownloadHTTP = httpClient, httpClient
	c.PlayerDataURL, c.ActivitiesURL, c.DownloadURL = "https://fixture.test/player", "https://fixture.test/list", "https://fixture.test/download"
	dir := savedSession(t)
	for _, tc := range []struct {
		args     []string
		contains string
	}{
		{[]string{"profile", "--format", "json"}, `"ftp_watts":0`},
		{[]string{"stats", "--format", "json"}, `"total_ride_time_reported_unknown_unit":123`},
		{[]string{"profile"}, "A [31m Name"},
		{[]string{"activities", "get", "00target", "--format", "json"}, `"timer_time_seconds":2.5`},
		{[]string{"activities", "get", "00target"}, "Ride [31m"},
	} {
		code, out, stderr := invoke(t, c, dir, "", tc.args...)
		if code != 0 || stderr != "" || !strings.Contains(out, tc.contains) || strings.Contains(out, "\x1b") || strings.Contains(out, "hidden") {
			t.Fatalf("%v code=%d out=%q err=%q", tc.args, code, out, stderr)
		}
	}
	code, out, stderr := invoke(t, c, dir, "", "activities", "get", "target", "--format", "json")
	if code != 4 || out != "" || !strings.Contains(stderr, "not_found") {
		t.Fatalf("exact ID: %d %q %q", code, out, stderr)
	}
	if len(calls) != 10 {
		t.Fatalf("unexpected calls: %v", calls)
	}
}
func TestCoachingUsageAndAuth(t *testing.T) {
	c := mywhoosh.New()
	dir := filepath.Join(t.TempDir(), "config")
	for _, args := range [][]string{{"profile", "--format", "csv"}, {"stats", "extra"}, {"activities", "get"}, {"activities", "get", "a", "extra"}} {
		code, out, _ := invoke(t, c, dir, "", args...)
		if code != 2 || out != "" {
			t.Fatalf("%v: %d %q", args, code, out)
		}
	}
	for _, args := range [][]string{{"profile"}, {"stats"}, {"activities", "get", "x"}} {
		code, out, err := invoke(t, c, dir, "", args...)
		if code != 3 || out != "" || !strings.Contains(err, "auth_required") {
			t.Fatalf("%v: %d %q %q", args, code, out, err)
		}
	}
}

func TestCoachingReauthAndMalformedFIT(t *testing.T) {
	dir := savedSession(t)
	c := mywhoosh.New()
	c.PlayerDataURL, c.LoginURL = "https://fixture.test/player", "https://fixture.test/login"
	c.ActivitiesURL, c.DownloadURL = "https://fixture.test/list", "https://fixture.test/download"
	var playerCalls, loginCalls, pages int
	c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/player":
			playerCalls++
			if playerCalls == 1 {
				res := response([]byte("private response"))
				res.StatusCode = 401
				return res, nil
			}
			if r.Header.Get("Authorization") != "Bearer new-token" {
				t.Error("new session not used")
			}
			return response([]byte(`{"PlayerDataStruct":{"PlayerProfileStruct":{},"PlayerPersonalStruct":{}}}`)), nil
		case "/login":
			loginCalls++
			return response([]byte(`{"Success":true,"AccessToken":"new-token","WhooshId":"00042"}`)), nil
		case "/list":
			pages++
			if pages == 1 {
				return response([]byte(`{"data":{"results":[{"id":"other","activityFileId":"wrong"}],"currentPage":1,"totalPages":2}}`)), nil
			}
			return response([]byte(`{"data":{"results":[{"id":"target","activityFileId":"actual"}],"currentPage":2,"totalPages":2}}`)), nil
		case "/download":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["fileId"] != "actual" {
				t.Error("wrong activity file")
			}
			return response([]byte(`{"data":"https://fixture.test/file?signature=private"}`)), nil
		}
		t.Error("unexpected request")
		return response(nil), nil
	})}
	c.DownloadHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(fitFixture()), nil })}
	var out, stderr bytes.Buffer
	a := app{in: strings.NewReader(""), out: &out, stderr: &stderr, client: c, getenv: func(key string) string {
		switch key {
		case "MYWHOOSH_EMAIL":
			return "fixture@example.test"
		case "MYWHOOSH_PASSWORD":
			return "fixture-password"
		}
		return ""
	}}
	if code := a.run(context.Background(), []string{"--config-dir", dir, "profile", "--format", "json"}); code != 0 || playerCalls != 2 || loginCalls != 1 || stderr.Len() != 0 {
		t.Fatalf("reauth code=%d calls=%d/%d out=%q err=%q", code, playerCalls, loginCalls, out.String(), stderr.String())
	}
	code, stdout, diagnostic := invoke(t, c, dir, "", "activities", "get", "target", "--format", "json")
	if code != 1 || stdout != "" || !strings.Contains(diagnostic, "invalid_fit") || strings.Contains(diagnostic, "private") || pages != 2 {
		t.Fatalf("malformed FIT: code=%d pages=%d out=%q err=%q", code, pages, stdout, diagnostic)
	}
}
