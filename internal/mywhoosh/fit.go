package mywhoosh

import (
	"bytes"
	"context"
	"encoding/binary"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
	"github.com/voydz/mywhoosh-cli/internal/fault"
)

// FITSummary contains only recorded FIT session or lap values, never estimates.
type FITSummary struct {
	StartTime          *time.Time `json:"start_time"`
	ElapsedTimeSeconds *float64   `json:"elapsed_time_seconds"`
	TimerTimeSeconds   *float64   `json:"timer_time_seconds"`
	DistanceMeters     *float64   `json:"distance_meters"`
	AscentMeters       *float64   `json:"ascent_meters"`
	DescentMeters      *float64   `json:"descent_meters"`
	AvgPowerWatts      *float64   `json:"avg_power_watts"`
	MaxPowerWatts      *float64   `json:"max_power_watts"`
	AvgHeartRateBPM    *float64   `json:"avg_heart_rate_bpm"`
	MaxHeartRateBPM    *float64   `json:"max_heart_rate_bpm"`
	AvgCadenceRPM      *float64   `json:"avg_cadence_rpm"`
	MaxCadenceRPM      *float64   `json:"max_cadence_rpm"`
	CaloriesKcal       *float64   `json:"calories_kcal"`
}

func scaled(v uint64, invalid uint64, scale float64) *float64 {
	if v == invalid {
		return nil
	}
	f := float64(v) / scale
	return &f
}
func fitTime(t time.Time) *time.Time {
	if t.IsZero() || t.Year() < 1989 {
		return nil
	}
	u := t.UTC()
	return &u
}
func summary(start time.Time, elapsed, timer, distance uint32, ascent, descent, avgPower, maxPower uint16, avgHR, maxHR, avgCadence, maxCadence uint8, calories uint16) FITSummary {
	return FITSummary{fitTime(start), scaled(uint64(elapsed), 0xffffffff, 1000), scaled(uint64(timer), 0xffffffff, 1000), scaled(uint64(distance), 0xffffffff, 100), scaled(uint64(ascent), 0xffff, 1), scaled(uint64(descent), 0xffff, 1), scaled(uint64(avgPower), 0xffff, 1), scaled(uint64(maxPower), 0xffff, 1), scaled(uint64(avgHR), 0xff, 1), scaled(uint64(maxHR), 0xff, 1), scaled(uint64(avgCadence), 0xff, 1), scaled(uint64(maxCadence), 0xff, 1), scaled(uint64(calories), 0xffff, 1)}
}

type fitCollector struct {
	result   ActivityDetails
	count    int
	exceeded bool
	cancel   context.CancelFunc
}

func (c *fitCollector) OnMesg(m proto.Message) {
	c.count++
	if c.count > 1000000 || ((m.Num == typedef.MesgNumSession || m.Num == typedef.MesgNumLap) && len(c.result.Sessions)+len(c.result.Laps) >= 10000) {
		c.exceeded = true
		c.cancel()
		return
	}
	switch m.Num {
	case typedef.MesgNumSession:
		s := mesgdef.NewSession(&m)
		c.result.Sessions = append(c.result.Sessions, summary(s.StartTime, s.TotalElapsedTime, s.TotalTimerTime, s.TotalDistance, s.TotalAscent, s.TotalDescent, s.AvgPower, s.MaxPower, s.AvgHeartRate, s.MaxHeartRate, s.AvgCadence, s.MaxCadence, s.TotalCalories))
	case typedef.MesgNumLap:
		l := mesgdef.NewLap(&m)
		c.result.Laps = append(c.result.Laps, summary(l.StartTime, l.TotalElapsedTime, l.TotalTimerTime, l.TotalDistance, l.TotalAscent, l.TotalDescent, l.AvgPower, l.MaxPower, l.AvgHeartRate, l.MaxHeartRate, l.AvgCadence, l.MaxCadence, l.TotalCalories))
	}
}
func decodeFIT(ctx context.Context, data []byte) (ActivityDetails, error) {
	bad := func() (ActivityDetails, error) {
		return ActivityDetails{}, fault.New("invalid_fit", 1, "activity FIT could not be decoded or failed integrity checks")
	}
	if len(data) < 14 || len(data) > MaxFITSize || (data[0] != 12 && data[0] != 14) || string(data[8:12]) != ".FIT" || uint64(data[0])+uint64(binary.LittleEndian.Uint32(data[4:8]))+2 != uint64(len(data)) {
		return bad()
	}
	sub, cancel := context.WithCancel(ctx)
	defer cancel()
	c := fitCollector{result: ActivityDetails{Source: "fit", Sessions: []FITSummary{}, Laps: []FITSummary{}}, cancel: cancel}
	// The decoder streams messages and validates header and file CRC by default.
	d := decoder.New(bytes.NewReader(data), decoder.WithBroadcastOnly(), decoder.WithMesgListener(&c))
	_, err := d.DecodeWithContext(sub)
	if ctx.Err() != nil {
		return ActivityDetails{}, fault.New("cancelled", 1, "request cancelled")
	}
	if err != nil || c.exceeded {
		return bad()
	}
	return c.result, nil
}
