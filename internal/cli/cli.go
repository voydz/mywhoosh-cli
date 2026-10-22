// Package cli provides the command boundary. Only main calls os.Exit.
package cli

import (
	"context"
	"io"
	"os"

	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

// Version is set by release builds using -ldflags -X.
var Version = "dev"

func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	a := app{in: in, out: out, stderr: stderr, client: mywhoosh.New(), getenv: os.Getenv}
	return a.run(ctx, args)
}
