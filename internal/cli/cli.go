package cli

import (
	"context"
	"fmt"
	"io"
)

var Version = "dev"

func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "mywhoosh: auth login|status|logout; activities list|download; version")
		return 0
	}
	if args[0] == "version" {
		fmt.Fprintln(out, "mywhoosh", Version)
		return 0
	}
	fmt.Fprintln(stderr, "usage_error: unknown command; use mywhoosh --help")
	return 2
}
