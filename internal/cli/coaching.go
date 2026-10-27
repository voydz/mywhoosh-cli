package cli

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

func (a *app) coaching(ctx context.Context, command string, args []string) error {
	f := flags(command)
	format := outputFormat(f)
	if f.Parse(args) != nil || f.NArg() != 0 || !validFormat(*format) {
		return usage()
	}
	var profile mywhoosh.Profile
	var stats mywhoosh.Stats
	if err := a.authorized(ctx, func() error { var err error; profile, stats, err = a.client.PlayerData(ctx); return err }); err != nil {
		return err
	}
	var result any = profile
	if command == "stats" {
		result = stats
	}
	if *format == "json" {
		return a.document(result)
	}
	w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	printFields(w, result)
	return w.Flush()
}
func printFields(w *tabwriter.Writer, value any) {
	v := reflect.ValueOf(value)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		label := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		field := v.Field(i)
		text := "—"
		if field.Kind() == reflect.Pointer {
			if !field.IsNil() {
				field = field.Elem()
			} else {
				fmt.Fprintf(w, "%s\t%s\n", label, text)
				continue
			}
		}
		if field.Type() == reflect.TypeOf(time.Time{}) {
			text = field.Interface().(time.Time).Format(time.RFC3339)
		} else {
			text = fmt.Sprint(field.Interface())
		}
		fmt.Fprintf(w, "%s\t%s\n", label, tableText(text))
	}
}
func (a *app) get(ctx context.Context, args []string) error {
	f := flags("activities get")
	format := outputFormat(f)
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if f.Parse(args) != nil {
		return usage()
	}
	if id == "" && f.NArg() == 1 {
		id = f.Arg(0)
	} else if f.NArg() != 0 {
		return usage()
	}
	if id == "" || !validFormat(*format) {
		return usage()
	}
	var activity mywhoosh.Activity
	if err := a.authorized(ctx, func() error { var err error; activity, err = a.client.Resolve(ctx, id); return err }); err != nil {
		return err
	}
	var detail mywhoosh.ActivityDetails
	if err := a.authorized(ctx, func() error { var err error; detail, err = a.client.Get(ctx, activity); return err }); err != nil {
		return err
	}
	if *format == "json" {
		return a.document(detail)
	}
	w := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "activity_id\t%s\nname\t%s\nsource\t%s\n", tableText(detail.ActivityID), tableText(detail.Name), detail.Source)
	for i, s := range detail.Sessions {
		fmt.Fprintf(w, "\nSession %d\n", i+1)
		printFields(w, s)
	}
	for i, l := range detail.Laps {
		fmt.Fprintf(w, "\nLap %d\n", i+1)
		printFields(w, l)
	}
	return w.Flush()
}
