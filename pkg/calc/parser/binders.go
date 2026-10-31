package parser

import (
	"context"
	"errors"
	"math/big"

	"github.com/ripta/reals/pkg/unified"
)

// errBinderBoundDomain reports a sum or product bound that is not an integer at the active
// precision.
var errBinderBoundDomain = errors.New("range bounds must be integers")

func sumBinder(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	return foldRange(ctx, env, body[0], bind, intReal(big.NewInt(0)), (*unified.Real).Add)
}

func productBinder(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	return foldRange(ctx, env, body[0], bind, intReal(big.NewInt(1)), (*unified.Real).Multiply)
}

// foldRange combines the body's value at each integer index from lo to hi, starting from the
// identity. A reversed range returns the identity.
//
// Bodies are evaluated eagerly, one index at a time. A lazy evaluation inside a reals
// approximation callback would skip the budget and the context checks.
func foldRange(ctx context.Context, env *Env, body Node, bind Binding, identity *unified.Real, combine func(*unified.Real, *unified.Real) *unified.Real) (*unified.Real, error) {
	lo, err := integerBound(ctx, bind.Lo, env.precision)
	if err != nil {
		return nil, err
	}
	hi, err := integerBound(ctx, bind.Hi, env.precision)
	if err != nil {
		return nil, err
	}

	// Adjacent terms merge like a binary counter, so the result tree is O(log n) deep. A left-deep
	// chain of n terms makes the final approximation cost grow much faster than n.
	type partial struct {
		val   *unified.Real
		level int
	}
	var stack []partial

	one := big.NewInt(1)
	for i := new(big.Int).Set(lo); i.Cmp(hi) <= 0; i.Add(i, one) {
		v, err := evalBody(ctx, env, body, bind.Name, intReal(i))
		if err != nil {
			return nil, err
		}

		cur := partial{val: v}
		for n := len(stack); n > 0 && stack[n-1].level == cur.level; n = len(stack) {
			cur = partial{val: combine(stack[n-1].val, cur.val), level: cur.level + 1}
			stack = stack[:n-1]
		}
		stack = append(stack, cur)
	}

	acc := identity
	for _, p := range stack {
		acc = combine(acc, p.val)
	}
	return acc, nil
}

func integerBound(ctx context.Context, b Bound, precision int) (*big.Int, error) {
	rat, err := approxRat(ctx, b.Value, precision)
	if err != nil {
		return nil, err
	}
	if !rat.IsInt() {
		return nil, errBinderBoundDomain
	}
	return rat.Num(), nil
}
