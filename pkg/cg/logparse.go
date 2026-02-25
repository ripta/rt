package cg

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProcessedLine is the result of processing a raw output line for display.
type ProcessedLine struct {
	// Prefix is an optional string to display before the message, e.g., timestamp
	Prefix string
	// Line is the main content to display
	Line string
}

// LineProcessor transforms raw output lines for display. Returns nil to pass
// the line through unchanged.
type LineProcessor func(line string) *ProcessedLine

// JSONProcessorOptions configures the JSON log line processor.
type JSONProcessorOptions struct {
	MessageKey   string
	TimestampKey string
	TimestampFmt string
	Fields       []string
	Format       string
}

// NewJSONProcessor returns a LineProcessor that extracts the message from JSON
// log lines. Non-JSON lines and lines missing the message key pass through
// unchanged.
func NewJSONProcessor(opts JSONProcessorOptions) LineProcessor {
	var tsParser *TimestampParser
	if opts.TimestampKey != "" {
		tsParser = NewTimestampParser(opts.TimestampFmt, opts.Format)
	}

	return func(line string) *ProcessedLine {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			return nil
		}

		msgVal, ok := obj[opts.MessageKey]
		if !ok {
			return nil
		}

		msg := stringify(msgVal)

		var suffix string
		if len(opts.Fields) > 0 {
			suffix = formatFields(obj, opts.Fields, opts.MessageKey, opts.TimestampKey)
		}

		var prefix string
		if tsParser != nil {
			if tsVal, ok := obj[opts.TimestampKey]; ok {
				prefix = tsParser.Parse(tsVal)
			}
		}

		return &ProcessedLine{
			Prefix: prefix,
			Line:   msg + suffix,
		}
	}
}

// stringify converts a JSON value to its string representation.
func stringify(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64, bool:
		return fmt.Sprint(val)
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

// parseFieldsFlag splits a comma-separated fields string into a slice.
// Returns nil for empty input.
func parseFieldsFlag(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
