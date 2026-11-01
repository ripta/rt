package parser

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/tokens"
)

// jetNode is a node that evaluates to a jet. Only the body of a diff uses it. Eval stays the one
// path every other evaluation takes.
type jetNode interface {
	evalJet(ctx context.Context, env *Env) (jv, error)
}

func evalJet(ctx context.Context, env *Env, n Node) (jv, error) {
	j, ok := n.(jetNode)
	if !ok {
		return jv{}, fmt.Errorf("%T cannot be differentiated", n)
	}
	return j.evalJet(ctx, env)
}

func notDifferentiable(pos tokens.Position, name, reason string) error {
	return fmt.Errorf("%s: %s is not yet differentiable inside diff (%s)", pos, name, reason)
}

func (n *NumberNode) evalJet(context.Context, *Env) (jv, error) {
	return scalarJ(n.Value), nil
}

func (n *IdentNode) evalJet(_ context.Context, env *Env) (jv, error) {
	if env != nil {
		if j, ok := env.getJet(n.Name.Value); ok {
			return j, nil
		}
	}

	// Eval words the error for an undefined name.
	_, err := n.Eval(context.Background(), env)
	return jv{}, err
}

func (n *CommentNode) evalJet(ctx context.Context, env *Env) (jv, error) {
	if env.trace && env.traceOut != nil {
		fmt.Fprintf(env.traceOut, "# %s\n", n.Text)
	}
	return evalJet(ctx, env, n.Expr)
}

func (n *UnaryNode) evalJet(ctx context.Context, env *Env) (jv, error) {
	u, err := evalJet(ctx, env, n.Expr)
	if err != nil {
		return jv{}, err
	}
	if u.co == nil {
		r, err := n.apply(u.re)
		return scalarJ(r), err
	}

	switch n.Op.Type {
	case tokens.OP_MINUS:
		return jneg(u), nil

	case tokens.OP_ROOT:
		// Eval takes this root without a domain check, so the value does the same.
		value := func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) { return n.apply(a[0]) }
		return composeN(ctx, env, n.Op.Value, value, unaryPartials(dSqrt), []jv{u})

	default:
		return jv{}, fmt.Errorf("unknown unary operator")
	}
}

func (n *BinaryNode) evalJet(ctx context.Context, env *Env) (jv, error) {
	l, err := evalJet(ctx, env, n.Left)
	if err != nil {
		return jv{}, err
	}
	r, err := evalJet(ctx, env, n.Right)
	if err != nil {
		return jv{}, err
	}
	if l.co == nil && r.co == nil {
		v, err := n.apply(ctx, env, l.re, r.re)
		return scalarJ(v), err
	}

	switch n.Op.Type {
	case tokens.OP_PLUS:
		return jadd(l, r), nil

	case tokens.OP_MINUS:
		return jsub(l, r), nil

	case tokens.OP_STAR:
		return jmul(l, r), nil

	case tokens.OP_SLASH:
		return jdiv(l, r)

	case tokens.OP_POW:
		return jetPow(ctx, env, l, r)

	case tokens.OP_PERCENT, tokens.OP_SHL, tokens.OP_SHR:
		return jv{}, notDifferentiable(n.Op.Pos, n.Op.Value, "not smooth or integer-only")

	default:
		return jv{}, fmt.Errorf("unknown operator")
	}
}

func (n *CallNode) evalJet(ctx context.Context, env *Env) (jv, error) {
	table := n.catalog
	if table == nil {
		table = functions
	}

	fn, ok := table[n.Func.Value]
	if !ok {
		return jv{}, fmt.Errorf("%s: unknown function %q", n.Func.Pos, n.Func.Value)
	}
	if fn.binder != nil {
		return jv{}, notDifferentiable(n.Func.Pos, n.Func.Value, fn.notDiff)
	}

	args := make([]jv, len(n.Args))
	live := false
	for i, a := range n.Args {
		var err error
		if args[i], err = evalJet(ctx, env, a); err != nil {
			return jv{}, err
		}
		live = live || args[i].co != nil
	}
	if live && fn.partials == nil {
		return jv{}, notDifferentiable(n.Func.Pos, n.Func.Value, fn.notDiff)
	}

	// An entry with no rule still has a value when no argument depends on the variable.
	value, partials := fn.fn, fn.partials
	if !live {
		partials = nil
	}
	result, err := composeN(ctx, env, n.Func.Value, value, partials, args)
	if err != nil {
		if isStop(err) {
			return jv{}, err
		}
		return jv{}, fmt.Errorf("%s: %w", n.Func.Pos, err)
	}
	return result, nil
}

// maxDiffOrder is the order diff takes the derivative to.
const maxDiffOrder = 1

func diffBinder(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	return diffAt(ctx, env, body[0], bind.Name, bind.Lo.Value, maxDiffOrder)
}

// diffAt returns the order-th derivative of body at name = at. The body is evaluated once, with
// name bound to the jet at + e in a fresh tag.
func diffAt(ctx context.Context, env *Env, body Node, name string, at *unified.Real, order int) (*unified.Real, error) {
	t := newTag()

	co := make([]jv, order+1)
	for k := range co {
		co[k] = zeroJ()
	}
	co[0] = scalarJ(at)
	if order >= 1 {
		co[1] = intJ(1)
	}

	r, err := evalBodyJet(ctx, env, body, name, jv{tag: t, co: co})
	if err != nil {
		return nil, err
	}

	// The k-th derivative is k! times the k-th Taylor coefficient.
	result := r.coef(t, order).value()
	for k := 2; k <= order; k++ {
		result = result.Multiply(intReal(big.NewInt(int64(k))))
	}
	return result, nil
}
