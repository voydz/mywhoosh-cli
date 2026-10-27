package mywhoosh

import (
	"context"
	"strings"
	"testing"
)

func TestGPXRejectsMalformedRFC3339AndDuplicateVersion(t *testing.T) {
	for _, stamp := range []string{"2000-01-01T2:00:00Z", "2000-01-01T00:00:00+24:00", "2000-01-01T00:00:00+00:60", "2000-01-01T00:00:00,5Z"} {
		t.Run(stamp, func(t *testing.T) {
			data := strings.Replace(recordedGPX, "2000-01-01T02:00:00+02:00", stamp, 1)
			if _, err := decodeGPX(context.Background(), []byte(data)); err == nil {
				t.Fatal("malformed timestamp accepted")
			}
		})
	}
	data := strings.Replace(recordedGPX, `version="1.1"`, `version="" version="1.1"`, 1)
	if _, err := decodeGPX(context.Background(), []byte(data)); err == nil {
		t.Fatal("duplicate version attribute accepted")
	}
	for _, badNumber := range []string{"1_95", "0x1p+0"} {
		bad := strings.Replace(recordedGPX, "<power>0</power>", "<power>"+badNumber+"</power>", 1)
		if _, err := decodeGPX(context.Background(), []byte(bad)); err == nil {
			t.Fatalf("non-standard float literal %q accepted", badNumber)
		}
	}
	valid := strings.Replace(recordedGPX, "<power>0</power>", "<power>195.5</power>", 1)
	tracks, err := decodeGPX(context.Background(), []byte(valid))
	if err != nil || tracks[0].Segments[0].Points[0].PowerWatts == nil || *tracks[0].Segments[0].Points[0].PowerWatts != 195.5 {
		t.Fatalf("valid decimal power rejected: %v", err)
	}
}
