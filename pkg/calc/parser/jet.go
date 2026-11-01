package parser

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ripta/reals/pkg/unified"
)

// jv is a scalar or a truncated Taylor series, called a jet. A scalar has co == nil and holds
// its value in re. A jet holds the coefficients of the series in its perturbation tag, lowest
// order first, and each coefficient is itself a jv.
//
// Every diff call takes a fresh tag, and a later call gets a larger one. A jet's coefficients are
// scalars or jets of smaller tags, so a nested diff cannot mistake the outer perturbation for its
// own. All jets of one tag have the same length, because only diff and composeN make them and
// composeN keeps its input's length.
//
// A value whose tag differs from the one an operation works in is constant in that tag.
type jv struct {
	re  *unified.Real
	tag int64
	co  []jv
}

var tagCounter atomic.Int64

func newTag() int64 {
	return tagCounter.Add(1)
}

var errDivideByZero = errors.New("division by zero")

func scalarJ(r *unified.Real) jv {
	return jv{re: r}
}

func intJ(n int64) jv {
	return scalarJ(intReal(big.NewInt(n)))
}

func zeroJ() jv {
	return scalarJ(unified.Zero())
}

// value returns the scalar the jet evaluates to when every perturbation is zero.
func (a jv) value() *unified.Real {
	for a.co != nil {
		a = a.co[0]
	}
	return a.re
}

// coef returns coefficient k of a, read as a jet in tag t. Anything that is not a jet of t is
// constant in t.
func (a jv) coef(t int64, k int) jv {
	if a.co != nil && a.tag == t {
		if k < len(a.co) {
			return a.co[k]
		}
		return zeroJ()
	}
	if k == 0 {
		return a
	}
	return zeroJ()
}

// jetLen returns the coefficient count of the first of vs that is a jet in tag t.
func jetLen(t int64, vs ...jv) int {
	for _, v := range vs {
		if v.co != nil && v.tag == t {
			return len(v.co)
		}
	}
	return 0
}

// jetTag returns the largest tag among vs, or 0 when all are scalars.
func jetTag(vs ...jv) int64 {
	var t int64
	for _, v := range vs {
		t = max(t, v.tag)
	}
	return t
}

func jlin(a, b jv, op func(x, y *unified.Real) *unified.Real) jv {
	t := jetTag(a, b)
	if t == 0 {
		return scalarJ(op(a.re, b.re))
	}

	co := make([]jv, jetLen(t, a, b))
	for k := range co {
		co[k] = jlin(a.coef(t, k), b.coef(t, k), op)
	}
	return jv{tag: t, co: co}
}

func jadd(a, b jv) jv {
	return jlin(a, b, (*unified.Real).Add)
}

func jsub(a, b jv) jv {
	return jlin(a, b, (*unified.Real).Subtract)
}

func jneg(a jv) jv {
	if a.co == nil {
		return scalarJ(a.re.Negate())
	}

	co := make([]jv, len(a.co))
	for k, c := range a.co {
		co[k] = jneg(c)
	}
	return jv{tag: a.tag, co: co}
}

// jmul multiplies two jets. Jets in the same tag use the Cauchy product.
func jmul(a, b jv) jv {
	t := jetTag(a, b)
	if t == 0 {
		return scalarJ(a.re.Multiply(b.re))
	}

	co := make([]jv, jetLen(t, a, b))
	for k := range co {
		switch {
		case a.tag != t:
			co[k] = jmul(a, b.co[k])
		case b.tag != t:
			co[k] = jmul(a.co[k], b)
		default:
			acc := jmul(a.co[0], b.coef(t, k))
			for i := 1; i <= k; i++ {
				acc = jadd(acc, jmul(a.co[i], b.coef(t, k-i)))
			}
			co[k] = acc
		}
	}
	return jv{tag: t, co: co}
}

// jdiv divides two jets. It fails when the divisor's value is an exact zero.
func jdiv(a, b jv) (jv, error) {
	t := jetTag(a, b)
	if t == 0 {
		if b.re.IsZero() {
			return jv{}, errDivideByZero
		}
		return scalarJ(a.re.Divide(b.re)), nil
	}

	co := make([]jv, jetLen(t, a, b))
	for k := range co {
		var err error
		if b.tag != t {
			co[k], err = jdiv(a.co[k], b)
		} else {
			// q_k = (a_k - sum_{j=1..k} b_j q_{k-j}) / b_0
			num := a.coef(t, k)
			for j := 1; j <= k; j++ {
				num = jsub(num, jmul(b.co[j], co[k-j]))
			}
			co[k], err = jdiv(num, b.co[0])
		}
		if err != nil {
			return jv{}, err
		}
	}
	return jv{tag: t, co: co}, nil
}

// jtrunc keeps the first n coefficients of a in tag t.
func jtrunc(a jv, t int64, n int) jv {
	if a.co != nil && a.tag == t && len(a.co) > n {
		return jv{tag: t, co: a.co[:n]}
	}
	return a
}

// jderiv is the formal derivative of a in tag t, with one coefficient fewer.
func jderiv(a jv, t int64) jv {
	if a.co == nil || a.tag != t {
		return zeroJ()
	}

	co := make([]jv, len(a.co)-1)
	for j := range co {
		co[j] = jmul(a.co[j+1], intJ(int64(j+1)))
	}
	return jv{tag: t, co: co}
}

// partialsFn returns the partial derivative of a catalog entry in each argument, as jets of the
// same operations the entry's value uses. Its arguments are one order lower than the jet being
// built, which is what ends the recursion between an entry and the entries its partials use.
type partialsFn func(ctx context.Context, env *Env, args []jv) ([]jv, error)

// derivFn is a partialsFn for one argument.
type derivFn func(ctx context.Context, env *Env, u jv) (jv, error)

func unaryPartials(d derivFn) partialsFn {
	return func(ctx context.Context, env *Env, args []jv) ([]jv, error) {
		p, err := d(ctx, env, args[0])
		if err != nil {
			return nil, err
		}
		return []jv{p}, nil
	}
}

// composeN applies a function to jets. The value is the function at the coefficient-0 arguments.
// Coefficient k comes from k*f_k = sum_j j*u_j*g_{k-j}, which is the chain rule on the series
// with g the partial derivative. Arguments constant in the top tag contribute no derivative.
func composeN(ctx context.Context, env *Env, name string, value func(context.Context, *Env, []*unified.Real) (*unified.Real, error), partials partialsFn, args []jv) (jv, error) {
	t := jetTag(args...)
	if t == 0 {
		reals := make([]*unified.Real, len(args))
		for i, a := range args {
			reals[i] = a.re
		}
		r, err := value(ctx, env, reals)
		if err != nil {
			return jv{}, domainError(ctx, name, reals, env.precision, err)
		}
		return scalarJ(r), nil
	}

	zeroth := make([]jv, len(args))
	for i, a := range args {
		zeroth[i] = a.coef(t, 0)
	}
	f0, err := composeN(ctx, env, name, value, partials, zeroth)
	if err != nil {
		return jv{}, err
	}

	n := jetLen(t, args...) - 1
	co := make([]jv, n+1)
	co[0] = f0
	if n == 0 {
		return jv{tag: t, co: co}, nil
	}

	lower := make([]jv, len(args))
	for i, a := range args {
		lower[i] = jtrunc(a, t, n)
	}
	ps, err := partials(ctx, env, lower)
	if err != nil {
		return jv{}, err
	}
	if len(ps) != len(args) {
		return jv{}, fmt.Errorf("%s: derivative rule returned %d partials for %d arguments", name, len(ps), len(args))
	}

	g := zeroJ()
	for i, a := range args {
		if a.tag == t {
			g = jadd(g, jmul(ps[i], jderiv(a, t)))
		}
	}
	for k := 1; k <= n; k++ {
		co[k] = jmul(g.coef(t, k-1), scalarJ(ratReal(big.NewRat(1, int64(k)))))
	}
	return jv{tag: t, co: co}, nil
}
