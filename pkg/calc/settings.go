package calc

import (
	"fmt"
	"strings"
)

// SettingType represents the data type of a setting
type SettingType int

const (
	SettingTypeBool SettingType = iota
	SettingTypeInt
)

// SettingDescriptor contains all metadata for a setting
type SettingDescriptor struct {
	Name        string      // e.g., "trace"
	Type        SettingType // bool or int
	Description string      // Help text

	// Type-safe accessors using closures
	GetBool func(*Calculator) bool
	SetBool func(*Calculator, bool)
	GetInt  func(*Calculator) int
	SetInt  func(*Calculator, int)

	// Optional validation for int types
	ValidateInt func(int) error
}

// settingsRegistry is the single source of truth for all settings
var settingsRegistry = []SettingDescriptor{
	{
		Name:        "trace",
		Type:        SettingTypeBool,
		Description: "Enable/disable trace output (on/off)",
		GetBool:     func(c *Calculator) bool { return c.Trace },
		SetBool:     func(c *Calculator, v bool) { c.Trace = v },
	},
	{
		Name:        "decimal_places",
		Type:        SettingTypeInt,
		Description: "Number of decimal places to display (integer)",
		GetInt:      func(c *Calculator) int { return c.DecimalPlaces },
		SetInt:      func(c *Calculator, v int) { c.DecimalPlaces = v },
		ValidateInt: func(v int) error {
			if v < 0 {
				return fmt.Errorf("decimal_places must be non-negative")
			}
			return nil
		},
	},
	{
		Name:        "keep_trailing_zeros",
		Type:        SettingTypeBool,
		Description: "Keep trailing zeros in output (on/off)",
		GetBool:     func(c *Calculator) bool { return c.KeepTrailingZeros },
		SetBool:     func(c *Calculator, v bool) { c.KeepTrailingZeros = v },
	},
	{
		Name:        "underscore_zeros",
		Type:        SettingTypeBool,
		Description: "Insert underscore before trailing zeros (on/off)",
		GetBool:     func(c *Calculator) bool { return c.UnderscoreZeros },
		SetBool:     func(c *Calculator, v bool) { c.UnderscoreZeros = v },
	},
	{
		Name:        "verbose",
		Type:        SettingTypeBool,
		Description: "Enable verbose output (on/off)",
		GetBool:     func(c *Calculator) bool { return c.Verbose },
		SetBool:     func(c *Calculator, v bool) { c.Verbose = v },
	},
}

// settingsIndex provides fast O(1) lookup by name
var settingsIndex map[string]*SettingDescriptor

func init() {
	settingsIndex = make(map[string]*SettingDescriptor, len(settingsRegistry))
	for i := range settingsRegistry {
		settingsIndex[settingsRegistry[i].Name] = &settingsRegistry[i]
	}
}

// findSetting looks up a setting by name (case-insensitive)
func findSetting(name string) (*SettingDescriptor, error) {
	setting, ok := settingsIndex[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("unknown setting: %s", name)
	}
	return setting, nil
}
