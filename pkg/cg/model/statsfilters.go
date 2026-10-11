package model

import (
	"fmt"
	"time"
)

// StatsFilters holds the raw, user-supplied filters of a stats lookup. The CLI flags and the MCP
// tool inputs both fill it, so the two surfaces parse and validate identically.
type StatsFilters struct {
	Cwd      string
	Since    string
	ExitCode string
	Samples  int
	Pool     string
}

// StatsFilterError reports a filter value that could not be parsed. Field is the snake_case name
// of the offending filter, either "since" or "exit_code".
type StatsFilterError struct {
	Field string
	Err   error
}

func (e *StatsFilterError) Error() string {
	return fmt.Sprintf("invalid %s: %v", e.Field, e.Err)
}

func (e *StatsFilterError) Unwrap() error {
	return e.Err
}

// Query builds a validated query for argv. A parse failure is a *StatsFilterError. Any other
// problem wraps ErrInvalidStatsQuery. An empty Since means no lower bound.
func (f StatsFilters) Query(argv []string, now time.Time) (StatsQuery, error) {
	exitFilter, err := ParseExitCodeFilter(f.ExitCode)
	if err != nil {
		return StatsQuery{}, &StatsFilterError{Field: "exit_code", Err: err}
	}

	query := NewStatsQuery(argv)
	query.Cwd = f.Cwd
	query.ExitCode = exitFilter
	query.Samples = f.Samples
	query.Pool = f.Pool

	if f.Since != "" {
		since, err := ParseFilterTime(f.Since, now)
		if err != nil {
			return StatsQuery{}, &StatsFilterError{Field: "since", Err: err}
		}
		query.Since = &since
	}

	if err := query.Validate(); err != nil {
		return StatsQuery{}, err
	}

	return query, nil
}
