package cg

import (
	"testing"
	"time"
)

// unixExpected computes the expected formatted string for a unix timestamp
// in the local timezone, matching the behavior of time.Unix().Format().
func unixExpected(sec int64, layout string) string {
	return time.Unix(sec, 0).Format(layout)
}

type timestampParseTest struct {
	name   string
	parser *TimestampParser
	val    any
	want   string
}

var timestampParseTests = []timestampParseTest{
	{
		name:   "RFC 3339 string",
		parser: NewTimestampParser("", "15:04:05 "),
		val:    "2024-01-15T10:30:00Z",
		want:   "10:30:00 ",
	},
	{
		name:   "RFC 3339 with nanoseconds",
		parser: NewTimestampParser("", "15:04:05.000 "),
		val:    "2024-01-15T10:30:00.123456789Z",
		want:   "10:30:00.123 ",
	},
	{
		name:   "explicit rfc3339 format",
		parser: NewTimestampParser("rfc3339", "15:04:05 "),
		val:    "2024-01-15T10:30:00Z",
		want:   "10:30:00 ",
	},
	{
		name:   "explicit rfc3339 rejects number",
		parser: NewTimestampParser("rfc3339", "15:04:05 "),
		val:    float64(1705312200),
		want:   "",
	},
	{
		name:   "explicit unix-s rejects RFC 3339 string",
		parser: NewTimestampParser("unix-s", "15:04:05 "),
		val:    "2024-01-15T10:30:00Z",
		want:   "",
	},
	{
		name:   "non-parseable value",
		parser: NewTimestampParser("", "15:04:05 "),
		val:    "not a timestamp",
		want:   "",
	},
	{
		name:   "nil value",
		parser: NewTimestampParser("", "15:04:05 "),
		val:    nil,
		want:   "",
	},
	{
		name:   "boolean value",
		parser: NewTimestampParser("", "15:04:05 "),
		val:    true,
		want:   "",
	},
	{
		name:   "year before 1970 rejected",
		parser: NewTimestampParser("rfc3339", "15:04:05 "),
		val:    "1969-12-31T23:59:59Z",
		want:   "",
	},
	{
		name:   "year after 2100 rejected",
		parser: NewTimestampParser("rfc3339", "15:04:05 "),
		val:    "2101-01-01T00:00:00Z",
		want:   "",
	},
	{
		name:   "year 1970 accepted",
		parser: NewTimestampParser("rfc3339", "2006 "),
		val:    "1970-01-01T00:00:00Z",
		want:   "1970 ",
	},
	{
		name:   "year 2100 accepted",
		parser: NewTimestampParser("rfc3339", "2006 "),
		val:    "2100-12-31T23:59:59Z",
		want:   "2100 ",
	},
}

func TestTimestampParse(t *testing.T) {
	t.Parallel()

	for _, tt := range timestampParseTests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.parser.Parse(tt.val)
			if got != tt.want {
				t.Errorf("Parse(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

type timestampParseUnixTest struct {
	name   string
	parser *TimestampParser
	val    any
	sec    int64
	layout string
}

var timestampParseUnixTests = []timestampParseUnixTest{
	{
		name:   "Unix seconds float",
		parser: NewTimestampParser("unix-s", "15:04:05 "),
		val:    float64(1705312200),
		sec:    1705312200,
		layout: "15:04:05 ",
	},
	{
		name:   "Unix seconds string",
		parser: NewTimestampParser("unix-s", "15:04:05 "),
		val:    "1705312200",
		sec:    1705312200,
		layout: "15:04:05 ",
	},
	{
		name:   "Unix milliseconds float",
		parser: NewTimestampParser("unix-ms", "15:04:05 "),
		val:    float64(1705312200000),
		sec:    1705312200,
		layout: "15:04:05 ",
	},
	{
		name:   "Unix milliseconds string",
		parser: NewTimestampParser("unix-ms", "15:04:05 "),
		val:    "1705312200000",
		sec:    1705312200,
		layout: "15:04:05 ",
	},
}

func TestTimestampParseUnix(t *testing.T) {
	t.Parallel()

	for _, tt := range timestampParseUnixTests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.parser.Parse(tt.val)
			want := unixExpected(tt.sec, tt.layout)
			if got != want {
				t.Errorf("Parse(%v) = %q, want %q", tt.val, got, want)
			}
		})
	}
}

func TestTimestampParserLockOnFirstSuccess(t *testing.T) {
	t.Parallel()

	tp := NewTimestampParser("", "15:04:05 ")

	// First call with RFC 3339 should lock to that format
	result := tp.Parse("2024-01-15T10:30:00Z")
	if result != "10:30:00 " {
		t.Fatalf("first Parse() = %q, want %q", result, "10:30:00 ")
	}
	if !tp.locked {
		t.Fatal("parser should be locked after first success")
	}
	if tp.format != tsFormatRFC3339 {
		t.Fatalf("locked format = %d, want %d (rfc3339)", tp.format, tsFormatRFC3339)
	}

	// Subsequent call with a unix timestamp should fail because format is locked
	result = tp.Parse(float64(1705312200))
	if result != "" {
		t.Errorf("Parse(unix) after RFC3339 lock = %q, want empty", result)
	}

	// RFC 3339 still works
	result = tp.Parse("2024-01-15T11:00:00Z")
	if result != "11:00:00 " {
		t.Errorf("Parse(rfc3339) after lock = %q, want %q", result, "11:00:00 ")
	}
}

func TestTimestampParserAutoDetectUnixFirst(t *testing.T) {
	t.Parallel()

	tp := NewTimestampParser("", "15:04:05 ")

	// Unix seconds should auto-detect and lock
	result := tp.Parse(float64(1705312200))
	if result == "" {
		t.Fatal("Parse(unix-s) should succeed on auto-detect")
	}
	if tp.format != tsFormatUnixS {
		t.Fatalf("locked format = %d, want %d (unix-s)", tp.format, tsFormatUnixS)
	}

	// RFC 3339 should now fail
	result = tp.Parse("2024-01-15T10:30:00Z")
	if result != "" {
		t.Errorf("Parse(rfc3339) after unix lock = %q, want empty", result)
	}
}
