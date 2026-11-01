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

	// integrateTailFactor bounds how close to an endpoint, or on an infinite range how far
	// out, a node may sit: 2^(integrateTailFactor * max(|working precision|, 64)) times the
	// scale. The 64-bit floor keeps the cap useful at low precision. It ends a divergent
	// integrand such as 1/x, whose tail never falls below the threshold.
	integrateTailFactor = 4
)

// errTailCap ends a run whose integrand is still above the tail threshold at the cap on t.
var errTailCap = errors.New("the tail does not decay before the cap on t")

func integrateBinder(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	q := &integral{
		env:  snapshotEnv(env, body[0], bind.Name),
		name: bind.Name,
		n:    1,
		eval: func(ctx context.Context, env *Env, x *big.Rat) ([]*unified.Real, error) {
			v, err := evalBody(ctx, env, body[0], bind.Name, ratReal(x))
			return []*unified.Real{v}, err
		},
	}

	zero, err := q.configure(ctx, env.precision, bind)
	if err != nil {
		return nil, err
	}
	if zero {
		return unified.Zero(), nil
	}

	// The first estimate is eager, so its errors land on the calling line. The lazy value then
	// serves it from the cache. Reading a value at p asks the approximator for a few bits finer.
	if _, err := q.approximate(ctx, env.precision-integrateReadSlack); err != nil {
		return nil, err
	}

	return unified.New(constructive.FromApproximation("integrate", func(ctx context.Context, p int) (*big.Int, error) {
		r, err := q.approximate(ctx, p)
		if err != nil {
			return nil, err
		}
		return r[0], nil
	}), nil), nil
}

// configure sets the shape and orientation of the range. It reports true when the range is
// provably empty, so the integral is an exact zero.
func (q *integral) configure(ctx context.Context, precision int, bind Binding) (bool, error) {
	switch lo, hi := bind.Lo, bind.Hi; {
	case lo.Inf != 0 && hi.Inf != 0:
		if lo.Inf == hi.Inf {
			return false, fmt.Errorf("both bounds of integrate are infinite in the same direction. Use opposite infinities, or give one finite bound")
		}
		q.shape, q.neg = shapeBoth, lo.Inf > 0
	case lo.Inf != 0 || hi.Inf != 0:
		// The finite bound is the start of the transformed range. The infinite end picks the shape,
		// and a range that runs from infinity toward the finite bound negates.
		c, inf, from := lo.Value, hi.Inf, false
		if lo.Inf != 0 {
			c, inf, from = hi.Value, lo.Inf, true
		}
		q.lo, q.neg = c, from == (inf > 0)
		q.shape = shapeUpper
		if inf < 0 {
			q.shape = shapeLower
		}
	default:
		lo, hi := lo.Value, hi.Value

		// The order of the bounds is decided once, so every precision follows the same orientation.
		// Only bounds that are provably equal give an exact zero. Bounds that cannot be told apart
		// might still differ by a width that a huge integrand turns into a large value.
		sign, err := nearZeroSign(ctx, hi.Subtract(lo), precision)
		switch {
		case errors.Is(err, errNearZero):
			if sameBound(lo, hi) {
				return true, nil
			}
			return false, fmt.Errorf("the bounds of integrate are too close to tell apart at this precision. Raise the precision or give bounds that differ by more than 2^%d", precision)
		case err != nil:
			return false, err
		case sign == 0:
			return true, nil
		}

		q.neg = sign < 0
		if q.neg {
			lo, hi = hi, lo
		}
		q.lo, q.hi = lo, hi
	}

	return false, nil
}

// sameBound reports whether two bounds are the same value by construction, such as PI and PI.
func sameBound(a, b *unified.Real) bool {
	return constructive.AsConstruction(a.Constructive()) == constructive.AsConstruction(b.Constructive())
}

// shape is the variable change of a range. A finite range uses tanh-sinh. A range with one
// infinite end uses exp-sinh, and the whole line uses sinh-sinh.
type shape int

const (
	shapeFinite shape = iota
	shapeUpper        // lo..inf
	shapeLower        // -inf..lo
	shapeBoth
)

// integral is the estimate of a body over lo..hi, with lo below hi. neg is set when the bounds
// were given in the other order. A range with an infinite end has only lo, the finite end.
//
// The estimate is tanh-sinh quadrature. Nodes sit at x = c + r*tanh(pi/2*sinh t) on t = k*h,
// with h halved at each level, so every level reuses all earlier samples. A node is placed as the
// nearer endpoint plus or minus d = 2r/(E+1), where E = exp(pi*sinh t). A node a trillionth of
// the way from an endpoint thus keeps full relative precision and never rounds onto the endpoint.
//
// An infinite end uses x = lo + exp(pi/2*sinh t), mirrored for -inf, and the whole line uses
// x = sinh(pi/2*sinh t). Both run on every t, so a level's two sides are t > 0 and t < 0 for
// exp-sinh, and +x and -x for sinh-sinh.
//
// An integral can have several outputs, which share every sample. The body then returns n values
// per point, and a run converges only when every output has.
type integral struct {
	env  *Env
	name string

	// eval samples the body at x, in the environment of the run. It returns n values.
	eval func(ctx context.Context, env *Env, x *big.Rat) ([]*unified.Real, error)
	n    int

	// span names the user's range in messages, when the run works on a substituted one.
	span string

	// userAt maps a node of the substituted range back to the user's variable, for messages.
	userAt func(x *big.Rat) *big.Rat

	lo, hi *unified.Real
	shape  shape
	neg    bool

	// est was converged at estP. A request at estP or coarser is rounded from it, so the first
	// display does not run the quadrature again.
	est  []*big.Rat
	estP int
}

func (q *integral) approximate(ctx context.Context, p int) ([]*big.Int, error) {
	if q.est == nil || p < q.estP {
		est, err := q.run(ctx, p)
		if err != nil {
			return nil, err
		}
		q.est, q.estP = est, p
	}
	out := make([]*big.Int, q.n)
	for i, e := range q.est {
		out[i] = roundToBits(e, p)
	}
	return out, nil
}

// run estimates the integral to within 2^p, stopping when two successive levels agree to that.
func (q *integral) run(ctx context.Context, p int) ([]*big.Rat, error) {
	wp := p - integrateGuardBits
	a, b, r := new(big.Rat), new(big.Rat), big.NewRat(1, 1)
	var err error
	if q.shape != shapeBoth {
		if a, err = approxRat(ctx, q.lo, wp); err != nil {
			return nil, err
		}
	}
	if q.shape == shapeFinite {
		if b, err = approxRat(ctx, q.hi, wp); err != nil {
			return nil, err
		}
		if a.Cmp(b) >= 0 {
			return nil, fmt.Errorf("the bounds of integrate are too close to tell apart at precision %d", p)
		}

		r.Sub(b, a)
		r.Quo(r, big.NewRat(2, 1))
		// A wide range scales every term up.
		wp -= max(0, r.Num().BitLen()-r.Denom().BitLen())
	}
	span := q.rangeName(a, b)

	run := &quadrature{
		q:    q,
		env:  q.env.withPrecision(wp),
		a:    a,
		b:    b,
		r:    r,
		wp:   wp,
		need: -wp + 8,
		thr:  new(big.Int).Lsh(big.NewInt(1), uint(max(0, p-integrateGuardBits-wp))),
		sum:  make([]*big.Int, q.n),
	}
	for i := range run.sum {
		run.sum[i] = new(big.Int)
	}
	// The cap is the same number of bits out on every shape. Exp-sinh and sinh-sinh reach half as
	// far per unit of t, so their cap on t is twice as large.
	reach := float64(integrateTailFactor * max(-wp, 64))
	if q.shape != shapeFinite {
		run.shift = 1
		reach *= 2
	}
	run.tmax = math.Asinh(math.Ln2 * reach / math.Pi)
	if run.pi, err = approxConstructive(ctx, constructive.Pi(), wp); err != nil {
		return nil, err
	}

	tol := ratPow2(p)
	var prev, cur []*big.Rat
	last := 0
	for level := 0; level <= integrateMaxLevels; level++ {
		if err := run.level(ctx, level); err != nil {
			if errors.Is(err, errTailCap) {
				return nil, fmt.Errorf("integrate did not converge on %s = %s: the integrand is still too large at the cap on t, %d bits out toward an end of the range. It may diverge or decay too slowly there",
					q.name, span, integrateTailFactor*max(-wp, 64))
			}
			return nil, err
		}

		prev, cur = cur, run.estimate(level)
		if level < integrateMinLevels {
			continue
		}
		slow := -1
		for i := range cur {
			if diff := new(big.Rat).Sub(cur[i], prev[i]); diff.Abs(diff).Cmp(tol) >= 0 {
				slow = i
				break
			}
		}
		if slow < 0 {
			if q.neg {
				for _, c := range cur {
					c.Neg(c)
				}
			}
			return cur, nil
		}
		last = slow
	}

	which := ""
	if q.n > 1 {
		which = fmt.Sprintf(" for Taylor coefficient %d", last)
	}
	return nil, fmt.Errorf("integrate did not converge on %s = %s after %d step halvings: the last two estimates%s were %s and %s. An interior singularity or kink slows it, so split the range there",
		q.name, span, integrateMaxLevels, which, fmtRat(prev[last]), fmtRat(cur[last]))
}

// rangeName names the range of a run in messages, as the bounds were approximated.
func (q *integral) rangeName(a, b *big.Rat) string {
	if q.span != "" {
		return q.span
	}
	switch q.shape {
	case shapeUpper:
		return fmtRat(a) + "..inf"
	case shapeLower:
		return "-inf.." + fmtRat(a)
	case shapeBoth:
		return "-inf..inf"
	}
	return fmtRat(a) + ".." + fmtRat(b)
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

	// sum is the sum of every weighted sample so far for each output, in units of 2^wp.
	sum []*big.Int

	// shift halves the step of every level. An infinite range needs it, because from t = 2 to t = 3
	// the point x of exp-sinh grows from hundreds to millions, and a body such as exp(-x) is costly
	// to evaluate there.
	shift int

	// end is the t at which each side last fell below the threshold, on an infinite range.
	end [2]*big.Rat
}

// estimate returns the sum times the step of the level.
func (g *quadrature) estimate(level int) []*big.Rat {
	out := make([]*big.Rat, len(g.sum))
	for i, s := range g.sum {
		out[i] = new(big.Rat).Mul(new(big.Rat).SetInt(s), ratPow2(g.wp-level-g.shift))
	}
	return out
}

// add accumulates the terms of one sample, and reports whether every one is below the threshold.
func (g *quadrature) add(terms []*big.Int) bool {
	small := true
	for i, term := range terms {
		g.sum[i].Add(g.sum[i], term)
		if new(big.Int).Abs(term).Cmp(g.thr) >= 0 {
			small = false
		}
	}
	return small
}

// level adds the samples that are new at this level. Level 0 holds every multiple of the level-0
// step. A later level holds the odd multiples of its own step.
func (g *quadrature) level(ctx context.Context, level int) error {
	h := ratPow2(-level - g.shift)
	step := int64(2)
	if level == 0 {
		step = 1
		if err := g.center(ctx); err != nil {
			return err
		}
	}

	// Each side ends at its first node whose term is below the threshold. On an infinite range, a
	// finer level also ends a side where a coarser one did. Beyond that point the body would be
	// evaluated at an enormous x, where evaluating it can cost more than all the other samples.
	live := [2]bool{true, true}
	for k := int64(1); live[0] || live[1]; k += step {
		t := new(big.Rat).Mul(big.NewRat(k, 1), h)
		for side := range live {
			if end := g.end[side]; end != nil && t.Cmp(end) > 0 {
				live[side] = false
			}
		}
		if !live[0] && !live[1] {
			break
		}

		xs, ws, err := g.nodes(ctx, t)
		if err != nil {
			return err
		}
		for side := range live {
			if !live[side] {
				continue
			}

			term, err := g.sample(ctx, xs[side], ws[side])
			if err != nil {
				return err
			}
			if g.add(term) {
				live[side] = false
				if g.q.shape != shapeFinite {
					g.end[side] = t
				}
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
	c := new(big.Rat)
	switch g.q.shape {
	case shapeFinite:
		c.Add(g.a, g.b)
		c.Quo(c, big.NewRat(2, 1))
	case shapeUpper:
		c.Add(g.a, big.NewRat(1, 1))
	case shapeLower:
		c.Sub(g.a, big.NewRat(1, 1))
	}

	w := new(big.Rat).Mul(g.r, g.pi)
	w.Quo(w, big.NewRat(2, 1))
	term, err := g.sample(ctx, c, w)
	if err != nil {
		return err
	}
	g.add(term)
	return nil
}

// nodes returns the points and weights of the two sides at |t|, without the step.
func (g *quadrature) nodes(ctx context.Context, t *big.Rat) (xs, ws [2]*big.Rat, err error) {
	if g.q.shape == shapeFinite {
		d, w, err := g.node(ctx, t)
		if err != nil {
			return xs, ws, err
		}
		xs[0], xs[1] = new(big.Rat).Add(g.a, d), new(big.Rat).Sub(g.b, d)
		return xs, [2]*big.Rat{w, w}, nil
	}

	// e is exp(u) for u = pi/2*sinh t, and em is exp(-u). Both keep full relative precision.
	// Only sinh subtracts them, and its absolute error stays at the working precision.
	e, cosh, err := g.hyperbolic(ctx, t, true)
	if err != nil {
		return xs, ws, err
	}
	em := roundRel(new(big.Rat).Inv(e), g.need)

	c := new(big.Rat).Mul(cosh, g.pi)
	c.Quo(c, big.NewRat(2, 1))
	if g.q.shape == shapeBoth {
		x := new(big.Rat).Sub(e, em)
		x.Quo(x, big.NewRat(2, 1))
		w := new(big.Rat).Add(e, em)
		w.Mul(w, c)
		w.Quo(w, big.NewRat(2, 1))
		return [2]*big.Rat{new(big.Rat).Neg(x), x}, [2]*big.Rat{w, w}, nil
	}

	for i, v := range [2]*big.Rat{em, e} {
		if g.q.shape == shapeUpper {
			xs[i] = new(big.Rat).Add(g.a, v)
		} else {
			xs[i] = new(big.Rat).Sub(g.a, v)
		}
		ws[i] = new(big.Rat).Mul(v, c)
	}
	return xs, ws, nil
}

// node returns the distance d of the nodes at t and -t from their nearer endpoint, and their
// weight without the step.
//
// With E = exp(pi*sinh t), the weight is r*pi/2*cosh t*4E/(E+1)^2. E is read to a fixed number of
// significant bits instead of a fixed absolute precision, because E can be 2^2000 and d is
// 2r/(E+1).
func (g *quadrature) node(ctx context.Context, t *big.Rat) (d, w *big.Rat, err error) {
	eRat, coshRat, err := g.hyperbolic(ctx, t, false)
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
	t    string
	wp   int
	half bool
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

// hyperbolic returns E = exp(pi*sinh t), or exp(pi/2*sinh t) when halved is set, and cosh t. E is
// read to a fixed number of significant bits.
func (g *quadrature) hyperbolic(ctx context.Context, t *big.Rat, halved bool) (*big.Rat, *big.Rat, error) {
	key := hyperKey{t.String(), g.wp, halved}
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
	scale := constructive.Pi()
	if halved {
		scale = constructive.Multiply(scale, constructive.FromRat(1, 2))
	}
	e := constructive.Exp(constructive.Multiply(scale, sinh))

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
func (g *quadrature) sample(ctx context.Context, x, w *big.Rat) ([]*big.Int, error) {
	vs, err := g.q.eval(ctx, g.env, x)
	if err == nil {
		// A weight of 2^k scales the error of f up by 2^k. Only an infinite range has one.
		prec := g.wp
		if g.q.shape != shapeFinite {
			prec -= max(0, w.Num().BitLen()-w.Denom().BitLen())
		}

		out := make([]*big.Int, len(vs))
		for i, v := range vs {
			var f *big.Rat
			if f, err = approxRat(ctx, v, prec); err != nil {
				break
			}
			out[i] = roundToBits(f.Mul(f, w), g.wp)
		}
		if err == nil {
			return out, nil
		}
	}
	if isStop(err) {
		return nil, err
	}
	at := x
	if g.q.userAt != nil {
		at = g.q.userAt(x)
	}
	return nil, fmt.Errorf("the body failed at %s = %s: %w. Split the range at that point", g.q.name, fmtRat(at), err)
}
