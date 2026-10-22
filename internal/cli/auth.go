package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/voydz/mywhoosh-cli/internal/auth"
	"github.com/voydz/mywhoosh-cli/internal/fault"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

func (a *app) login(ctx context.Context, args []string) error {
	f := flags("login")
	email := f.String("email", a.getenv("MYWHOOSH_EMAIL"), "account email")
	stdin := f.Bool("password-stdin", false, "read password from stdin")
	if f.Parse(args) != nil || f.NArg() != 0 {
		return usage()
	}
	password := a.getenv("MYWHOOSH_PASSWORD")
	input, isFile := a.in.(*os.File)
	isTerminal := a.isTerminal
	if isTerminal == nil {
		isTerminal = term.IsTerminal
	}
	readPassword := a.readPassword
	if readPassword == nil {
		readPassword = term.ReadPassword
	}
	interactive := isFile && isTerminal(int(input.Fd()))
	reader := bufio.NewReader(io.LimitReader(a.in, 4097))
	if *email == "" && interactive {
		fmt.Fprint(a.stderr, "Email: ")
		line, err := reader.ReadString('\n')
		if err != nil || len(line) > 4096 {
			return usage()
		}
		*email = strings.TrimSpace(line)
	}
	if *stdin {
		line, err := io.ReadAll(reader)
		if err != nil || len(line) > 4096 {
			return fault.New("credentials_required", 3, "cannot read password from stdin (maximum 4096 bytes)")
		}
		password = strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	} else if password == "" && interactive {
		fmt.Fprint(a.stderr, "Password: ")
		b, err := readPassword(int(input.Fd()))
		fmt.Fprintln(a.stderr)
		if err != nil {
			return fault.New("credentials_required", 3, "cannot read hidden password; use --password-stdin or protected environment variables")
		}
		password = string(b)
	}
	if *email == "" || password == "" {
		return fault.New("credentials_required", 3, "provide email and password via a terminal, --password-stdin or protected MYWHOOSH_EMAIL/MYWHOOSH_PASSWORD environment variables")
	}
	old, err := a.store.Load()
	if err != nil && !errors.Is(err, auth.ErrMissing) {
		return err
	}
	deviceID := old.DeviceID
	if deviceID == "" {
		deviceID = mywhoosh.NewDeviceID()
	}
	session, err := a.client.Login(ctx, *email, password, deviceID)
	if err != nil {
		return err
	}
	if err := a.store.Save(session); err != nil {
		return err
	}
	_, err = fmt.Fprintln(a.out, "Logged in. Session saved; password not stored.")
	return err
}
