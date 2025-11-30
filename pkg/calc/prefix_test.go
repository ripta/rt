package calc

import (
	"strings"
	"testing"
)

type findByPrefixTest struct {
	name    string
	prefix  string
	want    string // expected value
	wantErr bool
	errMsg  string // substring to check in error message
}

var findByPrefixTests = []findByPrefixTest{
	// Exact matches
	{"exact: help", "help", "help_value", false, ""},
	{"exact: set", "set", "set_value", false, ""},
	{"exact: show", "show", "show_value", false, ""},
	{"exact: save", "save", "save_value", false, ""},
	{"exact: load", "load", "load_value", false, ""},

	// Case-insensitive exact matches
	{"case: HELP", "HELP", "help_value", false, ""},
	{"case: Help", "Help", "help_value", false, ""},
	{"case: SET", "SET", "set_value", false, ""},

	// Single character prefixes
	{"prefix: h", "h", "help_value", false, ""},
	{"prefix: l", "l", "load_value", false, ""},

	// Multi-character prefixes
	{"prefix: he", "he", "help_value", false, ""},
	{"prefix: hel", "hel", "help_value", false, ""},
	{"prefix: se", "se", "set_value", false, ""},
	{"prefix: sh", "sh", "show_value", false, ""},
	{"prefix: sho", "sho", "show_value", false, ""},
	{"prefix: sa", "sa", "save_value", false, ""},
	{"prefix: sav", "sav", "save_value", false, ""},
	{"prefix: lo", "lo", "load_value", false, ""},
	{"prefix: loa", "loa", "load_value", false, ""},

	// Case-insensitive prefixes
	{"prefix case: H", "H", "help_value", false, ""},
	{"prefix case: HE", "HE", "help_value", false, ""},
	{"prefix case: He", "He", "help_value", false, ""},
	{"prefix case: SE", "SE", "set_value", false, ""},

	// Ambiguous prefixes
	{"ambiguous: s", "s", "", true, "ambiguous"},
	{"ambiguous: sa vs se vs sh", "s", "", true, "set"},

	// Unknown prefixes
	{"unknown: x", "x", "", true, "prefix not found"},
	{"unknown: xyz", "xyz", "", true, "prefix not found"},
	{"unknown: foo", "foo", "", true, "prefix not found"},

	// Empty prefix (matches all)
	{"empty string", "", "", true, "ambiguous"},
}

func TestFindByPrefix(t *testing.T) {
	t.Parallel()

	items := map[string]string{
		"help": "help_value",
		"set":  "set_value",
		"show": "show_value",
		"save": "save_value",
		"load": "load_value",
	}

	for _, tt := range findByPrefixTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findByPrefix(tt.prefix, items)
			if (err != nil) != tt.wantErr {
				t.Errorf("findByPrefix(%q) error = %v, wantErr %v", tt.prefix, err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("findByPrefix(%q) error = %q, want error containing %q", tt.prefix, err.Error(), tt.errMsg)
				}
				return
			}

			if got != tt.want {
				t.Errorf("findByPrefix(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestFindByPrefixAmbiguousErrorListing(t *testing.T) {
	items := map[string]string{
		"help": "help_value",
		"set":  "set_value",
		"show": "show_value",
		"save": "save_value",
	}

	_, err := findByPrefix("s", items)
	if err == nil {
		t.Fatal("findByPrefix(\"s\") should return error for ambiguous match, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ambiguous") {
		t.Errorf("error = %q, want error containing \"ambiguous\"", errMsg)
	}

	expectedMatches := []string{"set", "show", "save"}
	for _, match := range expectedMatches {
		if !strings.Contains(errMsg, match) {
			t.Errorf("error should list %q in: %q", match, errMsg)
		}
	}
}

func TestFindByPrefixEmptyMap(t *testing.T) {
	items := map[string]string{}

	_, err := findByPrefix("test", items)
	if err == nil {
		t.Error("findByPrefix on empty map should return error, got nil")
		return
	}

	if !strings.Contains(err.Error(), "prefix not found") {
		t.Errorf("error = %q, want error containing \"prefix not found\"", err.Error())
	}
}
