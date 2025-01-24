package manager

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

var (
	ErrInvalidKey    = errors.New("invalid key")
	ErrInvalidStruct = errors.New("invalid struct type")
	ErrKeyNotFound   = errors.New("key not found")
)

func TraverseKeys(s any, keys []string) (any, error) {
	if len(keys) == 0 {
		return s, nil
	}

	for _, key := range keys {
		v, err := traverse(s, key)
		if err != nil {
			return nil, fmt.Errorf("traversing path %s: %w", strings.Join(keys, "."), err)
		}

		s = v
	}

	return s, nil
}

func TraversePath(s any, path string) (any, error) {
	if path == "" {
		return s, nil
	}

	keys := strings.Split(path, ".")
	return TraverseKeys(s, keys)
}

func traverse(s any, key string) (any, error) {
	switch t := s.(type) {
	case map[string]any:
		v, ok := t[key]
		if !ok {
			return nil, fmt.Errorf("%w %q", ErrKeyNotFound, key)
		}

		return v, nil

	default:
		switch u := reflect.TypeOf(s).Kind(); u {
		case reflect.Map:
			v := reflect.ValueOf(s)
			f := v.MapIndex(reflect.ValueOf(key))
			if f.IsValid() {
				return f.Interface(), nil
			}

			return nil, fmt.Errorf("%w %q", ErrKeyNotFound, key)

		case reflect.Slice:
			v := reflect.ValueOf(s)
			i, err := strconv.Atoi(key)
			if err != nil {
				return nil, fmt.Errorf("%w %q (underlying error %w)", ErrInvalidKey, key, err)
			}
			if i >= 0 && i < v.Len() {
				return v.Index(i).Interface(), nil
			}
			if i < 0 {
				return v.Index(v.Len() + i).Interface(), nil
			}
			return nil, fmt.Errorf("%w %q", ErrKeyNotFound, key)

		case reflect.Struct:
			v := reflect.ValueOf(s)
			f := v.FieldByName(key)
			if f.IsValid() {
				return f.Interface(), nil
			}

			return nil, fmt.Errorf("%w %q", ErrKeyNotFound, key)

		default:
			return nil, fmt.Errorf("%w %T %s", ErrInvalidStruct, s, u)
		}
	}

	panic("unreachable")
}
