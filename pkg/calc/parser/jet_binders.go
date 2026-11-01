package parser

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/bits"
	"slices"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
)

// jetShape is the layout of the perturbations a binder sees: one tag per live diff, largest
// first, and the coefficient count of each. A jet in this layout flattens to a list of scalars, one
// per coefficient, and the list builds back into a jet.
type jetShape struct {
	tags []int64
	lens []int
}

// newJetShape collects the tags of the jets bound in env and of the extra values.
func newJetShape(env *Env, extra ...jv) jetShape {
	lens := map[int64]int{}
	for e := env; e != nil; e = e.parent {
		for _, b := range e.vars {
			if b.jet != nil {
				collectTags(*b.jet, lens)
			}
		}
	}
	for _, v := range extra {
		collectTags(v, lens)
	}

	var s jetShape
	for t := range lens {
		s.tags = append(s.tags, t)
	}
	slices.SortFunc(s.tags, func(a, b int64) int { return int(b - a) })
	for _, t := range s.tags {
		s.lens = append(s.lens, lens[t])
	}
	return s
}

func collectTags(v jv, out map[int64]int) {
	if v.co == nil {
		return
	}
	out[v.tag] = len(v.co)
	for _, c := range v.co {
		collectTags(c, out)
	}
}

// size is the number of scalar coefficients.
func (s jetShape) size() int {
	n := 1
	for _, l := range s.lens {
		n *= l
	}
	return n
}

// degree is the highest total order of a coefficient, which bounds the Newton steps that give the
// root every coefficient.
func (s jetShape) degree() int {
	d := 0
	for _, l := range s.lens {
		d += l - 1
	}
	return d
}

// flatten returns the scalar coefficients of v. A value constant in a tag has zeros in it.
func (s jetShape) flatten(v jv) ([]*unified.Real, error) {
	out := make([]*unified.Real, 0, s.size())

	var walk func(v jv, level int) error
	walk = func(v jv, level int) error {
		if level == len(s.tags) {
			if v.co != nil {
				return errors.New("a jet has a perturbation that the enclosing diff does not hold")
			}
			out = append(out, v.re)
			return nil
		}
		for k := 0; k < s.lens[level]; k++ {
			if err := walk(v.coef(s.tags[level], k), level+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(v, 0); err != nil {
		return nil, err
	}
	return out, nil
}

// build is the inverse of flatten.
func (s jetShape) build(vals []*unified.Real) jv {
	pos := 0

	var walk func(level int) jv
	walk = func(level int) jv {
		if level == len(s.tags) {
			v := scalarJ(vals[pos])
			pos++
			return v
		}
		co := make([]jv, s.lens[level])
		for k := range co {
			co[k] = walk(level + 1)
		}
		return jv{tag: s.tags[level], co: co}
	}
	return walk(0)
}

// integrateJet integrates every coefficient of the integrand on the same nodes, so one quadrature
// run fills them all.
//
// A finite range runs over u in 0..1 with s = a + (b-a)*u, which keeps the bounds' own
// perturbations exact at every order. A range with one infinite end splits at the constant part of
// its finite bound c. The integral from c0 to the infinite end has constant bounds. The rest is the
// finite integral between c and c0, whose width has value 0 but may have a nonzero derivative.
func integrateJet(ctx context.Context, env *Env, body []Node, bind jetBinding) (jv, error) {
	snap := snapshotEnv(env, body[0], bind.Name)
	shape := newJetShape(snap, bind.Lo.Value, bind.Hi.Value)
	if len(shape.tags) == 0 {
		r, err := integrateBinder(ctx, env, body, bind.scalar())
		return scalarJ(r), err
	}

	lo, hi := bind.Lo, bind.Hi
	if lo.Inf == 0 && hi.Inf == 0 {
		return integrateFiniteJet(ctx, snap, shape, body[0], bind.Name, lo.Value, hi.Value, env.precision)
	}

	constant := bind.scalar()
	main, err := integrateInfiniteJet(ctx, snap, shape, body[0], constant, env.precision)
	if err != nil {
		return jv{}, err
	}

	// The finite bound is the only one that can carry a perturbation.
	switch {
	case lo.Inf == 0 && lo.Value.co != nil:
		corr, err := integrateFiniteJet(ctx, snap, shape, body[0], bind.Name, lo.Value, scalarJ(constant.Lo.Value), env.precision)
		if err != nil {
			return jv{}, err
		}
		return jadd(main, corr), nil

	case hi.Inf == 0 && hi.Value.co != nil:
		corr, err := integrateFiniteJet(ctx, snap, shape, body[0], bind.Name, scalarJ(constant.Hi.Value), hi.Value, env.precision)
		if err != nil {
			return jv{}, err
		}
		return jadd(main, corr), nil
	}
	return main, nil
}

func integrateFiniteJet(ctx context.Context, snap *Env, shape jetShape, body Node, name string, a, b jv, precision int) (jv, error) {
	// Bounds that are the same value by construction give an exact zero without sampling, as the
	// scalar path does.
	if a.co == nil && b.co == nil && sameBound(a.re, b.re) {
		return shape.build(zeroReals(shape.size())), nil
	}

	w := jsub(b, a)
	q := &integral{
		env:  snap,
		name: name,
		n:    shape.size(),
		span: userSpan(ctx, a.value(), b.value(), precision),
		eval: func(ctx context.Context, env *Env, u *big.Rat) ([]*unified.Real, error) {
			s := jadd(a, jmul(w, scalarJ(ratReal(u))))
			f, err := evalBodyJet(ctx, env, body, name, s)
			if err != nil {
				return nil, err
			}
			return shape.flatten(jmul(f, w))
		},
	}

	unit := Binding{Name: name, Range: true, Lo: Bound{Value: unified.Zero()}, Hi: Bound{Value: intReal(big.NewInt(1))}}
	return q.runJet(ctx, shape, unit, precision)
}

func integrateInfiniteJet(ctx context.Context, snap *Env, shape jetShape, body Node, bind Binding, precision int) (jv, error) {
	q := &integral{
		env:  snap,
		name: bind.Name,
		n:    shape.size(),
		eval: func(ctx context.Context, env *Env, x *big.Rat) ([]*unified.Real, error) {
			f, err := evalBodyJet(ctx, env, body, bind.Name, scalarJ(ratReal(x)))
			if err != nil {
				return nil, err
			}
			return shape.flatten(f)
		},
	}
	return q.runJet(ctx, shape, bind, precision)
}

func zeroReals(n int) []*unified.Real {
	zeros := make([]*unified.Real, n)
	for i := range zeros {
		zeros[i] = unified.Zero()
	}
	return zeros
}

// userSpan words the range the user wrote, since a run over the substituted unit range would name
// 0..1. It is empty when a bound cannot be approximated.
func userSpan(ctx context.Context, a, b *unified.Real, precision int) string {
	ra, err := approxRat(ctx, a, precision)
	if err != nil {
		return ""
	}
	rb, err := approxRat(ctx, b, precision)
	if err != nil {
		return ""
	}
	return fmtRat(ra) + ".." + fmtRat(rb)
}

// runJet estimates the integral for the range of bind and returns the lazy coefficients as a jet.
func (q *integral) runJet(ctx context.Context, shape jetShape, bind Binding, precision int) (jv, error) {
	zero, err := q.configure(ctx, precision, bind)
	if err != nil {
		return jv{}, err
	}
	if zero {
		return shape.build(zeroReals(q.n)), nil
	}

	// The first estimate is eager, as for one output.
	if _, err := q.approximate(ctx, precision-integrateReadSlack); err != nil {
		return jv{}, err
	}

	outs := constructive.FromApproximationSlice("integrate", q.n, q.approximate)
	vals := make([]*unified.Real, len(outs))
	for i, c := range outs {
		vals[i] = unified.New(c, nil)
	}
	return shape.build(vals), nil
}

// dropValue zeroes the scalar part of v, leaving every perturbation as it is.
func dropValue(v jv) jv {
	if v.co == nil {
		return zeroJ()
	}

	co := slices.Clone(v.co)
	co[0] = dropValue(co[0])
	return jv{tag: v.tag, co: co}
}

// solveJet lifts the root to a jet by Newton steps on jets, from its value. The implicit function
// theorem gives r' = -f_x / f_r, and each step doubles the number of correct coefficients.
//
// The slope f_r comes from evaluating the body at root + e in a fresh tag, so it is a jet in the
// enclosing variables too.
func solveJet(ctx context.Context, env *Env, body []Node, bind jetBinding) (jv, error) {
	shape := newJetShape(snapshotEnv(env, body[0], bind.Name))
	r0, err := solveBinder(ctx, env, body, bind.scalar())
	if err != nil {
		return jv{}, err
	}
	degree := shape.degree()
	if degree == 0 {
		return scalarJ(r0), nil
	}

	root := scalarJ(r0)
	for step := 0; step < bits.Len(uint(degree)); step++ {
		t := newTag()
		f, err := evalBodyJet(ctx, env, body[0], bind.Name, jv{tag: t, co: []jv{root, intJ(1)}})
		if err != nil {
			return jv{}, err
		}
		value, slope := f.coef(t, 0), f.coef(t, 1)

		if step == 0 {
			if err := checkSlope(ctx, env, bind.Name, r0, slope.value()); err != nil {
				return jv{}, err
			}
		}

		delta, err := jdiv(value, slope)
		if err != nil {
			return jv{}, err
		}
		// The body is zero at the root, so the correction has no scalar part. Dropping it keeps the
		// root's value the one the lazy root refines.
		root = jsub(root, dropValue(delta))
	}
	return root, nil
}

// checkSlope rejects a root whose slope is zero or cannot be told from zero.
func checkSlope(ctx context.Context, env *Env, name string, root, slope *unified.Real) error {
	sign, err := kinkSign(ctx, slope, env.precision)
	if err == nil && sign != 0 {
		return nil
	}
	if err != nil && !errors.Is(err, errNearZero) {
		return err
	}

	at := "the root"
	if x, aerr := approxRat(ctx, root, env.precision); aerr == nil {
		at = fmt.Sprintf("%s = %s", name, fmtRat(x))
	} else if isStop(aerr) {
		return aerr
	}
	if err == nil {
		return fmt.Errorf("the slope of the body is zero at %s, so the root has no derivative there", at)
	}
	return fmt.Errorf("the slope of the body at %s is too close to zero to tell whether the root has a derivative there", at)
}
