package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voydz/mywhoosh-cli/internal/auth"
)

func savedSession(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	if err := (auth.Store{Dir: dir}).Save(auth.Session{Version: 1, Token: "cached-token", WhooshID: "00042", DeviceID: "stable-device"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Structural fixture only: deliberately not a semantic/CRC-valid FIT activity.
func fitFixture() []byte {
	b := make([]byte, 20)
	b[0] = 14
	b[1] = 0x20
	binary.LittleEndian.PutUint32(b[4:8], 4)
	copy(b[8:12], ".FIT")
	copy(b[14:18], []byte{9, 8, 7, 6})
	return b
}

func TestListSinglePagePreservesAllRecords(t *testing.T) {
	dir := savedSession(t)
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var p struct {
			Page  int    `json:"page"`
			Limit int    `json:"limit"`
			Sort  string `json:"sortDate"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		if r.URL.Path != "/v2/rider/profile/activities" || r.Header.Get("Authorization") != "Bearer cached-token" || p.Page != 2 || p.Limit != 1 || p.Sort != "DESC" {
			t.Error("list request differs from contract")
		}
		_, _ = w.Write([]byte(`{"data":{"results":[{"id":9007199254740993,"name":"Ride","date":"2026-10-02T20:30:00+02:00","activityFileId":"file-1"},{"_id":"00042","title":"Untimed","date":"2026-10-02T18:30:00"}],"currentPage":2,"totalPages":3}}`))
	})
	code, out, stderr := invoke(t, c, dir, "", "activities", "--page", "2", "--limit", "1", "--format", "json")
	var result struct {
		Activities []struct {
			ID           string  `json:"id"`
			StartedAt    *string `json:"started_at"`
			Downloadable bool    `json:"downloadable"`
		} `json:"activities"`
		Page  int  `json:"page"`
		Next  *int `json:"next_page"`
		Known bool `json:"pagination_known"`
	}
	if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &result) != nil || calls != 1 {
		t.Fatalf("list: %d calls=%d out=%q err=%q", code, calls, out, stderr)
	}
	if len(result.Activities) != 2 || result.Activities[0].ID != "9007199254740993" || !result.Activities[0].Downloadable || result.Activities[0].StartedAt == nil || *result.Activities[0].StartedAt != "2026-10-02T18:30:00Z" || result.Activities[1].StartedAt != nil || result.Activities[1].ID != "00042" || result.Page != 2 || result.Next == nil || *result.Next != 3 || !result.Known {
		t.Fatalf("incorrect list: %s", out)
	}
}

func TestDownloadResolvesExactIDAndPreservesOriginalBytes(t *testing.T) {
	dir := savedSession(t)
	data := fitFixture()
	pages := []int{}
	fileCalls := 0
	var signedURL string
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/rider/profile/activities":
			var p struct {
				Page int `json:"page"`
			}
			_ = json.NewDecoder(r.Body).Decode(&p)
			pages = append(pages, p.Page)
			if p.Page == 1 {
				_, _ = w.Write([]byte(`{"data":{"results":[{"id":"not-target","activityFileId":"wrong-file"}],"page":1,"totalPages":2}}`))
			} else {
				_, _ = w.Write([]byte(`{"data":{"results":[{"id":"target","name":"../../unsafe","activityFileId":"actual-file"}],"page":2,"totalPages":2}}`))
			}
		case "/v2/rider/profile/download-activity-file":
			var p map[string]string
			_ = json.NewDecoder(r.Body).Decode(&p)
			if p["fileId"] != "actual-file" || p["key"] != "00042" || r.Header.Get("Authorization") != "Bearer cached-token" {
				t.Error("download must use exact separate file reference and opaque Whoosh ID")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"error": false, "data": signedURL})
		case "/signed":
			fileCalls++
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
				t.Error("credentials leaked into signed download")
			}
			_, _ = w.Write(data)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(500)
		}
	})
	signedURL = strings.TrimSuffix(c.LoginURL, "/http-service/api/login") + "/signed?signature=private-signature"
	target := filepath.Join(t.TempDir(), "ride.fit")
	code, out, stderr := invoke(t, c, dir, "", "activities", "download", "target", "--output", target, "--format", "json")
	if code != 0 || stderr != "" || !json.Valid([]byte(out)) || fileCalls != 1 || len(pages) != 2 || pages[0] != 1 || pages[1] != 2 || strings.Contains(out, "private-signature") {
		t.Fatalf("download: %d pages=%v files=%d out=%q err=%q", code, pages, fileCalls, out, stderr)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("download changed original bytes")
	}
	var receipt struct {
		ActivityID string `json:"activity_id"`
		Path       string `json:"path"`
		Bytes      int    `json:"bytes"`
	}
	if json.Unmarshal([]byte(out), &receipt) != nil || receipt.ActivityID != "target" || receipt.Path != target || receipt.Bytes != len(data) {
		t.Fatalf("receipt %s", out)
	}
	code, _, stderr = invoke(t, c, dir, "", "activities", "download", "target", "-o", target)
	if code != 5 || !strings.Contains(stderr, "target_exists") {
		t.Fatalf("no-clobber: %d %q", code, stderr)
	}
	data = append([]byte(nil), data...)
	data[15] = 42
	code, _, stderr = invoke(t, c, dir, "", "activities", "download", "target", "-o", target, "--overwrite")
	got, err = os.ReadFile(target)
	if code != 0 || err != nil || !bytes.Equal(got, data) {
		t.Fatalf("overwrite failed: %d %q %v", code, stderr, err)
	}
}

func TestReauthenticationIsBoundedAndStatusDoesNotRelogin(t *testing.T) {
	t.Setenv("MYWHOOSH_EMAIL", "rider@example.test")
	t.Setenv("MYWHOOSH_PASSWORD", "private-password")
	for _, rejectAgain := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "reject_again"}[rejectAgain], func(t *testing.T) {
			dir := savedSession(t)
			loginCalls, listCalls := 0, 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/http-service/api/login" {
					loginCalls++
					var p map[string]any
					_ = json.NewDecoder(r.Body).Decode(&p)
					if p["DeviceId"] != "stable-device" {
						t.Error("device ID changed")
					}
					_, _ = w.Write([]byte(`{"Success":true,"AccessToken":"replacement-token","WhooshId":"00042"}`))
					return
				}
				listCalls++
				if r.Header.Get("Authorization") == "Bearer cached-token" || rejectAgain {
					w.WriteHeader(401)
					return
				}
				_, _ = w.Write([]byte(`[]`))
			})
			code, out, stderr := invoke(t, c, dir, "", "activities", "list", "--format", "json")
			want := 0
			if rejectAgain {
				want = 3
			}
			if code != want || loginCalls != 1 || listCalls != 2 || strings.Contains(out+stderr, "replacement-token") || strings.Contains(out+stderr, "private-password") {
				t.Fatalf("recovery: %d login=%d list=%d out=%q err=%q", code, loginCalls, listCalls, out, stderr)
			}
			before := loginCalls
			_, _, _ = invoke(t, c, dir, "", "status", "--verify", "--format", "json")
			if loginCalls != before {
				t.Fatal("status must not reauthenticate")
			}
		})
	}
}
