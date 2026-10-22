package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/voydz/mywhoosh-cli/internal/fault"
	"github.com/voydz/mywhoosh-cli/internal/mywhoosh"
)

func (a *app) download(ctx context.Context, args []string) error {
	f := flags("activities download")
	var output string
	f.StringVar(&output, "o", "", "destination FIT path")
	f.StringVar(&output, "output", "", "destination FIT path")
	overwrite := f.Bool("overwrite", false, "replace destination atomically")
	format := outputFormat(f)
	// flag stops at a positional argument. Deliberately support the natural
	// documented form `download ID -o PATH` as well as flags before ID.
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") { id, args = args[0], args[1:] }
	if f.Parse(args) != nil { return usage() }
	if id == "" && f.NArg() == 1 { id = f.Arg(0) } else if f.NArg() != 0 { return usage() }
	if id == "" || output == "" || !validFormat(*format) { return usage() }
	path, err := filepath.Abs(output)
	if err != nil { return outputError() }
	if _, err := os.Lstat(path); err == nil {
		if !*overwrite { return targetExists() }
	} else if !os.IsNotExist(err) { return outputError() }
	var activity mywhoosh.Activity
	err = a.authorized(ctx, func() error {
		var err error
		activity, err = a.client.Resolve(ctx, id)
		return err
	})
	if err != nil { return err }
	file, err := os.CreateTemp(filepath.Dir(path), ".mywhoosh-*.fit")
	if err != nil { return outputError() }
	defer os.Remove(file.Name())
	defer file.Close()
	var size int64
	err = a.authorized(ctx, func() error {
		if err := file.Truncate(0); err != nil { return outputError() }
		if _, err := file.Seek(0, 0); err != nil { return outputError() }
		var err error
		size, err = a.client.Download(ctx, activity, file)
		return err
	})
	if err != nil { return err }
	if ctx.Err() != nil { return fault.New("cancelled", 1, "request cancelled; no output published") }
	if file.Sync() != nil || file.Close() != nil { return outputError() }
	if *overwrite {
		if os.Rename(file.Name(), path) != nil { return outputError() }
	} else {
		// Linking a closed complete temp file is an atomic no-replace publish.
		// Unlike a prior existence check followed by rename, this cannot clobber
		// a file created concurrently after the download began.
		if err := os.Link(file.Name(), path); os.IsExist(err) { return targetExists() } else if err != nil { return outputError() }
	}
	if *format == "json" {
		return a.document(struct {
			ActivityID string `json:"activity_id"`
			Path string `json:"path"`
			Bytes int64 `json:"bytes"`
		}{id, path, size})
	}
	_, err = fmt.Fprintf(a.out, "Saved %d original FIT bytes to %s\n", size, tableText(path))
	return err
}

func targetExists() error { return fault.New("target_exists", 5, "destination exists; choose another path or use --overwrite") }
func outputError() error { return fault.New("output_failed", 1, "cannot write destination; check the existing parent directory, permissions and available space") }
