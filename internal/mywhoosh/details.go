package mywhoosh

import (
	"bytes"
	"context"
)

// ActivityDetails keeps recorded FIT summaries distinct from GPX tracks/points.
// A nil Tracks pointer omits the new field entirely for existing FIT consumers.
type ActivityDetails struct {
	ActivityID string       `json:"activity_id"`
	Name       string       `json:"name"`
	Source     string       `json:"source"`
	Sessions   []FITSummary `json:"sessions"`
	Laps       []FITSummary `json:"laps"`
	Tracks     *[]GPXTrack  `json:"tracks,omitempty"`
}

// Get fetches an already-resolved activity once and reads its actual format.
func (c *Client) Get(ctx context.Context, activity Activity) (ActivityDetails, error) {
	res, err := c.openActivityFile(ctx, activity)
	if err != nil {
		return ActivityDetails{}, err
	}
	defer res.Body.Close()
	data, err := readActivityFile(ctx, res)
	if err != nil {
		return ActivityDetails{}, err
	}
	var result ActivityDetails
	switch {
	case hasGPXRoot(bytes.NewReader(data)):
		tracks, err := decodeGPX(ctx, data)
		if err != nil {
			return ActivityDetails{}, err
		}
		result = ActivityDetails{Source: "gpx", Sessions: []FITSummary{}, Laps: []FITSummary{}, Tracks: &tracks}
	case len(data) >= 12 && string(data[8:12]) == ".FIT":
		result, err = decodeFIT(ctx, data)
		if err != nil {
			return ActivityDetails{}, err
		}
	default:
		return ActivityDetails{}, unsupportedFormat("fit or gpx", "")
	}
	result.ActivityID, result.Name = activity.ID, activity.Name
	return result, nil
}
