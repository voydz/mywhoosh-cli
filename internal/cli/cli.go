package cli

import (
	"context"
	"io"
)

var Version = "dev"

func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	return 0
}
