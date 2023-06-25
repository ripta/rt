package decoder

import "reflect"

var concreteNil = reflect.ValueOf(nil)

func traversePath(rv reflect.Value, paths []any) (reflect.Value, bool) {
	if !rv.IsValid() {
		return rv, false
	}

	curr := rv
	found := false
	for {
		if len(paths) == 0 {
			// fmt.Printf("found!\n")
			found = true
			break
		}
		if curr.IsNil() {
			return curr, false
		}
		if curr.Kind() != reflect.Map {
			return concreteNil, false
		}

		foundKey := false
		iter := curr.MapRange()
		for iter.Next() {
			if !iter.Key().Equal(reflect.ValueOf(paths[0])) {
				continue
			}

			paths = paths[1:]
			curr = reflect.ValueOf(iter.Value().Interface())
			foundKey = true
			break
		}

		if !foundKey {
			break
		}
	}

	if !found {
		return concreteNil, false
	}
	return curr, true
}
