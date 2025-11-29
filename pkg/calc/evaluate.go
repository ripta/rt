package calc

import (
	"errors"
	"strings"

	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/parser"
)

var ErrEnvironmentMissing = errors.New("environment missing")

// Evaluate parses expr and evaluates it in the given environment.
func Evaluate(expr string, env *parser.Env) (*unified.Real, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return unified.Zero(), nil
	}

	if env == nil {
		return nil, ErrEnvironmentMissing
	}

	p := parser.New("(eval)", expr)
	node, err := p.Parse()
	if err != nil {
		return nil, err
	}

	val, err := node.Eval(env)
	if err != nil {
		return nil, err
	}

	return val, nil
}
