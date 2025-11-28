package calc

import (
	"strings"

	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/parser"
)

func Evaluate(expr string) (*unified.Real, error) {
	return evaluate(expr, parser.NewEnv())
}

func evaluate(expr string, env *parser.Env) (*unified.Real, error) {
	expr = strings.TrimSpace(expr)
	if env == nil {
		env = parser.NewEnv()
	}
	if expr == "" {
		return unified.Zero(), nil
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
