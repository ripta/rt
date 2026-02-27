package cg

import (
	"fmt"
	"sort"
	"strings"
)

// formatFields produces a " key=value key=value" suffix from the given JSON
// object. If fields is ["*"], all keys are included in alphabetical order,
// excluding any keys in excludeKeys. Values containing spaces are quoted.
func formatFields(obj map[string]any, fields []string, excludeKeys ...string) string {
	if len(fields) == 0 {
		return ""
	}

	exclude := make(map[string]bool, len(excludeKeys))
	for _, k := range excludeKeys {
		if k != "" {
			exclude[k] = true
		}
	}

	var keys []string
	if len(fields) == 1 && fields[0] == "*" {
		for k := range obj {
			if !exclude[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
	} else {
		for _, k := range fields {
			if _, ok := obj[k]; ok && !exclude[k] {
				keys = append(keys, k)
			}
		}
	}

	if len(keys) == 0 {
		return ""
	}

	sb := strings.Builder{}
	for _, k := range keys {
		v := stringify(obj[k])
		sb.WriteByte(' ')
		sb.WriteString(k)
		sb.WriteByte('=')
		if strings.ContainsRune(v, ' ') {
			fmt.Fprintf(&sb, "%q", v)
		} else {
			sb.WriteString(v)
		}
	}
	return sb.String()
}
