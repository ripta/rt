package decoder

import (
	"reflect"
	"strings"
)

type SplitterFunc func(any) ([]any, bool)

func KubernetesListSplitter(obj any) ([]any, bool) {
	rv := reflect.ValueOf(obj)
	if !rv.IsValid() {
		return nil, false
	}
	if rv.IsNil() {
		return nil, false
	}
	if rv.Kind() != reflect.Map {
		return nil, false
	}

	rvKind, ok := traversePath(rv, []any{"kind"})
	if !ok {
		return nil, false
	}

	valKind, ok := rvKind.Interface().(string)
	if !ok {
		return nil, false
	}

	// fmt.Printf("YYY: %s\n", valKind)
	if !strings.HasSuffix(valKind, "List") {
		return nil, false
	}

	return GenerateSplitter("items")(obj)
}

func GenerateSplitter(paths ...any) func(obj any) ([]any, bool) {
	return func(obj any) ([]any, bool) {
		rv := reflect.ValueOf(obj)
		if !rv.IsValid() {
			return nil, false
		}
		if rv.IsNil() {
			return nil, false
		}
		if rv.Kind() != reflect.Map {
			return nil, false
		}

		rvItems, ok := traversePath(rv, paths)
		if !ok {
			return nil, false
		}

		//valItems, ok := rvItems.Interface().([]map[string]any)
		//if ok {
		//	anyItems := []any{}
		//	for i := range valItems {
		//		anyItems = append(anyItems, valItems[i])
		//	}
		//
		//	return anyItems, true
		//}

		anyItems, ok := rvItems.Interface().([]any)
		return anyItems, ok
	}
}
