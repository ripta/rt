package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGateNeverExplains guards ADR-06 structurally. The explain call lists the rules the ranking put
// below the deciding set. If any non-test file of this package called it, an outranked rule would be
// one field access from the gate. The test cannot catch a call made through another package, but the
// gate reaches the ruleset only through this package.
func TestGateNeverExplains(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}

		if strings.Contains(string(src), ".Explain(") {
			t.Errorf("%s calls Explain, which the gate must never do", file)
		}
	}
}
