package calc

import (
	"fmt"
	"strings"
)

var (
	ErrAmbiguousPrefix = fmt.Errorf("ambiguous prefix")
	ErrPrefixNotFound  = fmt.Errorf("prefix not found")
)

// findByPrefix performs case-insensitive unambiguous prefix matching on a map.
// Returns the matched value if exactly one match is found. Returns an error if
// no matches or multiple (ambiguous) matches are found.
func findByPrefix[T any](prefix string, items map[string]T) (T, error) {
	var zero T
	prefix = strings.ToLower(prefix)

	var matches []T
	var matchNames []string
	for name, item := range items {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			matches = append(matches, item)
			matchNames = append(matchNames, name)
		}
	}

	if len(matches) == 0 {
		return zero, fmt.Errorf("%w %q", ErrPrefixNotFound, prefix)
	} else if len(matches) > 1 {
		return zero, fmt.Errorf("%w %q, coult be one of: %s", ErrAmbiguousPrefix, prefix, strings.Join(matchNames, ", "))
	}

	return matches[0], nil
}
