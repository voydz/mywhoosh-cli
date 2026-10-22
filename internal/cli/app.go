package cli

import (
	"context"
	"io"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

type app struct {
	in io.Reader
	out io.Writer
	stderr io.Writer
	client *mywhoosh.Client
	getenv func(string) string
}

func (a *app) run(ctx context.Context, args []string) int { return 0 }
