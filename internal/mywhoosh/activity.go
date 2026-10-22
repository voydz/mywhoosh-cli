package mywhoosh

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/voydz/mywhoosh-cli/internal/fault"
)

type Activity struct {
	ID string `json:"id"`
	Name string `json:"name"`
	StartedAt *time.Time `json:"started_at"`
	Downloadable bool `json:"downloadable"`
	FileRef string `json:"-"`
}

type Page struct {
	Activities []Activity `json:"activities"`
	Page int `json:"page"`
	NextPage *int `json:"next_page"`
	PaginationKnown bool `json:"pagination_known"`
}

func (c *Client) List(ctx context.Context, page, limit int) (Page, error) {
	if page < 1 || limit < 1 {
		return Page{}, fault.New("usage_error", 2, "page and limit must be positive")
	}
	var raw json.RawMessage
	err := c.post(ctx, c.ActivitiesURL, c.Session.Token, struct {
		Page int `json:"page"`
		Limit int `json:"limit"`
		SortDate string `json:"sortDate"`
	}{page, limit, "DESC"}, &raw)
	if err != nil { return Page{}, err }
	return parsePage(raw, page)
}

func parsePage(raw json.RawMessage, requested int) (Page, error) {
	result := Page{Activities: []Activity{}, Page: requested}
	var items []map[string]json.RawMessage
	var metadata []map[string]json.RawMessage
	current := raw
	for depth := 0; ; depth++ {
		if len(current) == 0 || string(current) == "null" || depth > 2 { return Page{}, malformed() }
		if current[0] == '[' {
			if json.Unmarshal(current, &items) != nil { return Page{}, malformed() }
			break
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(current, &obj) != nil || obj == nil { return Page{}, malformed() }
		if string(obj["error"]) == "true" || string(obj["Success"]) == "false" { return Page{}, malformed() }
		metadata = append(metadata, obj)
		if value, ok := obj["results"]; ok {
			current = value
		} else if value, ok := obj["data"]; ok {
			current = value
		} else { return Page{}, malformed() }
	}
	seen := map[string]bool{}
	for _, item := range items {
		id := first(item, "id", "_id")
		if id == "" || seen[id] { return Page{}, malformed() }
		seen[id] = true
		ref := first(item, "activityFileId")
		result.Activities = append(result.Activities, Activity{
			ID: id, Name: first(item, "name", "title"),
			StartedAt: parseTime(first(item, "date", "startTime", "createdAt", "timestamp")),
			FileRef: ref, Downloadable: ref != "",
		})
	}
	var next *int
	known := false
	setNext := func(value *int) bool {
		if known && ((next == nil) != (value == nil) || (next != nil && *next != *value)) { return false }
		next, known = value, true
		return true
	}
	for _, obj := range metadata {
		for _, key := range []string{"page", "currentPage", "current_page"} {
			if raw, ok := obj[key]; ok {
				n, err := integer(raw)
				if err != nil || n != requested { return Page{}, paginationError() }
			}
		}
		for _, key := range []string{"totalPages", "total_pages"} {
			if raw, ok := obj[key]; ok {
				n, err := integer(raw)
				if err != nil || n < 0 || (n < requested && !(n == 0 && requested == 1 && len(items) == 0)) { return Page{}, paginationError() }
				var candidate *int
				if requested < n { v := requested+1; candidate = &v }
				if !setNext(candidate) { return Page{}, paginationError() }
			}
		}
		for _, key := range []string{"hasNextPage", "hasNext"} {
			if raw, ok := obj[key]; ok {
				var more bool
				if json.Unmarshal(raw, &more) != nil || string(raw) == "null" { return Page{}, paginationError() }
				var candidate *int
				if more { v := requested+1; candidate = &v }
				if !setNext(candidate) { return Page{}, paginationError() }
			}
		}
		if raw, ok := obj["nextPage"]; ok {
			var candidate *int
			if string(raw) != "null" {
				n, err := integer(raw)
				if err != nil || n != requested+1 { return Page{}, paginationError() }
				candidate = &n
			}
			if !setNext(candidate) { return Page{}, paginationError() }
		}
	}
	if len(items) == 0 {
		if known && next != nil { return Page{}, paginationError() }
		known, next = true, nil
	}
	result.NextPage, result.PaginationKnown = next, known
	return result, nil
}

func integer(raw json.RawMessage) (int, error) { return strconv.Atoi(scalar(raw)) }

func first(item map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys { if s := scalar(item[key]); s != "" { return s } }
	return ""
}

func parseTime(s string) *time.Time {
	if s == "" { return nil }
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		var t time.Time
		if n > 1e12 { t = time.UnixMilli(n).UTC() } else { t = time.Unix(n, 0).UTC() }
		if t.Year() < 1 || t.Year() > 9999 { return nil }
		return &t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil { t = t.UTC(); return &t }
	return nil // Unzoned textual dates do not establish an instant.
}

func paginationError() error { return fault.New("invalid_pagination", 1, "MyWhoosh returned inconsistent or repeated activity pages") }

// Resolve never treats an activity ID as a file reference. In the absence of
// metadata, only an empty page confirms the end; page length is not evidence.
func (c *Client) Resolve(ctx context.Context, id string) (Activity, error) {
	seen := map[string]bool{}
	for page := 1; page <= 100; page++ {
		p, err := c.List(ctx, page, 20)
		if err != nil { return Activity{}, err }
		for _, a := range p.Activities {
			if seen[a.ID] { return Activity{}, paginationError() }
			seen[a.ID] = true
		}
		for _, a := range p.Activities {
			if a.ID == id {
				if a.FileRef == "" { return Activity{}, fault.New("file_not_found", 4, "activity has no downloadable file") }
				return a, nil
			}
		}
		if p.PaginationKnown && p.NextPage == nil { return Activity{}, fault.New("not_found", 4, "activity ID was not found") }
	}
	return Activity{}, fault.New("search_limit_reached", 6, "activity lookup reached 100 pages without a confirmed end")
}
