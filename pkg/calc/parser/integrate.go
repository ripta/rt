package parser

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
)

const (
	// integrateGuardBits is how many bits finer than the requested precision a run works, so that
	// the rounding of thousands of samples cannot reach the last requested bit.
	integrateGuardBits = 32

	// integrateReadSlack is how many bits finer than the active precision the eager run converges,
	// so a read at the active precision is served from it.
	integrateReadSlack = 8

	// integrateMaxLevels is the number of step halvings a run tries before it gives up.
	integrateMaxLevels = 12

	// integrateMinLevels is the first level whose estimate is compared with its predecessor.
	// The first levels are too coarse for agreement to mean anything.
	integrateMinLevels = 2

	// integrateTailFactor bounds how close to an endpoint a node may sit. A node is at most
	// 2^(integrateTailFactor * |working precision|) times nearer than the range is wide. It
	// ends a divergent integrand such as 1/x, whose tail never falls below the threshold.
	integrateTailFactor = 4
)

// errTailCap ends a run whose integrand is still above the tail threshold at the cap on t.
var errTailCap = errors.New("the tail does not decay before the cap on t")

func integrateBinder(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	lo, hi := bind.Lo.Value, bind.Hi.Value

	// The order of the bounds is decided once, so every precision follows the same orientation.
	// Only bounds that are provably equal give an exact zero. Bounds that cannot be told apart
	// might still differ by a width that a huge integrand turns into a large value.
	sign, err := nearZeroSign(ctx, hi.Subtract(lo), env.precision)
	switch {
	case errors.Is(err, errNearZero):
		if sameBound(lo, hi) {
			return unified.Zero(), nil
		}
		return nil, fmt.Errorf("the bounds of integrate are too close to tell apart at this precision. Raise the precision or give bounds that differ by more than 2^%d", env.precision)
	case err != nil:
		return nil, err
	case sign == 0:
		return unified.Zero(), nil
	}

	q := &integral{
		env:  snapshotEnv(env, body[0], bind.Name),
		body: body[0],
		name: bind.Name,
		neg:  sign < 0,
	}
	if q.neg {
		lo, hi = hi, lo
	}
	q.lo, q.hi = lo, hi

	// The first estimate is eager, so its errors land on the calling line. The lazy value then
	// serves it from the cache. Reading a value at p asks the approximator for a few bits finer.
	if _, err := q.approximate(ctx, env.precision-integrateReadSlack); err != nil {
		return nil, err
	}

	return unified.New(constructive.FromApproximation("integrate", q.approximate), nil), nil
}

// sameBound reports whether two bounds are the same value by construction, such as PI and PI.
func sameBound(a, b *unified.Real) bool {
	return constructive.AsConstruction(a.Constructive()) == constructive.AsConstruction(b.Constructive())
}

// integral is the estimate of a body over lo..hi, with lo below hi. neg is set when the bounds
// were given in the other order.
//
// The estimate is tanh-sinh quadrature. Nodes sit at x = c + r*tanh(pi/2*sinh t) on t = k*h,
// with h halved at each level, so every level reuses all earlier samples. A node is placed as the
// nearer endpoint plus or minus d = 2r/(E+1), where E = exp(pi*sinh t). A node a trillionth of
// the way from an endpoint thus keeps full relative precision and never rounds onto the endpoint.
type integral struct {
	env  *Env
	body Node
	name string

	lo, hi *unified.Real
	neg    bool

	// est was converged at estP. A request at estP or coarser is rounded from it, so the first
	// display does not run the quadrature again.
	est  *big.Rat
	estP int
}

func (q *integral) approximate(ctx context.Context, p int) (*big.Int, error) {
	if q.est == nil || p < q.estP {
		est, err := q.run(ctx, p)
		if err != nil {
			return nil, err
		}
		q.est, q.estP = est, p
	}
	return roundToBits(q.est, p), nil
}

// run estimates the integral to within 2^p, stopping when two successive levels agree to that.
func (q *integral) run(ctx context.Context, p int) (*big.Rat, error) {
	wp := p - integrateGuardBits
	a, err := approxRat(ctx, q.lo, wp)
	if err != nil {
		return nil, err
	}
	b, err := approxRat(ctx, q.hi, wp)
	if err != nil {
		return nil, err
	}
	if a.Cmp(b) >= 0 {
		return nil, fmt.Errorf("the bounds of integrate are too close to tell apart at precision %d", p)
	}

	r := new(big.Rat).Sub(b, a)
	r.Quo(r, big.NewRat(2, 1))
	// A wide range scales every term up.
	wp -= max(0, r.Num().BitLen()-r.Denom().BitLen())

	run := &quadrature{
		q:    q,
		env:  q.env.withPrecision(wp),
		a:    a,
		b:    b,
		r:    r,
		wp:   wp,
		need: -wp + 8,
		thr:  new(big.Int).Lsh(big.NewInt(1), uint(max(0, p-integrateGuardBits-wp))),
		tmax: math.Asinh(math.Ln2 * float64(integrateTailFactor*max(-wp, 64)) / math.Pi),
		sum:  new(big.Int),
	}
	if run.pi, err = approxConstructive(ctx, constructive.Pi(), wp); err != nil {
		return nil, err
	}

	tol := ratPow2(p)
	var prev, cur *big.Rat
	for level := 0; level <= integrateMaxLevels; level++ {
		if err := run.level(ctx, level); err != nil {
			if errors.Is(err, errTailCap) {
				return nil, fmt.Errorf("integrate did not converge on %s = %s..%s: the integrand is still too large at the cap on t, %d bits nearer an endpoint than the range is wide. It may diverge or decay too slowly there",
					q.name, fmtRat(a), fmtRat(b), integrateTailFactor*max(-wp, 64))
			}
			return nil, err
		}

		prev, cur = cur, run.estimate(level)
		if level < integrateMinLevels {
			continue
		}
		if diff := new(big.Rat).Sub(cur, prev); diff.Abs(diff).Cmp(tol) < 0 {
			if q.neg {
				cur.Neg(cur)
			}
			return cur, nil
		}
	}

	return nil, fmt.Errorf("integrate did not converge on %s = %s..%s after %d step halvings: the last two estimates were %s and %s. An interior singularity or kink slows it, so split the range there",
		q.name, fmtRat(a), fmtRat(b), integrateMaxLevels, fmtRat(prev), fmtRat(cur))
}

// quadrature is one run at a fixed working precision wp.
type quadrature struct {
	q    *integral
	env  *Env
	a, b *big.Rat
	r    *big.Rat
	pi   *big.Rat
	wp   int
	need int

	// thr is the tail threshold in units of 2^wp.
	thr  *big.Int
	tmax float64

	// sum is the sum of every weighted sample so far, in units of 2^wp.
	sum *big.Int
}

// estimate returns the sum times the step of the level.
func (g *quadrature) estimate(level int) *big.Rat {
	return new(big.Rat).Mul(new(big.Rat).SetInt(g.sum), ratPow2(g.wp-level))
}

// level adds the samples that are new at this level. Level 0 holds every integer t. A later level
// holds the odd multiples of its step.
func (g *quadrature) level(ctx context.Context, level int) error {
	h := ratPow2(-level)
	step := int64(2)
	if level == 0 {
		step = 1
		if err := g.center(ctx); err != nil {
			return err
		}
	}

	// Each side ends at its first node whose term is below the threshold.
	live := [2]bool{true, true}
	for k := int64(1); live[0] || live[1]; k += step {
		t := new(big.Rat).Mul(big.NewRat(k, 1), h)
		d, w, err := g.node(ctx, t)
		if err != nil {
			return err
		}
		for side := range live {
			if !live[side] {
				continue
			}

			x := new(big.Rat)
			if side == 0 {
				x.Add(g.a, d)
			} else {
				x.Sub(g.b, d)
			}
			term, err := g.sample(ctx, x, w)
			if err != nil {
				return err
			}
			g.sum.Add(g.sum, term)
			if new(big.Int).Abs(term).Cmp(g.thr) < 0 {
				live[side] = false
			}
		}

		// The first node past the cap is still sampled, because a coarse level can step over the
		// point where a convergent tail dies.
		if f, _ := t.Float64(); f > g.tmax && (live[0] || live[1]) {
			return errTailCap
		}
	}
	return nil
}

func (g *quadrature) center(ctx context.Context) error {
	c := new(big.Rat).Add(g.a, g.b)
	c.Quo(c, big.NewRat(2, 1))

	w := new(big.Rat).Mul(g.r, g.pi)
	w.Quo(w, big.NewRat(2, 1))
	term, err := g.sample(ctx, c, w)
	if err != nil {
		return err
	}
	g.sum.Add(g.sum, term)
	return nil
}

// node returns the distance d of the nodes at t and -t from their nearer endpoint, and their
// weight without the step.
//
// With E = exp(pi*sinh t), the weight is r*pi/2*cosh t*4E/(E+1)^2. E is read to a fixed number of
// significant bits instead of a fixed absolute precision, because E can be 2^2000 and d is
// 2r/(E+1).
func (g *quadrature) node(ctx context.Context, t *big.Rat) (d, w *big.Rat, err error) {
	eRat, coshRat, err := g.hyperbolic(ctx, t)
	if err != nil {
		return nil, nil, err
	}

	e1 := new(big.Rat).Add(eRat, big.NewRat(1, 1))
	d = new(big.Rat).Quo(new(big.Rat).Mul(g.r, big.NewRat(2, 1)), e1)
	d = roundRel(d, g.need)

	// 4E/(E+1)^2
	w = new(big.Rat).Mul(eRat, big.NewRat(4, 1))
	w.Quo(w, new(big.Rat).Mul(e1, e1))
	w.Mul(w, coshRat)
	w.Mul(w, g.r)
	w.Mul(w, g.pi)
	w.Quo(w, big.NewRat(2, 1))
	return d, w, nil
}

type hyperKey struct {
	t  string
	wp int
}

type hyperVal struct {
	e, cosh *big.Rat
}

// hyperCacheMax bounds the cache. It is reset when full.
const hyperCacheMax = 1 << 14

// hyperCache holds E and cosh t, which depend only on t and the working precision. Nested
// integrals run the same inner quadrature at every outer sample, and recomputing them there is
// most of the cost. Stored values are never modified.
var hyperCache = struct {
	sync.Mutex
	m map[hyperKey]hyperVal
}{m: map[hyperKey]hyperVal{}}

// hyperbolic returns E = exp(pi*sinh t) and cosh t. E is read to a fixed number of significant
// bits.
func (g *quadrature) hyperbolic(ctx context.Context, t *big.Rat) (*big.Rat, *big.Rat, error) {
	key := hyperKey{t.String(), g.wp}
	hyperCache.Lock()
	v, ok := hyperCache.m[key]
	hyperCache.Unlock()
	if ok {
		return v.e, v.cosh, nil
	}

	tc := ratReal(t).Constructive()
	et := constructive.Exp(tc)
	emt := constructive.Inverse(et)
	half := constructive.FromRat(1, 2)
	sinh := constructive.Multiply(constructive.Subtract(et, emt), half)
	cosh := constructive.Multiply(constructive.Add(et, emt), half)
	e := constructive.Exp(constructive.Multiply(constructive.Pi(), sinh))

	rough, err := constructive.Approximate(ctx, e, 0)
	if err != nil {
		return nil, nil, err
	}
	v.e, err = approxConstructive(ctx, e, max(g.wp, rough.BitLen()-g.need))
	if err != nil {
		return nil, nil, err
	}
	v.cosh, err = approxConstructive(ctx, cosh, g.wp)
	if err != nil {
		return nil, nil, err
	}

	hyperCache.Lock()
	if len(hyperCache.m) >= hyperCacheMax {
		clear(hyperCache.m)
	}
	hyperCache.m[key] = v
	hyperCache.Unlock()
	return v.e, v.cosh, nil
}

func approxConstructive(ctx context.Context, c constructive.Real, p int) (*big.Rat, error) {
	n, err := constructive.Approximate(ctx, c, p)
	if err != nil {
		return nil, err
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt(n), ratPow2(p)), nil
}

// roundRel rounds x to a dyadic rational with bits significant bits.
func roundRel(x *big.Rat, bits int) *big.Rat {
	e := x.Num().BitLen() - x.Denom().BitLen() - bits
	return new(big.Rat).Mul(new(big.Rat).SetInt(roundToBits(x, e)), ratPow2(e))
}

// sample returns w*f(x) in units of 2^wp. An ordinary error from the body names x.
func (g *quadrature) sample(ctx context.Context, x, w *big.Rat) (*big.Int, error) {
	v, err := evalBody(ctx, g.env, g.q.body, g.q.name, ratReal(x))
	if err == nil {
		var f *big.Rat
		if f, err = approxRat(ctx, v, g.wp); err == nil {
			return roundToBits(f.Mul(f, w), g.wp), nil
		}
	}
	if isStop(err) {
		return nil, err
	}
	return nil, fmt.Errorf("the body failed at %s = %s: %w. Split the range at that point", g.q.name, fmtRat(x), err)
}
