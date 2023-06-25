package program

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

type Package struct {
	expression string
	objectName string
	compiled   cel.Program
}

func New(objectName, expression string) (*Package, error) {
	object := cel.Variable(objectName, cel.MapType(cel.StringType, cel.DynType))
	env, err := cel.NewEnv(object)
	if err != nil {
		return nil, err
	}

	ast, res := env.Compile(expression)
	if res != nil && res.Err() != nil {
		return nil, res.Err()
	}

	p, err := env.Program(ast)
	if err != nil {
		return nil, err
	}

	return &Package{
		expression: expression,
		objectName: objectName,
		compiled:   p,
	}, nil
}

func (p *Package) Run(obj any) (ref.Val, *cel.EvalDetails, error) {
	return p.compiled.Eval(map[string]any{
		p.objectName: obj,
	})
}
