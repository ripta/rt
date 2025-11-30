package calc

import (
	"fmt"
	"strconv"
	"strings"
)

// Setting is the interface that all settings must implement.
// This allows storing SettingDescriptor[bool], SettingDescriptor[int], etc.
// in the same registry.
type Setting interface {
	Name() string
	Description() string

	// SetValue parses and sets the value from a string
	SetValue(c *Calculator, value string) error

	// GetValue gets the current value as a string for display
	GetValue(c *Calculator) string
}

// SettingDescriptor is a generic descriptor for a setting of type T
type SettingDescriptor[T any] struct {
	name        string
	description string

	// Type-safe accessors
	Get func(*Calculator) T
	Set func(*Calculator, T)

	// Type-specific parsing and formatting
	Parse    func(string) (T, error)
	Format   func(T) string
	Validate func(T) error
}

// Name implements Setting interface
func (s *SettingDescriptor[T]) Name() string {
	return s.name
}

// Description implements Setting interface
func (s *SettingDescriptor[T]) Description() string {
	return s.description
}

// SetValue implements Setting interface
func (s *SettingDescriptor[T]) SetValue(c *Calculator, value string) error {
	v, err := s.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %s", s.name, value)
	}

	if s.Validate != nil {
		if err := s.Validate(v); err != nil {
			return err
		}
	}

	s.Set(c, v)

	// Format confirmation message
	displayName := formatSettingName(s.name)
	displayValue := s.Format(v)
	fmt.Printf("%s %s\n", displayName, displayValue)

	return nil
}

// GetValue implements Setting interface
func (s *SettingDescriptor[T]) GetValue(c *Calculator) string {
	return s.Format(s.Get(c))
}

// NewBoolSetting creates a boolean setting
func NewBoolSetting(name, desc string, get func(*Calculator) bool, set func(*Calculator, bool)) Setting {
	return &SettingDescriptor[bool]{
		name:        name,
		description: desc,
		Get:         get,
		Set:         set,
		Parse:       parseBool,
		Format: func(v bool) string {
			if v {
				return "on"
			}
			return "off"
		},
	}
}

// NewIntSetting creates an integer setting
func NewIntSetting(name, desc string, get func(*Calculator) int, set func(*Calculator, int), validate func(int) error) Setting {
	return &SettingDescriptor[int]{
		name:        name,
		description: desc,
		Get:         get,
		Set:         set,
		Parse: func(s string) (int, error) {
			v, err := strconv.Atoi(s)
			if err != nil {
				return 0, fmt.Errorf("invalid number: %s", s)
			}
			return v, nil
		},
		Format:   func(v int) string { return fmt.Sprintf("%d", v) },
		Validate: validate,
	}
}

// settingsRegistry is the single source of truth for all settings
var settingsRegistry = []Setting{
	NewBoolSetting(
		"trace",
		"Enable/disable trace output (on/off)",
		func(c *Calculator) bool { return c.Trace },
		func(c *Calculator, v bool) { c.Trace = v },
	),
	NewIntSetting(
		"decimal_places",
		"Number of decimal places to display (integer)",
		func(c *Calculator) int { return c.DecimalPlaces },
		func(c *Calculator, v int) { c.DecimalPlaces = v },
		func(v int) error {
			if v < 0 {
				return fmt.Errorf("decimal_places must be non-negative")
			}
			return nil
		},
	),
	NewBoolSetting(
		"keep_trailing_zeros",
		"Keep trailing zeros in output (on/off)",
		func(c *Calculator) bool { return c.KeepTrailingZeros },
		func(c *Calculator, v bool) { c.KeepTrailingZeros = v },
	),
	NewBoolSetting(
		"underscore_zeros",
		"Insert underscore before trailing zeros (on/off)",
		func(c *Calculator) bool { return c.UnderscoreZeros },
		func(c *Calculator, v bool) { c.UnderscoreZeros = v },
	),
	NewBoolSetting(
		"verbose",
		"Enable verbose output (on/off)",
		func(c *Calculator) bool { return c.Verbose },
		func(c *Calculator, v bool) { c.Verbose = v },
	),
}

// settingsIndex provides fast O(1) lookup by name
var settingsIndex map[string]Setting

func init() {
	settingsIndex = make(map[string]Setting, len(settingsRegistry))
	for _, setting := range settingsRegistry {
		settingsIndex[setting.Name()] = setting
	}
}

// findSetting looks up a setting by name (case-insensitive)
func findSetting(name string) (Setting, error) {
	setting, ok := settingsIndex[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("unknown setting: %s", name)
	}
	return setting, nil
}

// formatSettingName converts snake_case to Title Case for display
// Example: "keep_trailing_zeros" -> "Keep trailing zeros"
func formatSettingName(name string) string {
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}
