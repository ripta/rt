package manager

import (
	"fmt"
)

func ConvertAnyMapToStringMapRecursive(result map[string]any, m map[any]any) {
	for k, v := range m {
		switch k := k.(type) {
		case string:
			result[k] = ConvertAnyRecursive(v)
		case []byte:
			result[string(k)] = ConvertAnyRecursive(v)
		case fmt.Stringer:
			result[k.String()] = ConvertAnyRecursive(v)
		default:
			result[fmt.Sprintf("%v", k)] = ConvertAnyRecursive(v)
		}
	}
}

func ConvertAnyRecursive(v any) any {
	switch v := v.(type) {
	case []any:
		nestedV := make([]any, len(v))
		for i, item := range v {
			nestedV[i] = ConvertAnyRecursive(item)
		}
		return nestedV

	case map[any]any:
		nestedResult := map[string]any{}
		ConvertAnyMapToStringMapRecursive(nestedResult, v)
		return nestedResult

	default:
		return v
	}
}
