package mywhoosh

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/voydz/mywhoosh-cli/internal/fault"
)

const MaxActivityFileSize = 64 << 20
const MaxFITSize = MaxActivityFileSize

func secureDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

// openActivityFile is shared by original downloads and format-specific readers.
// Its caller owns and closes the successful response body.
func (c *Client) openActivityFile(ctx context.Context, activity Activity) (*http.Response, error) {
	if activity.FileRef == "" {
		return nil, fault.New("file_not_found", 4, "activity has no downloadable file")
	}
	var response struct {
		Error bool   `json:"error"`
		Data  string `json:"data"`
	}
	err := c.post(ctx, c.DownloadURL, c.Session.Token, struct {
		Key    string `json:"key"`
		FileID string `json:"fileId"`
	}{c.Session.WhooshID, activity.FileRef}, &response)
	if err != nil {
		return nil, err
	}
	if response.Error {
		return nil, fault.New("download_rejected", 1, "MyWhoosh could not provide an activity file")
	}
	if !secureDownloadURL(response.Data) {
		return nil, fault.New("invalid_download_url", 1, "MyWhoosh returned an unsafe or missing download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, response.Data, nil)
	if err != nil {
		return nil, malformed()
	}
	client := *c.DownloadHTTP
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !secureDownloadURL(req.URL.String()) {
			return fmt.Errorf("unsafe download redirect")
		}
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		return nil
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, transportError(ctx)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		// Signed-URL failures are not bearer-session failures.
		return nil, fault.New("download_rejected", 1, fmt.Sprintf("file server returned HTTP %d; request a new download", res.StatusCode))
	}
	return res, nil
}

// Download preserves the original FIT-only contract used by existing consumers.
func (c *Client) Download(ctx context.Context, activity Activity, target io.Writer) (int64, error) {
	return c.DownloadAs(ctx, activity, target, "fit")
}

// DownloadAs validates the requested format and copies original bytes, never converting them.
// Its caller must publish the private target only after a successful return.
func (c *Client) DownloadAs(ctx context.Context, activity Activity, target io.Writer, fileFormat string) (int64, error) {
	if fileFormat != "fit" && fileFormat != "gpx" {
		return 0, unsupportedFormat(fileFormat, "")
	}
	res, err := c.openActivityFile(ctx, activity)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if fileFormat == "fit" {
		if res.ContentLength > MaxActivityFileSize {
			return 0, invalidFIT()
		}
		return copyFIT(ctx, res.Body, target)
	}
	data, err := readActivityFile(ctx, res)
	if err != nil {
		return 0, err
	}
	if !hasGPXRoot(bytes.NewReader(data)) {
		return 0, unsupportedFormat("gpx", "")
	}
	if _, err := decodeGPX(ctx, data); err != nil {
		return 0, err
	}
	if ctx.Err() != nil {
		return 0, transportError(ctx)
	}
	n, err := io.Copy(target, bytes.NewReader(data))
	if err != nil {
		return 0, fault.New("output_failed", 1, "cannot write original activity file")
	}
	return n, nil
}

func readActivityFile(ctx context.Context, res *http.Response) ([]byte, error) {
	tooLarge := func() error {
		return fault.New("file_too_large", 1, "activity file exceeds the 64 MiB limit; no output published")
	}
	if res.ContentLength > MaxActivityFileSize {
		return nil, tooLarge()
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxActivityFileSize+1))
	if ctx.Err() != nil || err != nil {
		return nil, transportError(ctx)
	}
	if len(data) > MaxActivityFileSize {
		return nil, tooLarge()
	}
	return data, nil
}

// copyFIT screens only header, declared length and size. CRCs remain the detail decoder's job.
func copyFIT(ctx context.Context, source io.Reader, target io.Writer) (int64, error) {
	reader := bufio.NewReader(io.LimitReader(source, MaxActivityFileSize+1))
	header, err := reader.Peek(12)
	if ctx.Err() != nil {
		return 0, transportError(ctx)
	}
	if err != nil {
		return 0, invalidFIT()
	}
	if (header[0] != 12 && header[0] != 14) || string(header[8:12]) != ".FIT" {
		if hasGPXRoot(reader) {
			return 0, unsupportedFormat("fit", "gpx")
		}
		return 0, invalidFIT()
	}
	expected := int64(header[0]) + int64(binary.LittleEndian.Uint32(header[4:8])) + 2
	if expected > MaxActivityFileSize || expected <= int64(header[0])+2 {
		return 0, invalidFIT()
	}
	n, err := io.Copy(target, reader)
	if err != nil || ctx.Err() != nil {
		return 0, transportError(ctx)
	}
	if n != expected {
		return 0, invalidFIT()
	}
	return n, nil
}

func invalidFIT() error {
	return fault.New("invalid_fit", 1, "download failed FIT structural screening (header, declared length or 64 MiB size limit); no output published")
}
