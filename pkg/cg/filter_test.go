package cg

import (
	"errors"
	"testing"
	"time"
)

type parseExitCodeFilterTest struct {
	name    string
	in      string
	want    ExitCodeFilter
	wantErr bool
}

var parseExitCodeFilterTests = []parseExitCodeFilterTest{
	{name: "bare zero", in: "0", want: ExitCodeFilter{op: exitCodeEQ, value: 0}},
	{name: "bare positive", in: "2", want: ExitCodeFilter{op: exitCodeEQ, value: 2}},
	{name: "bare negative", in: "-1", want: ExitCodeFilter{op: exitCodeEQ, value: -1}},
	{name: "not equal", in: "!=0", want: ExitCodeFilter{op: exitCodeNE, value: 0}},
	{name: "greater equal", in: ">=1", want: ExitCodeFilter{op: exitCodeGE, value: 1}},
	{name: "greater than", in: ">0", want: ExitCodeFilter{op: exitCodeGT, value: 0}},
	{name: "less than", in: "<0", want: ExitCodeFilter{op: exitCodeLT, value: 0}},
	{name: "less equal", in: "<=5", want: ExitCodeFilter{op: exitCodeLE, value: 5}},
	{name: "greater than negative", in: ">-5", want: ExitCodeFilter{op: exitCodeGT, value: -5}},
	{name: "whitespace trimmed", in: " >= 1 ", want: ExitCodeFilter{op: exitCodeGE, value: 1}},
	{name: "empty", in: "", wantErr: true},
	{name: "garbage", in: "abc", wantErr: true},
	{name: "unsupported operator", in: "==1", wantErr: true},
}

func TestParseExitCodeFilter(t *testing.T) {
	t.Parallel()

	for _, tt := range parseExitCodeFilterTests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExitCodeFilter(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseExitCodeFilter(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExitCodeFilter(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseExitCodeFilter(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

type exitCodeFilterMatchTest struct {
	name string
	f    ExitCodeFilter
	code int
	want bool
}

var exitCodeFilterMatchTests = []exitCodeFilterMatchTest{
	{name: "eq matches", f: ExitCodeFilter{op: exitCodeEQ, value: 0}, code: 0, want: true},
	{name: "eq mismatches", f: ExitCodeFilter{op: exitCodeEQ, value: 0}, code: 1, want: false},
	{name: "ne matches", f: ExitCodeFilter{op: exitCodeNE, value: 0}, code: 1, want: true},
	{name: "ne mismatches", f: ExitCodeFilter{op: exitCodeNE, value: 0}, code: 0, want: false},
	{name: "ge boundary matches", f: ExitCodeFilter{op: exitCodeGE, value: 1}, code: 1, want: true},
	{name: "ge above matches", f: ExitCodeFilter{op: exitCodeGE, value: 1}, code: 2, want: true},
	{name: "ge below mismatches", f: ExitCodeFilter{op: exitCodeGE, value: 1}, code: 0, want: false},
	{name: "gt boundary mismatches", f: ExitCodeFilter{op: exitCodeGT, value: 0}, code: 0, want: false},
	{name: "gt above matches", f: ExitCodeFilter{op: exitCodeGT, value: 0}, code: 1, want: true},
	{name: "le boundary matches", f: ExitCodeFilter{op: exitCodeLE, value: 5}, code: 5, want: true},
	{name: "le above mismatches", f: ExitCodeFilter{op: exitCodeLE, value: 5}, code: 6, want: false},
	{name: "lt boundary mismatches", f: ExitCodeFilter{op: exitCodeLT, value: 0}, code: 0, want: false},
	{name: "lt below matches", f: ExitCodeFilter{op: exitCodeLT, value: 0}, code: -1, want: true},
}

func TestExitCodeFilterMatch(t *testing.T) {
	t.Parallel()

	for _, tt := range exitCodeFilterMatchTests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.f.Match(tt.code); got != tt.want {
				t.Errorf("Match(%d) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestParseFilterTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)

	t.Run("relative hours", func(t *testing.T) {
		got, err := ParseFilterTime("4h", now)
		if err != nil {
			t.Fatalf("ParseFilterTime error = %v", err)
		}
		want := now.Add(-4 * time.Hour)
		if !got.Equal(want) {
			t.Errorf("ParseFilterTime(4h) = %v, want %v", got, want)
		}
	})

	t.Run("relative days", func(t *testing.T) {
		got, err := ParseFilterTime("7d", now)
		if err != nil {
			t.Fatalf("ParseFilterTime error = %v", err)
		}
		want := now.Add(-7 * 24 * time.Hour)
		if !got.Equal(want) {
			t.Errorf("ParseFilterTime(7d) = %v, want %v", got, want)
		}
	})

	t.Run("rfc3339 with offset", func(t *testing.T) {
		got, err := ParseFilterTime("2026-07-19T15:04:05-07:00", now)
		if err != nil {
			t.Fatalf("ParseFilterTime error = %v", err)
		}
		want, _ := time.Parse(time.RFC3339, "2026-07-19T15:04:05-07:00")
		if !got.Equal(want) {
			t.Errorf("ParseFilterTime(rfc3339) = %v, want %v", got, want)
		}
	})

	t.Run("rfc3339 with fractional seconds", func(t *testing.T) {
		got, err := ParseFilterTime("2026-07-19T15:04:05.123456789Z", now)
		if err != nil {
			t.Fatalf("ParseFilterTime error = %v", err)
		}
		want, _ := time.Parse(time.RFC3339Nano, "2026-07-19T15:04:05.123456789Z")
		if !got.Equal(want) {
			t.Errorf("ParseFilterTime(rfc3339nano) = %v, want %v", got, want)
		}
	})

	t.Run("bare date local midnight", func(t *testing.T) {
		got, err := ParseFilterTime("2026-07-19", now)
		if err != nil {
			t.Fatalf("ParseFilterTime error = %v", err)
		}
		want := time.Date(2026, 7, 19, 0, 0, 0, 0, time.Local)
		if !got.Equal(want) {
			t.Errorf("ParseFilterTime(bare date) = %v, want %v", got, want)
		}
	})

	t.Run("empty", func(t *testing.T) {
		if _, err := ParseFilterTime("", now); err == nil {
			t.Error("ParseFilterTime(\"\") = nil error, want error")
		}
	})

	t.Run("garbage", func(t *testing.T) {
		if _, err := ParseFilterTime("banana", now); err == nil {
			t.Error("ParseFilterTime(banana) = nil error, want error")
		}
	})
}

func TestNewTimeRangeFilter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)

	t.Run("since before before is ok", func(t *testing.T) {
		if _, err := NewTimeRangeFilter(&earlier, &later); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("since equal before errors", func(t *testing.T) {
		_, err := NewTimeRangeFilter(&now, &now)
		if !errors.Is(err, ErrEmptyTimeRange) {
			t.Errorf("error = %v, want ErrEmptyTimeRange", err)
		}
	})

	t.Run("since after before errors", func(t *testing.T) {
		_, err := NewTimeRangeFilter(&later, &earlier)
		if !errors.Is(err, ErrEmptyTimeRange) {
			t.Errorf("error = %v, want ErrEmptyTimeRange", err)
		}
	})

	t.Run("nil since is ok", func(t *testing.T) {
		if _, err := NewTimeRangeFilter(nil, &later); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil before is ok", func(t *testing.T) {
		if _, err := NewTimeRangeFilter(&earlier, nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("both nil is ok", func(t *testing.T) {
		if _, err := NewTimeRangeFilter(nil, nil); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestTimeRangeFilterMatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	before := now.Add(time.Hour)
	f, err := NewTimeRangeFilter(&since, &before)
	if err != nil {
		t.Fatalf("NewTimeRangeFilter: %v", err)
	}

	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{name: "before since", t: since.Add(-time.Minute), want: false},
		{name: "at since (inclusive)", t: since, want: true},
		{name: "strictly between", t: now, want: true},
		{name: "at before (exclusive)", t: before, want: false},
		{name: "after before", t: before.Add(time.Minute), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := f.Match(tt.t); got != tt.want {
				t.Errorf("Match(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}
