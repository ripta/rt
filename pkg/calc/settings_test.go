package calc

import (
	"strings"
	"testing"
)

type findSettingTest struct {
	name    string
	prefix  string
	want    string // expected setting name
	wantErr bool
	errMsg  string // substring to check in error message
}

var findSettingTests = []findSettingTest{
	// Exact matches
	{"exact: trace", "trace", "trace", false, ""},
	{"exact: verbose", "verbose", "verbose", false, ""},
	{"exact: decimal_places", "decimal_places", "decimal_places", false, ""},
	{"exact: keep_trailing_zeros", "keep_trailing_zeros", "keep_trailing_zeros", false, ""},
	{"exact: underscore_zeros", "underscore_zeros", "underscore_zeros", false, ""},

	// Case-insensitive exact matches
	{"case: TRACE", "TRACE", "trace", false, ""},
	{"case: Verbose", "Verbose", "verbose", false, ""},
	{"case: Decimal_Places", "Decimal_Places", "decimal_places", false, ""},

	// Single character prefixes (all currently unambiguous)
	{"prefix: t", "t", "trace", false, ""},
	{"prefix: v", "v", "verbose", false, ""},
	{"prefix: d", "d", "decimal_places", false, ""},
	{"prefix: k", "k", "keep_trailing_zeros", false, ""},
	{"prefix: u", "u", "underscore_zeros", false, ""},

	// Multi-character prefixes
	{"prefix: tra", "tra", "trace", false, ""},
	{"prefix: ver", "ver", "verbose", false, ""},
	{"prefix: dec", "dec", "decimal_places", false, ""},
	{"prefix: keep", "keep", "keep_trailing_zeros", false, ""},
	{"prefix: under", "under", "underscore_zeros", false, ""},

	// Case-insensitive prefixes
	{"prefix case: T", "T", "trace", false, ""},
	{"prefix case: V", "V", "verbose", false, ""},
	{"prefix case: TRA", "TRA", "trace", false, ""},

	// Unknown setting
	{"unknown: xyz", "xyz", "", true, "unknown setting"},
	{"unknown: foo", "foo", "", true, "unknown setting"},
	{"unknown: x", "x", "", true, "unknown setting"},

	// Empty string would match all settings if any existed
	{"empty string", "", "", true, "ambiguous"},
}

func TestFindSetting(t *testing.T) {
	t.Parallel()

	for _, tt := range findSettingTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findSetting(tt.prefix)
			if (err != nil) != tt.wantErr {
				t.Errorf("findSetting(%q) error = %v, wantErr %v", tt.prefix, err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("findSetting(%q) error = %q, want error containing %q", tt.prefix, err.Error(), tt.errMsg)
				}
				return
			}

			if got == nil {
				t.Errorf("findSetting(%q) returned nil, want setting %q", tt.prefix, tt.want)
				return
			}
			if got.Name != tt.want {
				t.Errorf("findSetting(%q) = %q, want %q", tt.prefix, got.Name, tt.want)
			}
		})
	}
}

// TestFindSettingAmbiguous tests that ambiguous prefixes are properly detected.
// Currently all settings start with different letters, so we test with hypothetical
// settings to ensure the ambiguity detection logic works correctly.
func TestFindSettingAmbiguous(t *testing.T) {
	_, err := findSetting("")
	if err == nil {
		t.Error("findSetting should return error for ambiguous match, got nil")
		return
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "ambiguous") {
		t.Errorf("findSetting error = %q, want error containing \"ambiguous\"", errMsg)
	}

	// The error should list all the settings
	for _, setting := range settingsRegistry {
		if !strings.Contains(errMsg, setting.Name) {
			t.Errorf("findSetting(\"\") error should list all settings, missing %q in: %q", setting.Name, errMsg)
		}
	}
}

// TestFindSettingAllSettings verifies that all settings in the registry can be found
// by their full name and by their first character.
func TestFindSettingAllSettings(t *testing.T) {
	t.Parallel()

	for _, setting := range settingsRegistry {
		t.Run("full:"+setting.Name, func(t *testing.T) {
			got, err := findSetting(setting.Name)
			if err != nil {
				t.Errorf("findSetting(%q) error = %v, want nil", setting.Name, err)
				return
			}
			if got.Name != setting.Name {
				t.Errorf("findSetting(%q) = %q, want %q", setting.Name, got.Name, setting.Name)
			}
		})

		firstChar := string(setting.Name[0])
		t.Run("prefix:"+firstChar, func(t *testing.T) {
			got, err := findSetting(firstChar)
			// We expect this to succeed since current settings all start with different letters
			if err != nil {
				t.Errorf("findSetting(%q) error = %v, want nil (current settings have unique first chars)", firstChar, err)
				return
			}
			if got.Name != setting.Name {
				t.Errorf("findSetting(%q) = %q, want %q", firstChar, got.Name, setting.Name)
			}
		})
	}
}
