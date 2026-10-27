// Package mywhoosh implements the source-reviewed, undocumented MyWhoosh API.
// Protocol adapted from iquesada/mywhoosh2garmin; see THIRD_PARTY_NOTICES.md.
package mywhoosh

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/voydz/mywhoosh-cli/internal/auth"
	"github.com/voydz/mywhoosh-cli/internal/fault"
)

const (
	loginURL      = "https://services.mywhoosh.com/http-service/api/login"
	playerDataURL = "https://services.mywhoosh.com/http-service/v1/player/player-data"
	activitiesURL = "https://service14.mywhoosh.com/v2/rider/profile/activities"
	downloadURL   = "https://service14.mywhoosh.com/v2/rider/profile/download-activity-file"
)

// Endpoint/client fields allow internal fixture injection only. The executable
// never accepts arbitrary endpoints or disables certificate verification.
type Client struct {
	HTTP          *http.Client
	DownloadHTTP  *http.Client
	LoginURL      string
	PlayerDataURL string
	ActivitiesURL string
	DownloadURL   string
	Session       auth.Session
}

func New() *Client {
	return &Client{
		HTTP:          &http.Client{Timeout: 60 * time.Second},
		DownloadHTTP:  &http.Client{Timeout: 120 * time.Second},
		LoginURL:      loginURL,
		PlayerDataURL: playerDataURL,
		ActivitiesURL: activitiesURL,
		DownloadURL:   downloadURL,
	}
}

var ErrUnauthorized = fault.New("auth_required", 3, "authentication rejected; run mywhoosh login")

func NewDeviceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand is guaranteed on supported Go platforms.
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func (c *Client) Login(ctx context.Context, email, password, deviceID string) (auth.Session, error) {
	var response struct {
		Success     bool            `json:"Success"`
		AccessToken string          `json:"AccessToken"`
		WhooshID    json.RawMessage `json:"WhooshId"`
	}
	payload := struct {
		Username      string `json:"Username"`
		Password      string `json:"Password"`
		Platform      string `json:"Platform"`
		Action        int    `json:"Action"`
		CorrelationID string `json:"CorrelationId"`
		DeviceID      string `json:"DeviceId"`
		Authorization string `json:"Authorization"`
	}{email, password, "Android", 1001, NewDeviceID(), deviceID, ""}
	if err := c.post(ctx, c.LoginURL, "", payload, &response); err != nil {
		return auth.Session{}, err
	}
	if !response.Success {
		return auth.Session{}, ErrUnauthorized
	}
	id := scalar(response.WhooshID)
	if response.AccessToken == "" || id == "" {
		return auth.Session{}, malformed()
	}
	return auth.Session{Version: 1, Token: response.AccessToken, WhooshID: id, DeviceID: deviceID}, nil
}

func malformed() error {
	return fault.New("invalid_response", 1, "MyWhoosh returned an unexpected response; the private API may have changed")
}

func transportError(ctx context.Context) error {
	if ctx.Err() != nil {
		return fault.New("cancelled", 1, "request cancelled")
	}
	return fault.New("transport_error", 1, "request failed; check your connection and retry")
}

func statusError(status int) error {
	if status == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if status == http.StatusTooManyRequests {
		return fault.New("rate_limited", 1, "MyWhoosh rate limit reached; retry later")
	}
	return fault.New("upstream_error", 1, fmt.Sprintf("upstream returned HTTP %d; retry later", status))
}

// Refuse all API redirects, including same-origin redirects: private API calls
// have exact known paths, and login bodies contain credentials.
func (c *Client) post(ctx context.Context, endpoint, token string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return malformed()
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return malformed()
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return malformed()
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	client := *c.HTTP
	client.Jar = nil
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(r)
	if err != nil {
		return transportError(ctx)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return statusError(res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil {
		return transportError(ctx)
	}
	if len(body) > 4<<20 || json.Unmarshal(body, output) != nil {
		return malformed()
	}
	return nil
}

func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s // IDs are opaque: do not trim, coerce or round them.
	}
	var n json.Number
	if len(raw) != 0 && json.Unmarshal(raw, &n) == nil && string(raw) != "null" {
		return n.String()
	}
	return ""
}
