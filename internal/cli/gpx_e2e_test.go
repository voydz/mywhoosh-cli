package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// The fixture seam exists only in this compiled test process, never in the shipping CLI.
func TestGPXFixtureSubprocess(t *testing.T) {
	if os.Getenv("MYWHOOSH_GPX_TEST_HELPER") == "1" {
		c, _ := gpxCommandClient(t, []byte(cliGPX))
		code, out, stderr := invoke(t, c, os.Getenv("MYWHOOSH_GPX_TEST_CONFIG"), "", "activities", "get", "target", "--format", "json")
		fmt.Fprint(os.Stdout, out)
		fmt.Fprint(os.Stderr, stderr)
		os.Exit(code)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestGPXFixtureSubprocess$")
	cmd.Env = append(os.Environ(), "MYWHOOSH_GPX_TEST_HELPER=1", "MYWHOOSH_GPX_TEST_CONFIG="+savedSession(t))
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil || stderr.Len() != 0 {
		t.Fatalf("subprocess: %v %q", err, stderr.String())
	}
	var detail map[string]json.RawMessage
	if json.Unmarshal(out.Bytes(), &detail) != nil || string(detail["source"]) != `"gpx"` || string(detail["sessions"]) != `[]` || !bytes.Contains(detail["tracks"], []byte(`"power_watts":195`)) {
		t.Fatalf("subprocess schema: %s", out.String())
	}
}
