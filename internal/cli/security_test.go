package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthCredentialPrecedenceAndNoninteractiveFailures(t *testing.T) {
	t.Setenv("MYWHOOSH_EMAIL", "environment@example.test")
	t.Setenv("MYWHOOSH_PASSWORD", "environment-password")
	dir := filepath.Join(t.TempDir(), "config")
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p["Username"] != "explicit@example.test" || p["Password"] != "stdin-password" {
			t.Error("credential precedence changed")
		}
		_, _ = w.Write([]byte(`{"Success":true,"AccessToken":"new-token","WhooshId":"whoosh"}`))
	})
	code, out, stderr := invoke(t, c, dir, "stdin-password\n", "auth", "login", "--email", "explicit@example.test", "--password-stdin")
	if code != 0 || calls != 1 || strings.Contains(out+stderr, "stdin-password") || strings.Contains(out+stderr, "environment-password") {
		t.Fatalf("precedence %d %q %q", code, out, stderr)
	}
	code, _, _ = invoke(t, c, dir, "", "login", "--password-stdin")
	if code != 3 || calls != 1 {
		t.Fatal("empty explicit stdin must not fall back to environment")
	}
	code, out, stderr = invoke(t, c, dir, "", "login", "--password", "argv-password")
	if code != 2 || strings.Contains(out+stderr, "argv-password") || calls != 1 {
		t.Fatal("password argv was accepted or printed")
	}
	t.Setenv("MYWHOOSH_PASSWORD", "")
	code, _, _ = invoke(t, c, dir, "", "login")
	if code != 3 || calls != 1 {
		t.Fatal("noninteractive command prompted or sent empty credentials")
	}
}

func TestAuthStatusVerifiedStates(t *testing.T) {
	for _, tc := range []struct {
		status int
		state  string
		code   int
	}{{200, "valid", 0}, {401, "rejected", 3}, {503, "unknown", 1}} {
		t.Run(tc.state, func(t *testing.T) {
			dir := savedSession(t)
			calls := 0
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				if tc.status == 200 {
					_, _ = w.Write([]byte(`[]`))
				}
			})
			code, out, stderr := invoke(t, c, dir, "", "status", "--verify", "--format", "json")
			var doc struct {
				Present      bool   `json:"session_present"`
				Verification string `json:"verification"`
			}
			if code != tc.code || calls != 1 || json.Unmarshal([]byte(out), &doc) != nil || !doc.Present || doc.Verification != tc.state || strings.Contains(out+stderr, "cached-token") {
				t.Fatalf("status %d %q %q", code, out, stderr)
			}
		})
	}
}

func TestActivityUnauthorizedWithoutRecoveryCredentials(t *testing.T) {
	t.Setenv("MYWHOOSH_EMAIL", "")
	t.Setenv("MYWHOOSH_PASSWORD", "")
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(401) })
	code, out, stderr := invoke(t, c, savedSession(t), "", "activities", "--format", "json")
	if code != 3 || calls != 1 || out != "" || !strings.Contains(stderr, "auth_required") {
		t.Fatalf("unauthorized %d calls=%d %q %q", code, calls, out, stderr)
	}
}

func TestDownloadAtomicFailureCleanupAndConcurrentNoClobber(t *testing.T) {
	for _, kind := range []string{"invalid", "concurrent_target", "cancelled", "missing_parent"} {
		t.Run(kind, func(t *testing.T) {
			dir := savedSession(t)
			outputDir := t.TempDir()
			target := filepath.Join(outputDir, "ride.fit")
			original := []byte("existing-good-file")
			if kind == "invalid" {
				if err := os.WriteFile(target, original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "missing_parent" {
				target = filepath.Join(outputDir, "absent", "ride.fit")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var signedURL string
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/rider/profile/activities":
					_, _ = w.Write([]byte(`[{"id":"a","activityFileId":"separate-file"}]`))
				case "/v2/rider/profile/download-activity-file":
					_ = json.NewEncoder(w).Encode(map[string]string{"data": signedURL})
				case "/signed":
					if kind == "concurrent_target" {
						if err := os.WriteFile(target, original, 0600); err != nil {
							t.Error(err)
						}
					}
					if kind == "invalid" {
						_, _ = w.Write([]byte("<html>private-signature</html>"))
						return
					}
					if kind == "cancelled" {
						cancel()
						return
					}
					_, _ = w.Write(fitFixture())
				}
			})
			signedURL = strings.TrimSuffix(c.LoginURL, "/http-service/api/login") + "/signed?signature=private-signature"
			var out, stderr bytes.Buffer
			a := app{in: strings.NewReader(""), out: &out, stderr: &stderr, client: c, getenv: os.Getenv}
			args := []string{"--config-dir", dir, "activities", "download", "a", "-o", target, "--format", "json"}
			if kind == "invalid" {
				args = append(args, "--overwrite")
			}
			code := a.run(ctx, args)
			want := 1
			if kind == "concurrent_target" {
				want = 5
			}
			if code != want || out.Len() != 0 || strings.Contains(stderr.String(), "private-signature") {
				t.Fatalf("failure %d %q %q", code, out.String(), stderr.String())
			}
			got, err := os.ReadFile(target)
			if kind == "invalid" || kind == "concurrent_target" {
				if err != nil || !bytes.Equal(got, original) {
					t.Fatal("failed download damaged existing target")
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("failed download published target")
			}
			entries, err := os.ReadDir(outputDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".mywhoosh-") {
					t.Fatal("failed download left temporary file")
				}
			}
		})
	}
}
