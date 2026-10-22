package mywhoosh

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/voydz/mywhoosh-cli/internal/auth"
	"github.com/voydz/mywhoosh-cli/internal/fault"
)

func serverClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	c := New()
	c.HTTP, c.DownloadHTTP = s.Client(), s.Client()
	c.LoginURL, c.ActivitiesURL, c.DownloadURL = s.URL+"/login", s.URL+"/list", s.URL+"/download"
	c.Session = auth.Session{Version: 1, Token: "private-token", WhooshID: "00042", DeviceID: "device"}
	return c
}

func TestParsePageEnvelopesAndUnknownPagination(t *testing.T) {
	for _, raw := range []string{
		`[{"id":"a"}]`,
		`{"data":[{"id":"a"}]}`,
		`{"data":{"results":[{"id":"a"}]}}`,
		" \n [ {\"id\":\"a\"} ] \n ",
	} {
		p, err := parsePage(json.RawMessage(raw), 2)
		if err != nil || len(p.Activities) != 1 || p.Activities[0].ID != "a" || p.PaginationKnown || p.NextPage != nil {
			t.Fatalf("parse %q: %+v %v", raw, p, err)
		}
	}
	for _, raw := range []string{`[]`, `{"data":[]}`, `{"data":{"results":[]}}`} {
		p, err := parsePage(json.RawMessage(raw), 1)
		if err != nil || !p.PaginationKnown || p.NextPage != nil || p.Activities == nil {
			t.Fatalf("empty: %+v %v", p, err)
		}
	}
}

func TestParseRejectsMalformedResponsesAndInconsistentPagination(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"data":{}}`, `{"data":null}`, `{"data":{"results":null}}`,
		`{"data":{"results":"bad"}}`, `[{"name":"no id"}]`, `[null]`,
		`[{"id":"a"},{"id":"a"}]`, `{"error":true,"data":[]}`,
		`{"data":{"results":[{"id":"a"}],"page":2}}`,
		`{"data":{"results":[{"id":"a"}],"totalPages":"bad"}}`,
		`{"data":{"results":[],"hasNext":true}}`,
		`{"data":{"results":[{"id":"a"}],"totalPages":1,"hasNext":true}}`,
		`{"data":{"results":[{"id":"a"}],"nextPage":3}}`,
		`{"data":{"results":[{"id":"a"}],"hasNext":null}}`,
	} {
		if _, err := parsePage(json.RawMessage(raw), 1); err == nil {
			t.Errorf("accepted malformed response %s", raw)
		}
	}
}

func TestPaginationMetadata(t *testing.T) {
	for _, fields := range []string{`"totalPages":2`, `"total_pages":"2"`, `"hasNext":true`, `"hasNextPage":true`, `"nextPage":2`} {
		p, err := parsePage(json.RawMessage(`{"data":{"results":[{"id":"a"}],`+fields+`}}`), 1)
		if err != nil || !p.PaginationKnown || p.NextPage == nil || *p.NextPage != 2 {
			t.Fatalf("metadata %s: %+v %v", fields, p, err)
		}
	}
}

func TestResolveConfirmsEndOrReportsLimitsAndRepeatedPages(t *testing.T) {
	for _, kind := range []string{"end", "unknown_end", "limit", "repeated", "missing_file", "overlap"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch kind {
				case "end":
					_, _ = w.Write([]byte(`{"data":{"results":[{"id":"other"}],"hasNext":false}}`))
				case "unknown_end":
					if calls == 1 {
						_, _ = w.Write([]byte(`[{"id":"other"}]`))
					} else {
						_, _ = w.Write([]byte(`[]`))
					}
				case "limit":
					_, _ = fmt.Fprintf(w, `[{"id":"%d"}]`, calls)
				case "repeated":
					_, _ = w.Write([]byte(`[{"id":"same"}]`))
				case "missing_file":
					_, _ = w.Write([]byte(`[{"id":"target"}]`))
				case "overlap":
					_, _ = fmt.Fprintf(w, `[{"id":"same"},{"id":"%d"}]`, calls)
				}
			})
			_, err := c.Resolve(context.Background(), "target")
			wantCode, wantCalls := 4, 1
			if kind == "unknown_end" {
				wantCalls = 2
			}
			if kind == "limit" {
				wantCode, wantCalls = 6, 100
			}
			if kind == "repeated" || kind == "overlap" {
				wantCode, wantCalls = 1, 2
			}
			if fault.Exit(err) != wantCode || calls != wantCalls {
				t.Fatalf("resolve %s: calls=%d err=%v", kind, calls, err)
			}
		})
	}
}

func TestPaginationUnionHasNoLostIDs(t *testing.T) {
	seen := map[string]bool{}
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Page int `json:"page"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		_, _ = fmt.Fprintf(w, `{"data":{"results":[{"id":"%d-a"},{"id":"%d-b"}],"page":%d,"totalPages":3}}`, p.Page, p.Page, p.Page)
	})
	for page := 1; page <= 3; page++ {
		p, err := c.List(context.Background(), page, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range p.Activities {
			if seen[a.ID] {
				t.Fatal("duplicate ID")
			}
			seen[a.ID] = true
		}
	}
	if len(seen) != 6 {
		t.Fatalf("lost records: %v", seen)
	}
}

func TestLoginRejectedMalformedAndSanitized(t *testing.T) {
	for _, body := range []string{
		`{"Success":false,"Message":"private-password"}`,
		`{"Success":true,"AccessToken":"private-token"}`,
		`{"Success":true,"WhooshId":42}`,
		`garbage private-password`,
	} {
		c := serverClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		_, err := c.Login(context.Background(), "email", "private-password", "device")
		if err == nil || strings.Contains(err.Error(), "private-password") || strings.Contains(err.Error(), "private-token") {
			t.Fatalf("unsafe login error %v", err)
		}
	}
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Success":true,"AccessToken":"t","WhooshId":9007199254740993}`))
	})
	s, err := c.Login(context.Background(), "email", "password", "device")
	if err != nil || s.WhooshID != "9007199254740993" {
		t.Fatalf("numeric ID rounded: %+v %v", s, err)
	}
}

func TestTransportRejectsCredentialRedirects(t *testing.T) {
	leaked := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true; w.WriteHeader(200) }))
	defer target.Close()
	for _, status := range []int{301, 302, 307, 308} {
		c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", target.URL+"/?signature=secret")
			w.WriteHeader(status)
		})
		_, err := c.Login(context.Background(), "private-email", "private-password", "device")
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("redirect: %v", err)
		}
		_, err = c.List(context.Background(), 1, 20)
		if err == nil {
			t.Fatal("authenticated redirect accepted")
		}
	}
	if leaked {
		t.Fatal("API followed a credential redirect")
	}
}

func TestTransportStatusErrorsAndCancellation(t *testing.T) {
	for _, status := range []int{401, 404, 429, 500, 503} {
		c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("secret body"))
		})
		_, err := c.List(context.Background(), 1, 20)
		want := 1
		if status == 401 {
			want = 3
		}
		if err == nil || fault.Exit(err) != want || strings.Contains(err.Error(), "secret") {
			t.Fatalf("HTTP %d: %v", status, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled request reached server") })
	cancel()
	_, err := c.List(ctx, 1, 20)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation %v", err)
	}
	c.HTTP.Timeout = time.Millisecond
	c.ActivitiesURL = "https://127.0.0.1:1/private?signature=secret"
	_, err = c.List(context.Background(), 1, 20)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("transport leaked URL: %v", err)
	}
}

func structuralFIT() []byte {
	b := make([]byte, 20)
	b[0] = 14
	binary.LittleEndian.PutUint32(b[4:8], 4)
	copy(b[8:12], ".FIT")
	return b
}

func TestDownloadRejectsUnsafeURLsAndInvalidFIT(t *testing.T) {
	for _, kind := range []string{"http", "userinfo", "html", "xml", "truncated", "trailing", "oversize", "declared_oversize", "header_only", "expired", "error_body"} {
		t.Run(kind, func(t *testing.T) {
			var base string
			c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/download" {
					u := base + "/signed?signature=private-signature"
					if kind == "http" {
						u = "http://example.test/private"
					}
					if kind == "userinfo" {
						u = "https://user:private-password@example.test/file"
					}
					if kind == "error_body" {
						_, _ = w.Write([]byte(`{"error":true,"message":"private-signature"}`))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": u})
					return
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("download credential leak")
				}
				b := structuralFIT()
				switch kind {
				case "html":
					b = []byte("<html>private-signature</html>")
				case "xml":
					b = []byte("<?xml?><Error>private-signature</Error>")
				case "truncated":
					b = b[:19]
				case "trailing":
					b = append(b, 0)
				case "oversize":
					w.Header().Set("Content-Length", "67108865")
					return
				case "declared_oversize":
					binary.LittleEndian.PutUint32(b[4:8], MaxFITSize)
				case "header_only":
					b = b[:14]
					binary.LittleEndian.PutUint32(b[4:8], 0)
				case "expired":
					w.WriteHeader(403)
					b = []byte("private-signature")
				}
				_, _ = w.Write(b)
			})
			base = strings.TrimSuffix(c.DownloadURL, "/download")
			var out bytes.Buffer
			_, err := c.Download(context.Background(), Activity{ID: "a", FileRef: "f"}, &out)
			if err == nil || strings.Contains(err.Error(), "private-signature") || strings.Contains(err.Error(), "private-password") {
				t.Fatalf("unsafe download %s: %v", kind, err)
			}
		})
	}
}

func TestDownloadRedirectIsCredentialFree(t *testing.T) {
	data := structuralFIT()
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"Authorization", "Cookie", "Referer"} {
			if r.Header.Get(h) != "" {
				t.Errorf("redirect leaked %s", h)
			}
		}
		_, _ = w.Write(data)
	}))
	defer target.Close()
	var base string
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			_ = json.NewEncoder(w).Encode(map[string]string{"data": base + "/redirect?signature=secret"})
			return
		}
		w.Header().Set("Location", target.URL+"/final")
		w.WriteHeader(302)
	})
	base = strings.TrimSuffix(c.DownloadURL, "/download")
	// Both fixtures use httptest's shared testing certificate.
	var out bytes.Buffer
	n, err := c.Download(context.Background(), Activity{ID: "a", FileRef: "f"}, &out)
	if err != nil || n != int64(len(data)) || !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("redirect download %d %v", n, err)
	}
}
