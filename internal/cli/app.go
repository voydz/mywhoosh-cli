package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/voydz/mywhoosh-cli/internal/auth"
	"github.com/voydz/mywhoosh-cli/internal/fault"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

const help = `mywhoosh — read-only MyWhoosh coaching data and original FIT/GPX downloads

Usage: mywhoosh [--config-dir PATH] COMMAND

  login [--email EMAIL] [--password-stdin]    Log in (never pass passwords as arguments)
  status [--verify] [--format table|json]     Local session status; optionally verify online
  logout                                    Remove the local session
  auth login|status|logout                   Equivalent grouped commands
  profile [--format table|json]              Read the rider profile
  stats [--format table|json]                Read source-reported training totals
  activities [list] [--page N] [--limit N] [--format table|json]
  activities get ID [--format table|json]    Read recorded FIT summaries or GPX points
  activities download ID -o PATH [--file-format fit|gpx] [--overwrite] [--format table|json]
  version

Download accepts --output as an alias for -o. Root options precede commands.
Downloads default to FIT; GPX requires --file-format gpx and a .gpx destination.
Credentials: MYWHOOSH_EMAIL and MYWHOOSH_PASSWORD; no .env files are loaded.
`

type app struct {
	in              io.Reader
	out             io.Writer
	stderr          io.Writer
	client          *mywhoosh.Client
	getenv          func(string) string
	isTerminal      func(int) bool
	readPassword    func(int) ([]byte, error)
	store           auth.Store
	sessionLoaded   bool
	reauthenticated bool
}

func usage() error {
	return fault.New("usage_error", 2, "invalid command or options; use mywhoosh --help")
}

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

func (a *app) run(ctx context.Context, args []string) int {
	err := a.execute(ctx, args)
	if err == nil {
		return 0
	}
	// Only explicitly safe application errors cross this boundary.
	var safe *fault.Error
	if errors.As(err, &safe) {
		fmt.Fprintln(a.stderr, safe)
	} else {
		fmt.Fprintln(a.stderr, "operation_failed: command failed")
	}
	return fault.Exit(err)
}

func (a *app) execute(ctx context.Context, args []string) error {
	root := flags("mywhoosh")
	dir := root.String("config-dir", a.getenv("MYWHOOSH_CONFIG_DIR"), "private session directory")
	if err := root.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, err = fmt.Fprint(a.out, help)
		return err
	} else if err != nil {
		return usage()
	}
	args = root.Args()
	if len(args) == 0 || args[0] == "help" {
		_, err := fmt.Fprint(a.out, help)
		return err
	}
	if args[0] == "version" {
		if len(args) != 1 {
			return usage()
		}
		_, err := fmt.Fprintln(a.out, "mywhoosh", Version)
		return err
	}
	if *dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return fault.New("invalid_config", 2, "cannot determine configuration directory; set --config-dir")
		}
		*dir = filepath.Join(base, "mywhoosh")
	}
	a.store = auth.Store{Dir: *dir}
	if args[0] == "auth" {
		args = args[1:]
		if len(args) == 0 || (args[0] != "login" && args[0] != "status" && args[0] != "logout") {
			return usage()
		}
	}
	switch args[0] {
	case "profile", "stats":
		return a.coaching(ctx, args[0], args[1:])
	case "activities":
		return a.activities(ctx, args[1:])
	case "login":
		return a.login(ctx, args[1:])
	case "status":
		return a.status(ctx, args[1:])
	case "logout":
		if len(args) != 1 {
			return usage()
		}
		if err := a.store.Delete(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(a.out, "Local session removed (server-side revocation is not performed).")
		return err
	default:
		return usage()
	}
}

func outputFormat(f *flag.FlagSet) *string {
	return f.String("format", "table", "table or json")
}

func validFormat(s string) bool { return s == "table" || s == "json" }

func (a *app) document(value any) error { return json.NewEncoder(a.out).Encode(value) }

func (a *app) status(ctx context.Context, args []string) error {
	f := flags("status")
	verify := f.Bool("verify", false, "check online")
	format := outputFormat(f)
	if f.Parse(args) != nil || f.NArg() != 0 || !validFormat(*format) {
		return usage()
	}
	s, err := a.store.Load()
	present := !errors.Is(err, auth.ErrMissing)
	if err != nil && present {
		return err
	}
	state := "missing"
	if present {
		state = "present"
	}
	verification := "not_checked"
	var verifyErr error
	if *verify {
		if !present {
			verification, verifyErr = "missing", mywhoosh.ErrUnauthorized
		} else {
			a.client.Session = s
			_, verifyErr = a.client.List(ctx, 1, 1)
			verification = "valid"
			if errors.Is(verifyErr, mywhoosh.ErrUnauthorized) {
				verification = "rejected"
			} else if verifyErr != nil {
				verification = "unknown"
			}
		}
	}
	if *format == "json" {
		err = a.document(struct {
			SessionPresent bool   `json:"session_present"`
			Verification   string `json:"verification"`
		}{present, verification})
	} else {
		_, err = fmt.Fprintln(a.out, "Local session:", state, "— verification:", verification)
	}
	if err != nil {
		return err
	}
	return verifyErr
}
