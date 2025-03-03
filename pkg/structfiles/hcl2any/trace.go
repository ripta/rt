package hcl2any

import (
	"fmt"
	"strings"
)

type keyTrace struct {
	Keys []string
	Err  error
}

func (kt *keyTrace) Error() string {
	return fmt.Sprintf("at path %q: %s", strings.Join(kt.Keys, "."), kt.Err.Error())
}

func withKeyTrace(err error, keys ...string) *keyTrace {
	if err == nil {
		return nil
	}

	if kt, ok := err.(*keyTrace); ok {
		kt.Keys = append(keys, kt.Keys...)
		return kt
	}

	return &keyTrace{
		Keys: keys,
		Err:  err,
	}
}
