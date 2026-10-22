package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voydz/mywhoosh-cli/internal/auth"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

func fixture(t *testing.T, handler http.HandlerFunc) *mywhoosh.Client {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	t.Cleanup(s.Close)
	c := mywhoosh.New()
	c.HTTP = s.Client()
	c.DownloadHTTP = s.Client()
	c.LoginURL = s.URL + "/http-service/api/login"
	c.ActivitiesURL = s.URL + "/v2/rider/profile/activities"
	c.DownloadURL = s.URL + "/v2/rider/profile/download-activity-file"
	return c
}

func invoke(t *testing.T, c *mywhoosh.Client, dir string, input string, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	a := app{in: strings.NewReader(input), out: &out, stderr: &err, client: c, getenv: os.Getenv}
	code := a.run(context.Background(), append([]string{"--config-dir", dir}, args...))
	return code, out.String(), err.String()
}

func TestAuthLoginPersistsOnlySession(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	calls := 0
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/http-service/api/login" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected login request")
		}
		var p map[string]any
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		if p["Username"] != "rider@example.test" || p["Password"] != "secret" || p["Platform"] != "Android" || p["Action"] != float64(1001) || len(p["DeviceId"].(string)) != 36 || len(p["CorrelationId"].(string)) != 36 {
			t.Error("incorrect login protocol")
		}
		_, _ = w.Write([]byte(`{"Success":true,"AccessToken":"token-secret","WhooshId":"00042"}`))
	})
	code, out, stderr := invoke(t, c, dir, "secret\n", "login", "--email", "rider@example.test", "--password-stdin")
	if code != 0 || calls != 1 || strings.Contains(out+stderr, "secret") {
		t.Fatalf("login code=%d calls=%d out=%q err=%q", code, calls, out, stderr)
	}
	s, err := (auth.Store{Dir: dir}).Load()
	if err != nil || s.Token != "token-secret" || s.WhooshID != "00042" {
		t.Fatalf("session: %+v %v", s, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil || strings.Contains(string(b), "Password") || strings.Contains(string(b), "rider@example.test") || strings.Contains(string(b), `"secret"`) {
		t.Fatal("credentials persisted")
	}
	for path, mode := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, "session.json"): 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions %s: %v %v", path, info, err)
		}
	}
	code, out, stderr = invoke(t, c, dir, "", "auth", "status", "--format", "json")
	if code != 0 || calls != 1 || !json.Valid([]byte(out)) || strings.Contains(out+stderr, "token-secret") {
		t.Fatalf("local status: %d %q %q", code, out, stderr)
	}
	code, _, stderr = invoke(t, c, dir, "", "logout")
	if code != 0 || stderr != "" {
		t.Fatal("logout failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Fatal("logout did not remove session")
	}
}
