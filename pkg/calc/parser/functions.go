package parser

import (
	"fmt"

	"github.com/ripta/reals/pkg/unified"
)

// function is a registry entry. minArgs and maxArgs bound the accepted argument
// count; maxArgs of -1 means unbounded variadic. fn receives the environment so
// implementations that decide irrational operands at a binary precision can read
// env.precision.
type function struct {
	minArgs int
	maxArgs int
	fn      func(env *Env, args []*unified.Real) (*unified.Real, error)
}

// functions is the registry consulted by CallNode. Names share the identifier
// namespace with variables and constants but are only looked up in call
// position, so a variable named sin and the sin function coexist.
var functions = map[string]function{
	"abs":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Abs(), nil }},
	"sin":  {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sin(), nil }},
	"sqrt": {1, 1, func(_ *Env, a []*unified.Real) (*unified.Real, error) { return a[0].Sqrt() }},
}

// arityError formats a message describing the accepted argument count against
// the count actually supplied.
func arityError(f function, got int) string {
	switch {
	case f.maxArgs < 0:
		return fmt.Sprintf("expects at least %s, got %d", plural(f.minArgs), got)
	case f.minArgs == f.maxArgs:
		return fmt.Sprintf("expects %s, got %d", plural(f.minArgs), got)
	default:
		return fmt.Sprintf("expects %d to %d arguments, got %d", f.minArgs, f.maxArgs, got)
	}
}

func plural(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}
