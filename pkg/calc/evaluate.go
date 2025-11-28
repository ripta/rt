package calc

import (
	"strings"

	"github.com/ripta/rt/pkg/calc/parser"
	"github.com/ripta/rt/pkg/num"
)

func Evaluate(expr string) (*num.Num, error) {
	return evaluate(expr, parser.NewEnv())
}

func evaluate(expr string, env *parser.Env) (*num.Num, error) {
	expr = strings.TrimSpace(expr)
	if env == nil {
		env = parser.NewEnv()
	}
	if expr == "" {
		return num.Zero(), nil
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

	return num.FromFloat64(val), nil
}
