package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestHelp(t *testing.T) {
	var out, err bytes.Buffer
	if code := Run(context.Background(), nil, strings.NewReader(""), &out, &err); code != 0 || !strings.Contains(out.String(), "activities") || err.Len() != 0 {
		t.Fatalf("help: code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
}

func TestVersion(t *testing.T) {
	var out, err bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, strings.NewReader(""), &out, &err); code != 0 || !strings.Contains(out.String(), Version) || err.Len() != 0 {
		t.Fatalf("version: code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
}

func TestUsage(t *testing.T) {
	var out, err bytes.Buffer
	if code := Run(context.Background(), []string{"unknown"}, strings.NewReader(""), &out, &err); code != 2 || out.Len() != 0 || !strings.Contains(err.String(), "usage_error") {
		t.Fatalf("usage: code=%d stdout=%q stderr=%q", code, out.String(), err.String())
	}
}
