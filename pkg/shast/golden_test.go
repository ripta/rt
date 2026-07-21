package shast

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update", false, "update golden files in testdata/")

// TestParseGolden snapshots the full worked example from the design
// conversation (mkdir -p $(echo "foo")) so schema drift shows up as a diff
// instead of silently passing every field-by-field assertion elsewhere in
// this package. Run with -update to refresh the golden file after an
// intentional schema change.
func TestParseGolden(t *testing.T) {
	out, err := runParse(t, "--", `mkdir -p $(echo "foo")`)
	if exitCode(t, err) != 0 {
		t.Fatalf("exit code = %d, want 0; err = %v", exitCode(t, err), err)
	}

	golden := filepath.Join("testdata", "mkdir-cmdsubst.json")

	if *updateGolden {
		if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file: %v (run `go test ./pkg/shast/... -run TestParseGolden -update` to create it)", err)
	}
	if out != string(want) {
		t.Errorf("output != golden file %s (run `go test ./pkg/shast/... -run TestParseGolden -update` to refresh)\ngot:\n%s\nwant:\n%s", golden, out, want)
	}
}
