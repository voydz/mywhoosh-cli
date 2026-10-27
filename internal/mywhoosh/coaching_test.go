package mywhoosh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/voydz/mywhoosh-cli/internal/auth"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func playerFixture(handler func(*http.Request) (int, string)) *Client {
	c := New()
	c.Session = auth.Session{Version: 1, Token: "private-token", WhooshID: "00042", DeviceID: "device"}
	c.PlayerDataURL = "https://fixture.test/player"
	c.HTTP = &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		status, body := handler(r)
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return c
}
func TestPlayerDataProfileAndStats(t *testing.T) {
	c := playerFixture(func(r *http.Request) (int, string) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing bearer")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["WhooshId"] != "00042" || body["Action"] != float64(1052) || body["DeviceId"] != "device" || body["Authorization"] != "" || body["CorrelationId"] == "" {
			t.Errorf("bad request: %v", body)
		}
		return 200, `{"PlayerDataStruct":{"PlayerProfileStruct":{"fullName":"Rider","HeightCm":0,"Weight":72.5,"email":"secret"},"PlayerPersonalStruct":{"FtpPlayer":0,"BestPower5Second":123,"TotalKilometers":10.5,"TotalRideTime":3600,"TotalElevation":0,"TotalCaloriesBurn":50,"AveragePower":100}}}`
	})
	p, s, err := c.PlayerData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.RiderID != "00042" || p.DisplayName == nil || *p.DisplayName != "Rider" || p.HeightCM == nil || *p.HeightCM != 0 || p.FTPWatts == nil || *p.FTPWatts != 0 || s.BestPower5SecondsWatts == nil || *s.BestPower5SecondsWatts != 123 || s.BestPower30SecondsWatts != nil || s.TotalDistanceKM == nil || *s.TotalDistanceKM != 10.5 || s.TotalRideTimeReported == nil || *s.TotalRideTimeReported != 3600 || s.TotalElevationReported == nil || *s.TotalElevationReported != 0 || s.TotalCaloriesBurnReported == nil || *s.TotalCaloriesBurnReported != 50 || s.AveragePowerWatts == nil || *s.AveragePowerWatts != 100 {
		t.Fatalf("bad data: %+v %+v", p, s)
	}
	b, _ := json.Marshal([]any{p, s})
	if strings.Contains(string(b), "secret") {
		t.Fatal("leaked private field")
	}
}
func TestPlayerDataRejectsInvalidEnvelopeAndValues(t *testing.T) {
	for _, body := range []string{`{}`, `{"PlayerDataStruct":null}`, `{"PlayerDataStruct":{"PlayerProfileStruct":{},"PlayerPersonalStruct":{"FtpPlayer":"bad"}}}`, `{"PlayerDataStruct":{"PlayerProfileStruct":[],"PlayerPersonalStruct":{}}}`, `{"PlayerDataStruct":{"PlayerProfileStruct":{},"PlayerPersonalStruct":{"TotalKilometers":-1}}}`} {
		c := playerFixture(func(*http.Request) (int, string) { return 200, body })
		if _, _, err := c.PlayerData(context.Background()); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	c := playerFixture(func(*http.Request) (int, string) { return 401, "private token" })
	if _, _, err := c.PlayerData(context.Background()); err != ErrUnauthorized || strings.Contains(err.Error(), "private") {
		t.Fatalf("authorization: %v", err)
	}
}
func TestPlayerDataBlankFullNameFallsBackToNameParts(t *testing.T) {
	for _, fullName := range []string{"", "  \t "} {
		t.Run(fullName, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"PlayerDataStruct": map[string]any{
					"PlayerProfileStruct": map[string]any{
						"fullName": fullName, "PlayerFirstName": "Test", "PlayerLastName": "Rider",
					},
					"PlayerPersonalStruct": map[string]any{},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			c := playerFixture(func(*http.Request) (int, string) { return 200, string(body) })
			p, _, err := c.PlayerData(context.Background())
			if err != nil || p.DisplayName == nil || *p.DisplayName != "Test Rider" {
				t.Fatalf("blank full name should fall back to name parts: %+v, %v", p, err)
			}
		})
	}
}

func TestPlayerDataOptionalValues(t *testing.T) {
	c := playerFixture(func(*http.Request) (int, string) {
		return 200, `{"PlayerDataStruct":{"PlayerProfileStruct":{"PlayerFirstName":"Test","PlayerLastName":"Rider","HeightCm":null},"PlayerPersonalStruct":{"FtpPlayer":null}}}`
	})
	p, s, err := c.PlayerData(context.Background())
	if err != nil || p.DisplayName == nil || *p.DisplayName != "Test Rider" || p.HeightCM != nil || p.FTPWatts != nil || s.TotalDistanceKM != nil {
		t.Fatalf("optional: %+v %+v %v", p, s, err)
	}
}
