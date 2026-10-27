package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/voydz/mywhoosh-cli/internal/auth"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

func (a *app) authorized(ctx context.Context, call func() error) error {
	if !a.sessionLoaded {
		s, err := a.store.Load()
		if errors.Is(err, auth.ErrMissing) {
			return mywhoosh.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		a.client.Session, a.sessionLoaded = s, true
	}
	err := call()
	if !errors.Is(err, mywhoosh.ErrUnauthorized) || a.reauthenticated {
		return err
	}
	email, password := a.getenv("MYWHOOSH_EMAIL"), a.getenv("MYWHOOSH_PASSWORD")
	if email == "" || password == "" {
		return err
	}
	a.reauthenticated = true
	s, err := a.client.Login(ctx, email, password, a.client.Session.DeviceID)
	if err != nil {
		return err
	}
	if err := a.store.Save(s); err != nil {
		return err
	}
	a.client.Session = s
	return call()
}

func (a *app) activities(ctx context.Context, args []string) error {
	if len(args) != 0 && args[0] == "get" {
		return a.get(ctx, args[1:])
	}
	if len(args) != 0 && args[0] == "download" {
		return a.download(ctx, args[1:])
	}
	if len(args) != 0 && args[0] == "list" {
		args = args[1:]
	}
	f := flags("activities list")
	page := f.Int("page", 1, "API page")
	limit := f.Int("limit", 20, "requested page size")
	format := outputFormat(f)
	if f.Parse(args) != nil || f.NArg() != 0 || *page < 1 || *limit < 1 || !validFormat(*format) {
		return usage()
	}
	var result mywhoosh.Page
	err := a.authorized(ctx, func() error {
		var err error
		result, err = a.client.List(ctx, *page, *limit)
		return err
	})
	if err != nil {
		return err
	}
	if *format == "json" {
		return a.document(result)
	}
	w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID	STARTED (UTC)	NAME	FILE")
	for _, activity := range result.Activities {
		started := "—"
		if activity.StartedAt != nil {
			started = activity.StartedAt.Format(time.RFC3339)
		}
		available := "no"
		if activity.Downloadable {
			available = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", tableText(activity.ID), started, tableText(activity.Name), available)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if len(result.Activities) == 0 {
		_, err = fmt.Fprintln(a.out, "No activities.")
		return err
	}
	return nil
}

// Server-provided labels must never emit terminal controls or table delimiters.
func tableText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
