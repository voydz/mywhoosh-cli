package mywhoosh

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/proto"
)

func syntheticFIT(t *testing.T) []byte {
	t.Helper()
	s := mesgdef.NewSession(nil).SetTotalTimerTime(123450).SetTotalDistance(123456).SetAvgPower(0).SetAvgHeartRate(140).SetTotalAscent(42)
	l := mesgdef.NewLap(nil).SetTotalTimerTime(2000).SetAvgCadence(80)
	file := proto.FIT{Messages: []proto.Message{s.ToMesg(nil), l.ToMesg(nil)}}
	var b bytes.Buffer
	if err := encoder.New(&b).Encode(&file); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestDecodeFITSummaryAndCRC(t *testing.T) {
	b := syntheticFIT(t)
	d, err := decodeFIT(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sessions) != 1 || len(d.Laps) != 1 || d.Sessions[0].TimerTimeSeconds == nil || *d.Sessions[0].TimerTimeSeconds != 123.45 || d.Sessions[0].DistanceMeters == nil || *d.Sessions[0].DistanceMeters != 1234.56 || d.Sessions[0].AvgPowerWatts == nil || *d.Sessions[0].AvgPowerWatts != 0 || d.Sessions[0].AvgCadenceRPM != nil || d.Laps[0].AvgCadenceRPM == nil || *d.Laps[0].AvgCadenceRPM != 80 {
		t.Fatalf("bad summary: %+v %+v", d.Sessions, d.Laps)
	}
	b[len(b)-1] ^= 1
	if _, err = decodeFIT(context.Background(), b); err == nil {
		t.Fatal("accepted bad CRC")
	}
	if _, err = decodeFIT(context.Background(), []byte("bad")); err == nil {
		t.Fatal("accepted invalid data")
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { b[12] ^= 1; return b },
		func(b []byte) []byte { return b[:len(b)-1] },
		func(b []byte) []byte { return append(b, 0) },
		func(b []byte) []byte { b[8] = 'X'; return b },
	} {
		if _, err := decodeFIT(context.Background(), mutate(syntheticFIT(t))); err == nil || strings.Contains(err.Error(), "byte pos") {
			t.Fatalf("unsafe or accepted malformed FIT: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodeFIT(ctx, syntheticFIT(t)); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancelled FIT decode: %v", err)
	}
}

func TestFITCollectorLimits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := fitCollector{cancel: cancel, count: 1000000}
	c.OnMesg(proto.Message{})
	if !c.exceeded || ctx.Err() == nil {
		t.Fatal("message limit did not stop decoding")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	c = fitCollector{cancel: cancel, result: ActivityDetails{Laps: make([]FITSummary, 10000)}}
	c.OnMesg(mesgdef.NewLap(nil).ToMesg(nil))
	if !c.exceeded || ctx.Err() == nil {
		t.Fatal("lap limit did not stop decoding")
	}
}
