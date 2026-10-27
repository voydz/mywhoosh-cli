package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

func testTLSClient(encoded string) *http.Client {
	der, _ := base64.StdEncoding.DecodeString(encoded)
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
}

func TestExecutable(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mywhoosh")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/mywhoosh")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		name string
		args []string
		code int
		text string
	}{
		{"help", []string{"--help"}, 0, "activities"},
		{"version", []string{"version"}, 0, "mywhoosh dev"},
		{"usage", []string{"unknown"}, 2, "usage_error"},
		{"password_argv", []string{"login", "--password", "must-not-appear"}, 2, "usage_error"},
		{"missing_credentials", []string{"login"}, 3, "credentials_required"},
		{"missing_session", []string{"activities", "--format", "json"}, 3, "auth_required"},
		{"missing_profile_session", []string{"profile", "--format", "json"}, 3, "auth_required"},
		{"missing_stats_session", []string{"stats", "--format", "json"}, 3, "auth_required"},
		{"missing_get_session", []string{"activities", "get", "opaque-id", "--format", "json"}, 3, "auth_required"},
		{"missing_gpx_session", []string{"activities", "download", "opaque-id", "--file-format", "gpx", "--output", filepath.Join(t.TempDir(), "ride.gpx")}, 3, "auth_required"},
		{"invalid_gpx_suffix", []string{"activities", "download", "opaque-id", "--file-format", "gpx", "--output", "ride.fit"}, 2, "usage_error"},
		{"local_status", []string{"status", "--format", "json"}, 0, `"session_present":false`},
		{"invalid_format", []string{"activities", "--format", "csv"}, 2, "usage_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binary, append([]string{"--config-dir", filepath.Join(t.TempDir(), "config")}, tc.args...)...)
			cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}
			var out, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					code = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != tc.code || !strings.Contains(out.String()+stderr.String(), tc.text) || strings.Contains(out.String()+stderr.String(), "must-not-appear") {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
			}
			if tc.code != 0 && out.Len() != 0 {
				t.Fatal("diagnostics polluted stdout")
			}
			if tc.name == "local_status" && !json.Valid(out.Bytes()) {
				t.Fatal("invalid executable JSON")
			}
		})
	}
}

// The test executable supplies the internal transport seam, never the shipping
// binary. Production has no endpoint override environment variable or flag.
func TestFixtureSubprocess(t *testing.T) {
	if os.Getenv("MYWHOOSH_TEST_HELPER") == "1" {
		c := mywhoosh.New()
		base := os.Getenv("MYWHOOSH_TEST_ENDPOINT")
		// The parent's TLS server certificate is passed to this test-only process.
		c.HTTP = testTLSClient(os.Getenv("MYWHOOSH_TEST_CERT"))
		c.DownloadHTTP = testTLSClient(os.Getenv("MYWHOOSH_TEST_CERT"))
		c.ActivitiesURL = base + "/v2/rider/profile/activities"
		var out, stderr bytes.Buffer
		a := app{in: strings.NewReader(""), out: &out, stderr: &stderr, client: c, getenv: os.Getenv}
		code := a.run(context.Background(), []string{"--config-dir", os.Getenv("MYWHOOSH_TEST_CONFIG"), "activities", "--format", "json"})
		fmt.Fprint(os.Stdout, out.String())
		fmt.Fprint(os.Stderr, stderr.String())
		os.Exit(code)
	}
	dir := savedSession(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cached-token" {
			t.Error("subprocess session not reused")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-only","name":"Subprocess"}]}`))
	}))
	defer server.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFixtureSubprocess$")
	cmd.Env = append(os.Environ(), "MYWHOOSH_TEST_HELPER=1", "MYWHOOSH_TEST_ENDPOINT="+server.URL, "MYWHOOSH_TEST_CONFIG="+dir, "MYWHOOSH_TEST_CERT="+base64.StdEncoding.EncodeToString(server.Certificate().Raw))
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() != 0 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), "fixture-only") || strings.Contains(out.String(), "cached-token") {
		t.Fatalf("fixture subprocess: %v out=%q err=%q", err, out.String(), stderr.String())
	}
}
