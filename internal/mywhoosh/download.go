package mywhoosh

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/voydz/mywhoosh-cli/internal/fault"
)

const MaxFITSize = 64 << 20

func secureDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

// Download streams original bytes to a caller-owned private temporary file.
// Screening checks bounds only; it does not check FIT records or CRCs.
func (c *Client) Download(ctx context.Context, activity Activity, target io.Writer) (int64, error) {
	if activity.FileRef == "" { return 0, fault.New("file_not_found", 4, "activity has no downloadable file") }
	var response struct {
		Error bool `json:"error"`
		Data string `json:"data"`
	}
	err := c.post(ctx, c.DownloadURL, c.Session.Token, struct {
		Key string `json:"key"`
		FileID string `json:"fileId"`
	}{c.Session.WhooshID, activity.FileRef}, &response)
	if err != nil { return 0, err }
	if response.Error { return 0, fault.New("download_rejected", 1, "MyWhoosh could not provide an activity file") }
	if !secureDownloadURL(response.Data) { return 0, fault.New("invalid_download_url", 1, "MyWhoosh returned an unsafe or missing download URL") }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, response.Data, nil)
	if err != nil { return 0, malformed() }
	client := *c.DownloadHTTP
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !secureDownloadURL(req.URL.String()) { return fmt.Errorf("unsafe download redirect") }
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		return nil
	}
	res, err := client.Do(req)
	if err != nil { return 0, transportError(ctx) }
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		// Signed URL rejection is not bearer-session rejection; do not relogin.
		return 0, fault.New("download_rejected", 1, fmt.Sprintf("file server returned HTTP %d; request a new download", res.StatusCode))
	}
	if res.ContentLength > MaxFITSize { return 0, invalidFIT() }
	reader := bufio.NewReader(io.LimitReader(res.Body, MaxFITSize+1))
	header, err := reader.Peek(12)
	if err != nil { return 0, invalidFIT() }
	if (header[0] != 12 && header[0] != 14) || string(header[8:12]) != ".FIT" { return 0, invalidFIT() }
	expected := int64(header[0]) + int64(binary.LittleEndian.Uint32(header[4:8])) + 2
	if expected > MaxFITSize || expected <= int64(header[0])+2 { return 0, invalidFIT() }
	n, err := io.Copy(target, reader)
	if err != nil { return 0, transportError(ctx) }
	if n != expected { return 0, invalidFIT() }
	return n, nil
}

func invalidFIT() error {
	return fault.New("invalid_fit", 1, "download failed FIT structural screening (header, declared length or 64 MiB size limit); no output published")
}
