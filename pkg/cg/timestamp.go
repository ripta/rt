package cg

import (
	"math"
	"strconv"
	"sync"
	"time"
)

// TimestampFormat identifies a timestamp encoding.
type TimestampFormat int

const (
	tsFormatRFC3339 TimestampFormat = iota + 1
	tsFormatUnixS
	tsFormatUnixMS
)

// TimestampParser auto-detects and parses timestamps from JSON values. It
// locks on the first successfully detected format, avoiding repeated probing.
type TimestampParser struct {
	mu     sync.Mutex
	locked bool
	format TimestampFormat
	layout string
}

// NewTimestampParser creates a parser. If explicitFmt is non-empty, it locks
// to that format immediately. layout is the Go time.Format layout used for
// rendering the prefix string.
func NewTimestampParser(explicitFmt string, layout string) *TimestampParser {
	tp := &TimestampParser{layout: layout}
	switch explicitFmt {
	case "rfc3339":
		tp.locked = true
		tp.format = tsFormatRFC3339
	case "unix-s":
		tp.locked = true
		tp.format = tsFormatUnixS
	case "unix-ms":
		tp.locked = true
		tp.format = tsFormatUnixMS
	}
	return tp
}

// Parse attempts to extract a time from val and returns a formatted prefix
// string. Returns empty string on failure.
func (tp *TimestampParser) Parse(val any) string {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if tp.locked {
		return tp.parseWith(tp.format, val)
	}

	for _, f := range []TimestampFormat{tsFormatRFC3339, tsFormatUnixS, tsFormatUnixMS} {
		if result := tp.parseWith(f, val); result != "" {
			tp.locked = true
			tp.format = f
			return result
		}
	}
	return ""
}

func (tp *TimestampParser) parseWith(format TimestampFormat, val any) string {
	var t time.Time
	var ok bool

	switch format {
	case tsFormatRFC3339:
		t, ok = parseRFC3339(val)
	case tsFormatUnixS:
		t, ok = parseUnixSeconds(val)
	case tsFormatUnixMS:
		t, ok = parseUnixMillis(val)
	}

	if !ok {
		return ""
	}
	if !inRange(t) {
		return ""
	}
	return t.Format(tp.layout)
}

func parseRFC3339(val any) (time.Time, bool) {
	s, ok := val.(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func parseUnixSeconds(val any) (time.Time, bool) {
	f, ok := toFloat64(val)
	if !ok {
		return time.Time{}, false
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)), true
}

func parseUnixMillis(val any) (time.Time, bool) {
	f, ok := toFloat64(val)
	if !ok {
		return time.Time{}, false
	}
	ms := int64(f)
	return time.Unix(ms/1000, (ms%1000)*1e6), true
}

func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func inRange(t time.Time) bool {
	year := t.Year()
	return year >= 1970 && year <= 2100
}

