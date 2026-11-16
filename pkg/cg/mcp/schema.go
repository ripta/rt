package mcp

import (
	"fmt"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// requiredInputSchema reflects In's argument schema the way the SDK would, then removes null
// from every required argument and its nested items.
//
// The reflector marks every Go slice nullable because a nil slice encodes as null. On a
// required argument that tells callers null is acceptable, when the handler rejects it.
// Optional arguments keep null, which means the same as leaving them out.
func requiredInputSchema[In any]() *jsonschema.Schema {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Errorf("reflecting input schema for %T: %w", *new(In), err))
	}

	for _, name := range s.Required {
		for p := s.Properties[name]; p != nil; p = p.Items {
			dropNull(p)
		}
	}

	return s
}

func dropNull(s *jsonschema.Schema) {
	if len(s.Types) == 0 {
		return
	}

	types := slices.DeleteFunc(slices.Clone(s.Types), func(t string) bool { return t == "null" })
	if len(types) == 1 {
		s.Type = types[0]
		s.Types = nil
		return
	}

	s.Types = types
}
