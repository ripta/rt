package parser

import (
	"context"
	"errors"
	"math/big"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
)

// The functions here must not read functionCatalog or functions, which would be an
// initialization cycle: the catalog holds these rules. Each entry the rules recurse into has its
// own value function and jet function instead.

type valueFn = func(context.Context, *Env, []*unified.Real) (*unified.Real, error)

func real1(f func(*unified.Real) *unified.Real) valueFn {
	return func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
		return f(a[0]), nil
	}
}

var valSin = real1((*unified.Real).Sin)

var valCos = real1((*unified.Real).Cos)

var valTan = real1((*unified.Real).Tan)

var valExp = real1((*unified.Real).Exp)

var valSinh = real1((*unified.Real).Sinh)

var valCosh = real1((*unified.Real).Cosh)

var valTanh = real1((*unified.Real).Tanh)

var valCbrt = real1((*unified.Real).Cbrt)

func valSqrt(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
	return a[0].Sqrt(ctx)
}

func valLn(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
	return a[0].Ln(ctx)
}

func valGamma(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
	return a[0].Gamma()
}

func valHypot(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
	return hypot(ctx, a[0], a[1])
}

func valNorm(ctx context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
	return norm(ctx, a)
}

func valPolygamma(m int) valueFn {
	return func(_ context.Context, _ *Env, a []*unified.Real) (*unified.Real, error) {
		return a[0].Polygamma(m)
	}
}

func jetSin(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "sin", valSin, unaryPartials(dSin), []jv{u})
}

func jetCos(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "cos", valCos, unaryPartials(dCos), []jv{u})
}

func jetTan(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "tan", valTan, unaryPartials(dTan), []jv{u})
}

func jetExp(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "exp", valExp, unaryPartials(dExp), []jv{u})
}

func jetLn(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "ln", valLn, unaryPartials(dLn), []jv{u})
}

func jetSqrt(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "sqrt", valSqrt, unaryPartials(dSqrt), []jv{u})
}

func jetCbrt(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "cbrt", valCbrt, unaryPartials(dCbrt), []jv{u})
}

func jetSinh(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "sinh", valSinh, unaryPartials(dSinh), []jv{u})
}

func jetCosh(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "cosh", valCosh, unaryPartials(dCosh), []jv{u})
}

func jetTanh(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "tanh", valTanh, unaryPartials(dTanh), []jv{u})
}

func jetGamma(ctx context.Context, env *Env, u jv) (jv, error) {
	return composeN(ctx, env, "gamma", valGamma, unaryPartials(dGamma), []jv{u})
}

// jetPolygamma is the internal polygamma of order m, whose rule is the polygamma of order m+1.
// gamma and lgamma reach it through digamma, which is order 0. Errors carry the name of the entry
// the user wrote.
func jetPolygamma(ctx context.Context, env *Env, name string, m int, u jv) (jv, error) {
	rule := func(ctx context.Context, env *Env, u jv) (jv, error) {
		return jetPolygamma(ctx, env, name, m+1, u)
	}
	return composeN(ctx, env, name, valPolygamma(m), unaryPartials(rule), []jv{u})
}

func jetHypot(ctx context.Context, env *Env, x, y jv) (jv, error) {
	return composeN(ctx, env, "hypot", valHypot, dHypot, []jv{x, y})
}

func jetNorm(ctx context.Context, env *Env, components []jv) (jv, error) {
	return composeN(ctx, env, "norm", valNorm, dNorm, components)
}

func dSin(ctx context.Context, env *Env, u jv) (jv, error) {
	return jetCos(ctx, env, u)
}

func dCos(ctx context.Context, env *Env, u jv) (jv, error) {
	s, err := jetSin(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	return jneg(s), nil
}

func dTan(ctx context.Context, env *Env, u jv) (jv, error) {
	t, err := jetTan(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	return jadd(intJ(1), jmul(t, t)), nil
}

func dExp(ctx context.Context, env *Env, u jv) (jv, error) {
	return jetExp(ctx, env, u)
}

func dLn(_ context.Context, _ *Env, u jv) (jv, error) {
	return jdiv(intJ(1), u)
}

func dSqrt(ctx context.Context, env *Env, u jv) (jv, error) {
	s, err := jetSqrt(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	return jdiv(intJ(1), jmul(intJ(2), s))
}

func dCbrt(ctx context.Context, env *Env, u jv) (jv, error) {
	c, err := jetCbrt(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	return jdiv(intJ(1), jmul(intJ(3), jmul(c, c)))
}

func dSinh(ctx context.Context, env *Env, u jv) (jv, error) {
	return jetCosh(ctx, env, u)
}

func dCosh(ctx context.Context, env *Env, u jv) (jv, error) {
	return jetSinh(ctx, env, u)
}

func dTanh(ctx context.Context, env *Env, u jv) (jv, error) {
	t, err := jetTanh(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	return jsub(intJ(1), jmul(t, t)), nil
}

// dAsin is 1/sqrt(1-u^2).
func dAsin(ctx context.Context, env *Env, u jv) (jv, error) {
	r, err := jetSqrt(ctx, env, jsub(intJ(1), jmul(u, u)))
	if err != nil {
		return jv{}, err
	}
	return jdiv(intJ(1), r)
}

func dAcos(ctx context.Context, env *Env, u jv) (jv, error) {
	d, err := dAsin(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	return jneg(d), nil
}

func dAtan(_ context.Context, _ *Env, u jv) (jv, error) {
	return jdiv(intJ(1), jadd(intJ(1), jmul(u, u)))
}

func dAsinh(ctx context.Context, env *Env, u jv) (jv, error) {
	r, err := jetSqrt(ctx, env, jadd(jmul(u, u), intJ(1)))
	if err != nil {
		return jv{}, err
	}
	return jdiv(intJ(1), r)
}

func dAcosh(ctx context.Context, env *Env, u jv) (jv, error) {
	r, err := jetSqrt(ctx, env, jsub(jmul(u, u), intJ(1)))
	if err != nil {
		return jv{}, err
	}
	return jdiv(intJ(1), r)
}

func dAtanh(_ context.Context, _ *Env, u jv) (jv, error) {
	return jdiv(intJ(1), jsub(intJ(1), jmul(u, u)))
}

func dDeg2rad(_ context.Context, _ *Env, _ jv) (jv, error) {
	return jdiv(scalarJ(unified.Pi()), intJ(180))
}

func dRad2deg(_ context.Context, _ *Env, _ jv) (jv, error) {
	return jdiv(intJ(180), scalarJ(unified.Pi()))
}

func dLog10(ctx context.Context, _ *Env, u jv) (jv, error) {
	ln10, err := intReal(big.NewInt(10)).Ln(ctx)
	if err != nil {
		return jv{}, err
	}
	return jdiv(intJ(1), jmul(u, scalarJ(ln10)))
}

func dLog2(_ context.Context, _ *Env, u jv) (jv, error) {
	return jdiv(intJ(1), jmul(u, scalarJ(unified.Ln2())))
}

// dLog is the pair of partials of log(x, base) = ln(x) / ln(base).
func dLog(ctx context.Context, env *Env, args []jv) ([]jv, error) {
	x, base := args[0], args[1]
	lnx, err := jetLn(ctx, env, x)
	if err != nil {
		return nil, err
	}
	lnb, err := jetLn(ctx, env, base)
	if err != nil {
		return nil, err
	}

	dx, err := jdiv(intJ(1), jmul(x, lnb))
	if err != nil {
		return nil, err
	}
	db, err := jdiv(jneg(lnx), jmul(base, jmul(lnb, lnb)))
	if err != nil {
		return nil, err
	}
	return []jv{dx, db}, nil
}

// dAtan2 is the pair of partials of atan2(y, x).
func dAtan2(_ context.Context, _ *Env, args []jv) ([]jv, error) {
	y, x := args[0], args[1]
	den := jadd(jmul(x, x), jmul(y, y))
	dy, err := jdiv(x, den)
	if err != nil {
		return nil, err
	}
	dx, err := jdiv(jneg(y), den)
	if err != nil {
		return nil, err
	}
	return []jv{dy, dx}, nil
}

func dHypot(ctx context.Context, env *Env, args []jv) ([]jv, error) {
	return dNorm(ctx, env, args)
}

// dNorm is c_i / norm for each component. norm itself is the jet of the lower order.
func dNorm(ctx context.Context, env *Env, args []jv) ([]jv, error) {
	h, err := jetNorm(ctx, env, args)
	if err != nil {
		return nil, err
	}

	out := make([]jv, len(args))
	for i, c := range args {
		if out[i], err = jdiv(c, h); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// dDist is the partials of dist(x1, y1, x2, y2) = hypot(x2 - x1, y2 - y1).
func dDist(ctx context.Context, env *Env, args []jv) ([]jv, error) {
	dx, dy := jsub(args[2], args[0]), jsub(args[3], args[1])
	h, err := jetHypot(ctx, env, dx, dy)
	if err != nil {
		return nil, err
	}

	px, err := jdiv(dx, h)
	if err != nil {
		return nil, err
	}
	py, err := jdiv(dy, h)
	if err != nil {
		return nil, err
	}
	return []jv{jneg(px), jneg(py), px, py}, nil
}

// dGamma is gamma(u) * digamma(u).
func dGamma(ctx context.Context, env *Env, u jv) (jv, error) {
	g, err := jetGamma(ctx, env, u)
	if err != nil {
		return jv{}, err
	}
	d, err := jetPolygamma(ctx, env, "gamma", 0, u)
	if err != nil {
		return jv{}, err
	}
	return jmul(g, d), nil
}

// dLgamma is digamma(u).
func dLgamma(ctx context.Context, env *Env, u jv) (jv, error) {
	return jetPolygamma(ctx, env, "lgamma", 0, u)
}

var (
	errZeroNegativePower  = errors.New("zero to negative power is undefined")
	errNegativeNonInteger = errors.New("negative base to non-integer power is non-real")
	errPowerTooLarge      = errors.New("exponentiation result is too large for the precision limit")
)

// valPow is the value of l**r for every caller. unified.Pow detects an integer exponent
// structurally, so a base like SQRT2 raised to 2 stays exact.
func valPow(ctx context.Context, env *Env, l, r *unified.Real) (*unified.Real, error) {
	res, err := l.Pow(ctx, r)
	if err == nil {
		return res, nil
	}
	if stop := checkCtx(ctx); stop != nil {
		return nil, stop
	}

	switch {
	case errors.Is(err, constructive.ErrPrecisionOverflow):
		return nil, errPowerTooLarge

	case errors.Is(err, unified.ErrNonPositive):
		base, err := approxRat(ctx, l, env.precision)
		if err != nil {
			return nil, err
		}
		if base.Sign() < 0 {
			return nil, errNegativeNonInteger
		}

		// A zero base reaches here for a negative or non-integer exponent.
		exp, err := approxRat(ctx, r, env.precision)
		if err != nil {
			return nil, err
		}
		if exp.Sign() < 0 {
			return nil, errZeroNegativePower
		}
		return unified.Zero(), nil
	}
	return nil, err
}

// integerExponent returns e as an integer when e is structurally one. Raising -1 to e succeeds
// only for an integer, which is the test unified.Pow itself applies.
func integerExponent(ctx context.Context, env *Env, e *unified.Real) (*big.Int, bool, error) {
	if _, err := intReal(big.NewInt(-1)).Pow(ctx, e); err != nil {
		if errors.Is(err, unified.ErrNonPositive) {
			return nil, false, nil
		}
		if stop := checkCtx(ctx); stop != nil {
			return nil, false, stop
		}
		return nil, false, err
	}

	rat, err := approxRat(ctx, e, env.precision)
	if err != nil {
		return nil, false, err
	}
	return rat.Num(), true, nil
}

// jetPow raises a jet to a jet power. Coefficient 0 is always valPow of the coefficients 0.
func jetPow(ctx context.Context, env *Env, base, exp jv) (jv, error) {
	if jetTag(base, exp) == 0 {
		r, err := valPow(ctx, env, base.re, exp.re)
		return scalarJ(r), err
	}

	if exp.co != nil {
		return jetPowGeneral(ctx, env, base, exp)
	}

	n, ok, err := integerExponent(ctx, env, exp.re)
	if err != nil {
		return jv{}, err
	}
	if ok {
		return jetPowInteger(ctx, env, base, exp, n)
	}
	return jetPowReal(ctx, env, base, exp)
}

// jetPowGeneral is exp(v ln u). It needs a positive base. Coefficient 0 is replaced by the power
// of the coefficients 0, because exp(v ln u) is inexact where the power is not.
func jetPowGeneral(ctx context.Context, env *Env, base, exp jv) (jv, error) {
	lnu, err := jetLn(ctx, env, base)
	if err != nil {
		return jv{}, err
	}
	res, err := jetExp(ctx, env, jmul(exp, lnu))
	if err != nil {
		return jv{}, err
	}

	t := jetTag(base, exp)
	if res.tag != t || res.co == nil {
		return res, nil
	}
	p0, err := jetPow(ctx, env, base.coef(t, 0), exp.coef(t, 0))
	if err != nil {
		return jv{}, err
	}
	co := append([]jv{p0}, res.co[1:]...)
	return jv{tag: t, co: co}, nil
}

// jetPowInteger multiplies the series, so it holds at a zero or negative base.
func jetPowInteger(ctx context.Context, env *Env, base, exp jv, n *big.Int) (jv, error) {
	if n.Sign() == 0 {
		return intJ(1), nil
	}

	t := base.tag
	p0, err := jetPow(ctx, env, base.coef(t, 0), exp)
	if err != nil {
		return jv{}, err
	}

	acc, sq := intJ(1), base
	bits := n.BitLen()
	for i := 0; i < bits; i++ {
		if err := checkCtx(ctx); err != nil {
			return jv{}, err
		}
		if n.Bit(i) == 1 {
			acc = jmul(acc, sq)
		}
		if i+1 < bits {
			sq = jmul(sq, sq)
		}
	}
	if n.Sign() < 0 {
		if acc, err = jdiv(intJ(1), acc); err != nil {
			return jv{}, err
		}
	}

	co := append([]jv{p0}, acc.co[1:]...)
	return jv{tag: t, co: co}, nil
}

// jetPowReal is Miller's recurrence for a constant non-integer exponent. It divides by the
// coefficient-0 base, so it needs a positive one.
func jetPowReal(ctx context.Context, env *Env, base, exp jv) (jv, error) {
	t := base.tag
	u := base.co

	p := make([]jv, len(u))
	var err error
	if p[0], err = jetPow(ctx, env, u[0], exp); err != nil {
		return jv{}, err
	}

	for k := 1; k < len(u); k++ {
		if err := checkCtx(ctx); err != nil {
			return jv{}, err
		}

		sum := zeroJ()
		for j := 1; j <= k; j++ {
			w := jsub(jmul(exp, intJ(int64(j))), intJ(int64(k-j)))
			sum = jadd(sum, jmul(w, jmul(u[j], p[k-j])))
		}
		if p[k], err = jdiv(sum, jmul(intJ(int64(k)), u[0])); err != nil {
			return jv{}, err
		}
	}
	return jv{tag: t, co: p}, nil
}
