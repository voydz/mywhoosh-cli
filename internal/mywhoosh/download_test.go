package mywhoosh

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Synthetic public fixture: not copied from any rider's export.
const minimalGPX = `<?xml version="1.0" encoding="UTF-8"?><gpx xmlns="http://www.topografix.com/GPX/1/1" version="1.1" creator="synthetic-test"><trk><trkseg><trkpt lat="0" lon="0"/></trkseg></trk></gpx>`

func activityFileClient(t *testing.T, data []byte, mime string) *Client {
	t.Helper()
	var signed string
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download" {
			_ = json.NewEncoder(w).Encode(map[string]string{"data": signed})
			return
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("signed request leaked credentials")
		}
		w.Header().Set("Content-Type", mime)
		_, _ = w.Write(data)
	})
	signed = strings.TrimSuffix(c.DownloadURL, "/download") + "/signed?private=do-not-print"
	return c
}

func TestDownloadRecognizesGPXInsteadOfInvalidFIT(t *testing.T) {
	for _, prefix := range []string{"", "\xef\xbb\xbf"} {
		t.Run(prefix, func(t *testing.T) {
			c := activityFileClient(t, []byte(prefix+minimalGPX), "application/octet-stream")
			var out bytes.Buffer
			_, err := c.Download(context.Background(), Activity{ID: "a", FileRef: "f"}, &out)
			if err == nil || !strings.Contains(err.Error(), "unsupported_format") || !strings.Contains(err.Error(), "--file-format gpx") || out.Len() != 0 {
				t.Fatalf("expected safe GPX-specific failure, got %v, bytes=%d", err, out.Len())
			}
		})
	}
}

func TestDownloadDoesNotTrustGPXMIMEOrXMLDeclaration(t *testing.T) {
	for _, body := range []string{`<?xml version="1.0"?><Error>private</Error>`, `<html>private</html>`, `{"private":"body"}`, strings.Replace(minimalGPX, "http://www.topografix.com/GPX/1/1", "urn:wrong", 1)} {
		c := activityFileClient(t, []byte(body), "application/gpx+xml")
		var out bytes.Buffer
		_, err := c.Download(context.Background(), Activity{ID: "a", FileRef: "f"}, &out)
		if err == nil || strings.Contains(err.Error(), "--file-format gpx") || strings.Contains(err.Error(), "private") || out.Len() != 0 {
			t.Fatalf("unrelated content misclassified or leaked: %v", err)
		}
	}
}
