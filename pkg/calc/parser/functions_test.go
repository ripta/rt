package parser

import "testing"

// TestFunctionsMatchRegistry guards against drift between the dispatch map and
// the catalog the discoverability listing reads from.
func TestFunctionsMatchRegistry(t *testing.T) {
	t.Parallel()

	infos := Functions()
	if len(infos) != len(functions) {
		t.Fatalf("Functions() has %d entries, registry has %d", len(infos), len(functions))
	}

	seen := make(map[string]bool, len(infos))
	for _, info := range infos {
		if _, ok := functions[info.Name]; !ok {
			t.Errorf("Functions() lists %q, absent from the dispatch registry", info.Name)
		}
		seen[info.Name] = true
	}

	for name := range functions {
		if !seen[name] {
			t.Errorf("registry has %q, absent from Functions()", name)
		}
	}
}

// TestFunctionMetadataPopulated verifies every catalog entry carries the
// display fields the listing depends on.
func TestFunctionMetadataPopulated(t *testing.T) {
	t.Parallel()

	for _, info := range Functions() {
		if info.Group == "" {
			t.Errorf("%q: empty Group", info.Name)
		}
		if info.Signature == "" {
			t.Errorf("%q: empty Signature", info.Name)
		}
		if info.Summary == "" {
			t.Errorf("%q: empty Summary", info.Name)
		}
	}
}
