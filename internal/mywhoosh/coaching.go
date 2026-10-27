package mywhoosh

import (
	"context"
	"encoding/json"
	"math"
	"strings"
)

// Profile and Stats contain only source-reported coaching data, never the raw account response.
type Profile struct {
	RiderID     string   `json:"rider_id"`
	DisplayName *string  `json:"display_name"`
	FTPWatts    *float64 `json:"ftp_watts"`
	HeightCM    *float64 `json:"height_cm"`
	WeightKG    *float64 `json:"weight_kg"`
}
type Stats struct {
	BestPower5SecondsWatts    *float64 `json:"best_power_5_seconds_watts"`
	BestPower30SecondsWatts   *float64 `json:"best_power_30_seconds_watts"`
	BestPower1MinuteWatts     *float64 `json:"best_power_1_minute_watts"`
	BestPower3MinutesWatts    *float64 `json:"best_power_3_minutes_watts"`
	BestPower5MinutesWatts    *float64 `json:"best_power_5_minutes_watts"`
	BestPower12MinutesWatts   *float64 `json:"best_power_12_minutes_watts"`
	BestPower20MinutesWatts   *float64 `json:"best_power_20_minutes_watts"`
	BestPower30MinutesWatts   *float64 `json:"best_power_30_minutes_watts"`
	BestPower60MinutesWatts   *float64 `json:"best_power_60_minutes_watts"`
	MaxPowerWatts             *float64 `json:"max_power_watts"`
	MaxHeartRateBPM           *float64 `json:"max_heart_rate_bpm"`
	TotalDistanceKM           *float64 `json:"total_distance_km"`
	WeeklyDistanceKM          *float64 `json:"weekly_distance_km"`
	TotalRideTimeReported     *float64 `json:"total_ride_time_reported_unknown_unit"`
	TotalElevationReported    *float64 `json:"total_elevation_reported_unknown_unit"`
	TotalCaloriesBurnReported *float64 `json:"total_calories_burn_reported_unknown_unit"`
	AveragePowerWatts         *float64 `json:"average_power_watts"`
	AverageHeartRateBPM       *float64 `json:"average_heart_rate_bpm"`
}

func (c *Client) PlayerData(ctx context.Context) (Profile, Stats, error) {
	var raw struct {
		Data json.RawMessage `json:"PlayerDataStruct"`
	}
	err := c.post(ctx, c.PlayerDataURL, c.Session.Token, struct {
		WhooshID      string `json:"WhooshId"`
		Action        int    `json:"Action"`
		CorrelationID string `json:"CorrelationId"`
		DeviceID      string `json:"DeviceId"`
		Authorization string `json:"Authorization"`
	}{c.Session.WhooshID, 1052, NewDeviceID(), c.Session.DeviceID, ""}, &raw)
	if err != nil {
		return Profile{}, Stats{}, err
	}
	var envelope struct {
		Profile  json.RawMessage `json:"PlayerProfileStruct"`
		Personal json.RawMessage `json:"PlayerPersonalStruct"`
	}
	if json.Unmarshal(raw.Data, &envelope) != nil || !object(envelope.Profile) || !object(envelope.Personal) {
		return Profile{}, Stats{}, malformed()
	}
	var profile struct {
		FullName *string  `json:"fullName"`
		First    *string  `json:"PlayerFirstName"`
		Last     *string  `json:"PlayerLastName"`
		Height   *float64 `json:"HeightCm"`
		Weight   *float64 `json:"Weight"`
	}
	var personal struct {
		FTP       *float64 `json:"FtpPlayer"`
		P5s       *float64 `json:"BestPower5Second"`
		P30s      *float64 `json:"BestPower30Second"`
		P1        *float64 `json:"BestPower1Minute"`
		P3        *float64 `json:"BestPower3Minute"`
		P5        *float64 `json:"BestPower5Minute"`
		P12       *float64 `json:"BestPower12Minute"`
		P20       *float64 `json:"BestPower20Minute"`
		P30       *float64 `json:"BestPower30Minute"`
		P60       *float64 `json:"BestPower60Minute"`
		MaxPower  *float64 `json:"MaxPower"`
		MaxHR     *float64 `json:"MaxHeartRate"`
		Distance  *float64 `json:"TotalKilometers"`
		Week      *float64 `json:"TotalKilometersInWeek"`
		RideTime  *float64 `json:"TotalRideTime"`
		Elevation *float64 `json:"TotalElevation"`
		Calories  *float64 `json:"TotalCaloriesBurn"`
		AvgPower  *float64 `json:"AveragePower"`
		AvgHR     *float64 `json:"AverageHeartRate"`
	}
	if json.Unmarshal(envelope.Profile, &profile) != nil || json.Unmarshal(envelope.Personal, &personal) != nil {
		return Profile{}, Stats{}, malformed()
	}
	for _, v := range []*float64{profile.Height, profile.Weight, personal.FTP, personal.P5s, personal.P30s, personal.P1, personal.P3, personal.P5, personal.P12, personal.P20, personal.P30, personal.P60, personal.MaxPower, personal.MaxHR, personal.Distance, personal.Week, personal.RideTime, personal.Elevation, personal.Calories, personal.AvgPower, personal.AvgHR} {
		if v != nil && (*v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0)) {
			return Profile{}, Stats{}, malformed()
		}
	}
	name := profile.FullName
	if (name == nil || strings.TrimSpace(*name) == "") && (profile.First != nil || profile.Last != nil) {
		var parts []string
		if profile.First != nil {
			parts = append(parts, *profile.First)
		}
		if profile.Last != nil {
			parts = append(parts, *profile.Last)
		}
		n := strings.TrimSpace(strings.Join(parts, " "))
		name = &n
	}
	rider := Profile{
		RiderID: c.Session.WhooshID, DisplayName: name, FTPWatts: personal.FTP,
		HeightCM: profile.Height, WeightKG: profile.Weight,
	}
	stats := Stats{
		BestPower5SecondsWatts: personal.P5s, BestPower30SecondsWatts: personal.P30s,
		BestPower1MinuteWatts: personal.P1, BestPower3MinutesWatts: personal.P3,
		BestPower5MinutesWatts: personal.P5, BestPower12MinutesWatts: personal.P12,
		BestPower20MinutesWatts: personal.P20, BestPower30MinutesWatts: personal.P30,
		BestPower60MinutesWatts: personal.P60, MaxPowerWatts: personal.MaxPower,
		MaxHeartRateBPM: personal.MaxHR, TotalDistanceKM: personal.Distance, WeeklyDistanceKM: personal.Week,
		TotalRideTimeReported: personal.RideTime, TotalElevationReported: personal.Elevation,
		TotalCaloriesBurnReported: personal.Calories, AveragePowerWatts: personal.AvgPower,
		AverageHeartRateBPM: personal.AvgHR,
	}
	return rider, stats, nil
}
func object(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil
}
