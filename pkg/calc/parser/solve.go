package parser

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/rational"
	"github.com/ripta/reals/pkg/unified"
)

const (
	// solveExtraBits is how many bits finer than the active precision a near-zero value is
	// compared again, and how far below the bracket width interior samples are approximated.
	solveExtraBits = 64

	// solveEscalations is how many times an undecided sign is compared again at twice the
	// precision.
	solveEscalations = 4

	// solveScanCells is the number of equal cells the failure-path scan splits a bracket into.
	solveScanCells = 16

	// solveMaxSteps is the number of doublings the point form's search tries on each side of the
	// guess.
	solveMaxSteps = 16

	// solveStepShift sets the first search step to max(|x0|, 1) / 2^solveStepShift.
	solveStepShift = 6
)

var errNearZero = errors.New("indistinguishable from zero")

// nearZeroSign returns the sign of v: 0 for an exact rational zero, otherwise -1 or 1. A value
// that cannot be told from zero at p is compared again solveExtraBits finer. It returns
// errNearZero when it is still indistinguishable. The bound on the escalation is needed because
// equality of constructive reals is undecidable.
func nearZeroSign(ctx context.Context, v *unified.Real, p int) (int, error) {
	if v.IsZero() {
		return 0, nil
	}

	zero := unified.Zero().Constructive()
	for _, q := range []int{p, p - solveExtraBits} {
		s, err := constructive.PreciseCmp(ctx, v.Constructive(), zero, q)
		if err != nil {
			return 0, err
		}
		if s != 0 {
			return s, nil
		}
	}
	return 0, errNearZero
}

func solveBinder(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error) {
	s := &solveRoot{
		env:  snapshotEnv(env, body[0], bind.Name),
		body: body[0],
		name: bind.Name,
	}

	if !bind.Range {
		return s.fromGuess(ctx, bind.Lo.Value)
	}

	a, err := approxRat(ctx, bind.Lo.Value, env.precision)
	if err != nil {
		return nil, err
	}
	b, err := approxRat(ctx, bind.Hi.Value, env.precision)
	if err != nil {
		return nil, err
	}
	if a.Cmp(b) > 0 {
		a, b = b, a
	}
	s.a, s.b, s.from, s.to = a, b, a, b

	fa, sa, err := s.endpoint(ctx, a)
	if err != nil {
		return nil, err
	}
	if sa == 0 {
		return ratReal(a), nil
	}
	fb, sb, err := s.endpoint(ctx, b)
	if err != nil {
		return nil, err
	}
	if sb == 0 {
		return ratReal(b), nil
	}
	if sa == sb {
		return nil, s.noSignChange(ctx, fa, fb, sa)
	}

	return s.finish(ctx, fa, fb, sa)
}

// finish narrows the bracket [a, b], whose body has sign sa at a and the opposite sign at b, and
// returns the lazy root.
func (s *solveRoot) finish(ctx context.Context, fa, fb *unified.Real, sa int) (*unified.Real, error) {
	prec := s.env.precision
	s.sign = sa

	if err := s.refine(ctx, prec); err != nil {
		return nil, err
	}
	if s.exact != nil {
		return ratReal(s.exact), nil
	}
	if err := s.slopeCheck(ctx, fa, fb, prec); err != nil {
		return nil, err
	}

	return unified.New(constructive.FromApproximation("solve", s.approximate), nil), nil
}

// fromGuess searches outward from x0 for a sign change and solves in the bracket it finds.
//
// Probes sit at x0 + h*2^k and x0 - h*2^k, alternating sides, nearest first. Each probe is
// compared with the previous good probe on its side, which starts at x0. The first change found
// is the nearest to the guess on its side. A probe where the body fails with an ordinary error, or
// stays indistinguishable from zero, is skipped.
func (s *solveRoot) fromGuess(ctx context.Context, guess *unified.Real) (*unified.Real, error) {
	x0, err := approxRat(ctx, guess, s.env.precision)
	if err != nil {
		return nil, err
	}

	f0, sign0, err := s.signOf(ctx, x0)
	if errors.Is(err, errNearZero) {
		return nil, s.guessNearZero(x0)
	}
	if err != nil {
		return nil, err
	}
	if sign0 == 0 {
		return ratReal(x0), nil
	}

	h := new(big.Rat).Abs(x0)
	if h.Cmp(big.NewRat(1, 1)) < 0 {
		h.SetInt64(1)
	}
	h.Mul(h, ratPow2(-solveStepShift))

	type probe struct {
		x    *big.Rat
		f    *unified.Real
		sign int
	}
	prev := [2]probe{{x0, f0, sign0}, {x0, f0, sign0}}
	step := new(big.Rat).Set(h)
	skipped := false
	for k := 0; k <= solveMaxSteps; k++ {
		for side := range prev {
			x := new(big.Rat)
			if side == 0 {
				x.Add(x0, step)
			} else {
				x.Sub(x0, step)
			}

			f, sign, err := s.signOf(ctx, x)
			if err != nil {
				if isStop(err) {
					return nil, err
				}
				skipped = true
				continue
			}
			if sign == 0 {
				return ratReal(x), nil
			}

			p := prev[side]
			if sign == p.sign {
				prev[side] = probe{x, f, sign}
				continue
			}

			lo, hi := p, probe{x, f, sign}
			if side == 1 {
				lo, hi = hi, lo
			}
			s.a, s.b, s.from, s.to = lo.x, hi.x, lo.x, hi.x
			return s.finish(ctx, lo.f, hi.f, lo.sign)
		}
		step.Mul(step, big.NewRat(2, 1))
	}

	msg := fmt.Sprintf("no sign change found near %s = %s within %d doublings. Give a range, such as %s = a..b", s.name, fmtRat(x0), solveMaxSteps, s.name)
	if skipped {
		msg += ". Some probes failed or were too close to zero and were skipped, so a root may be hidden there"
	}
	return nil, errors.New(msg)
}

func (s *solveRoot) guessNearZero(x *big.Rat) error {
	return fmt.Errorf("the body at %s = %s is too close to zero to tell its sign. Move the guess away from it", s.name, fmtRat(x))
}

// solveRoot is a root of the body in a bracket.
//
// The bracket [from, to] always has certain, opposite signs at its ends: the sign at from is
// sign, and the sign at to is -sign. By the intermediate value theorem a continuous body has a
// root inside it, so the midpoint of a narrow bracket is within the bracket's width of that root.
// Every interior sample's sign is decided with PreciseCmp, which only returns a sign it is sure
// of. Nothing is ever kept on a tolerance, because a flat root such as x**3 at 0 would then
// leave a point whose body is tiny but whose distance to the root is not.
//
// Narrowing resumes from the bracket the last request left, so every precision follows one path.
// The slope check only rejects, so it never weakens the invariant.
type solveRoot struct {
	env  *Env
	body Node
	name string

	a, b *big.Rat
	sign int // sign of the body at a

	// from and to are the narrowest bracket so far. exact is set when a sample was an exact zero.
	from, to *big.Rat
	exact    *big.Rat
}

// signOf evaluates the body at x and returns its sign. The sign is 0 only for an exact zero. It
// returns errNearZero when the sign cannot be told.
func (s *solveRoot) signOf(ctx context.Context, x *big.Rat) (*unified.Real, int, error) {
	v, err := evalBody(ctx, s.env, s.body, s.name, ratReal(x))
	if err != nil {
		return nil, 0, err
	}

	sign, err := nearZeroSign(ctx, v, s.env.precision)
	return v, sign, err
}

// endpoint is signOf for a range bound, with a near-zero error that names the bound.
func (s *solveRoot) endpoint(ctx context.Context, x *big.Rat) (*unified.Real, int, error) {
	v, sign, err := s.signOf(ctx, x)
	if errors.Is(err, errNearZero) {
		return nil, 0, fmt.Errorf("the body at %s = %s is too close to zero to tell its sign. Move the bound away from it", s.name, fmtRat(x))
	}
	return v, sign, err
}

func (e *Env) withPrecision(p int) *Env {
	c := *e
	c.precision = p
	return &c
}

// refine narrows the bracket to 2^(p-1) wide, resuming from its current state.
//
// A sample whose sign is indistinguishable from zero is compared again at doubling precisions,
// from depth = |p| + solveExtraBits bits up to depth * 2^solveEscalations. A root of multiplicity
// k leaves a body about 2^(k*p) near it, so the cap covers multiplicities up to about 16. If the
// sample stays undecided, nearby points in the middle third are tried. If all of them stay
// undecided, the body is probably zero on an interval, and refine returns an error.
func (s *solveRoot) refine(ctx context.Context, p int) error {
	if s.exact != nil {
		return nil
	}

	depth := max(-p, 0) + solveExtraBits
	limit := ratPow2(p - 1)

	third := new(big.Rat)
	for new(big.Rat).Sub(s.to, s.from).Cmp(limit) > 0 {
		third.Sub(s.to, s.from)
		c1, sg1, err := s.pick(ctx, third, solveFirstOffsets, depth)
		if err != nil {
			return err
		}
		if sg1 == 0 {
			s.exact = c1
			return nil
		}
		if sg1 != s.sign {
			s.to = c1
			continue
		}

		c2, sg2, err := s.pick(ctx, third, solveSecondOffsets, depth)
		if err != nil {
			return err
		}
		switch {
		case sg2 == 0:
			s.exact = c2
			return nil
		case sg2 == s.sign:
			s.from = c2
		default:
			s.from, s.to = c1, c2
		}
	}
	return nil
}

// Offsets, in 48ths of the bracket width from from, of the two probe points. The first of each is
// the third. The rest stay inside the middle third, so a step still cuts the width to at most
// 0.4 of itself.
var (
	solveFirstOffsets  = []int64{16, 17, 18, 19}
	solveSecondOffsets = []int64{32, 31, 30, 29}
)

// pick returns the first of the candidate points whose sign can be decided, with that sign. The
// sign is 0 only for an exact rational zero.
func (s *solveRoot) pick(ctx context.Context, width *big.Rat, offsets []int64, depth int) (*big.Rat, int, error) {
	for _, off := range offsets {
		x := new(big.Rat).Mul(width, big.NewRat(off, 48))
		x.Add(x, s.from)

		sign, err := s.signAt(ctx, x, depth)
		if errors.Is(err, errNearZero) {
			continue
		}
		return x, sign, err
	}

	return nil, 0, fmt.Errorf("cannot tell the sign of the body near %s = %s. The body may be zero on an interval, or the root too flat to bracket", s.name, fmtRat(s.from))
}

// signAt decides the sign of the body at x. When it cannot tell the value from zero, it evaluates
// the body again at twice the precision.
//
// The body decides its operands, such as the base of **, at the environment precision. Each
// level therefore evaluates it in a copy at that level's precision plus solveExtraBits. The
// common case is decided at the first level and costs one evaluation.
func (s *solveRoot) signAt(ctx context.Context, x *big.Rat, depth int) (int, error) {
	zero := unified.Zero().Constructive()
	for i := 0; i <= solveEscalations; i++ {
		bits := depth << i
		env := s.env.withPrecision(min(s.env.precision, -bits-solveExtraBits))

		v, err := evalBody(ctx, env, s.body, s.name, ratReal(x))
		if err != nil {
			return 0, err
		}
		if v.IsZero() {
			return 0, nil
		}

		sign, err := constructive.PreciseCmp(ctx, v.Constructive(), zero, -bits)
		if err != nil {
			return 0, err
		}
		if sign != 0 {
			return sign, nil
		}
	}
	return 0, errNearZero
}

// approximate is the Approximator of the lazy root.
func (s *solveRoot) approximate(ctx context.Context, p int) (*big.Int, error) {
	if err := s.refine(ctx, p); err != nil {
		return nil, err
	}

	x := s.exact
	if x == nil {
		x = new(big.Rat).Add(s.from, s.to)
		x.Quo(x, big.NewRat(2, 1))
	}
	return roundToBits(x, p), nil
}

// slopeCheck rejects a bracket that narrowed onto a pole or jump. At a root the local slope tends
// to the finite derivative. At a pole or jump it grows like the inverse of the width, about
// 2^|prec|. The threshold sits halfway in the exponent.
func (s *solveRoot) slopeCheck(ctx context.Context, fa, fb *unified.Real, prec int) error {
	q := prec - 2*solveExtraBits

	va, err := approxRat(ctx, fa, q)
	if err != nil {
		return err
	}
	vb, err := approxRat(ctx, fb, q)
	if err != nil {
		return err
	}
	initial := slope(new(big.Rat).Add(new(big.Rat).Abs(va), new(big.Rat).Abs(vb)), s.a, s.b)
	limit := initial.Mul(initial, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(-prec/2))))

	env := s.env.withPrecision(q - solveExtraBits)
	lo, err := s.sample(ctx, env, s.from, q)
	if err != nil {
		return err
	}
	hi, err := s.sample(ctx, env, s.to, q)
	if err != nil {
		return err
	}
	final := slope(new(big.Rat).Abs(new(big.Rat).Sub(hi, lo)), s.from, s.to)

	if final.Cmp(limit) > 0 {
		mid := new(big.Rat).Add(s.from, s.to)
		mid.Quo(mid, big.NewRat(2, 1))
		return fmt.Errorf("the body changes too steeply near %s = %s to be a root. A pole or jump is likely", s.name, fmtRat(mid))
	}
	return nil
}

func slope(rise *big.Rat, a, b *big.Rat) *big.Rat {
	return new(big.Rat).Quo(rise, new(big.Rat).Sub(b, a))
}

func (s *solveRoot) sample(ctx context.Context, env *Env, x *big.Rat, q int) (*big.Rat, error) {
	v, err := evalBody(ctx, env, s.body, s.name, ratReal(x))
	if err != nil {
		return nil, err
	}
	return approxRat(ctx, v, q)
}

// noSignChange builds the error for a bracket whose ends share a sign. A coarse scan adds hints
// to the message and never changes the outcome. Failed samples are skipped, except for stop
// errors.
func (s *solveRoot) noSignChange(ctx context.Context, fa, fb *unified.Real, sign int) error {
	prec := s.env.precision
	va, err := approxRat(ctx, fa, prec)
	if err != nil {
		return err
	}
	vb, err := approxRat(ctx, fb, prec)
	if err != nil {
		return err
	}

	hint, err := s.scan(ctx, sign)
	if err != nil {
		return err
	}
	return fmt.Errorf("no sign change on %s = %s..%s: the body is %s at %s and %s at %s. %s",
		s.name, fmtRat(s.a), fmtRat(s.b), fmtRat(va), fmtRat(s.a), fmtRat(vb), fmtRat(s.b), hint)
}

type scanPoint struct {
	x    *big.Rat
	sign int
}

func (s *solveRoot) scan(ctx context.Context, endSign int) (string, error) {
	pts := []scanPoint{{s.a, endSign}}
	step := new(big.Rat).Sub(s.b, s.a)
	step.Quo(step, big.NewRat(solveScanCells, 1))
	for i := 1; i < solveScanCells; i++ {
		x := new(big.Rat).Mul(step, big.NewRat(int64(i), 1))
		x.Add(x, s.a)

		v, err := evalBody(ctx, s.env, s.body, s.name, ratReal(x))
		if err != nil {
			if isStop(err) {
				return "", err
			}
			continue
		}

		n, err := constructive.Approximate(ctx, v.Constructive(), s.env.precision)
		if err != nil {
			if isStop(err) {
				return "", err
			}
			continue
		}
		if n.Sign() != 0 && !v.IsZero() {
			pts = append(pts, scanPoint{x, n.Sign()})
		}
	}
	pts = append(pts, scanPoint{s.b, endSign})
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].x.Cmp(pts[j].x) < 0 })

	var near []string
	var first string
	for i := 1; i < len(pts); i++ {
		if pts[i-1].sign == pts[i].sign {
			continue
		}
		mid := new(big.Rat).Add(pts[i-1].x, pts[i].x)
		mid.Quo(mid, big.NewRat(2, 1))
		near = append(near, fmt.Sprintf("%s = %s", s.name, fmtRat(mid)))
		if first == "" {
			first = fmt.Sprintf("%s = %s..%s", s.name, fmtRat(pts[i-1].x), fmtRat(pts[i].x))
		}
	}

	if len(near) == 0 {
		return "The scan found no sign change either. A root of even multiplicity has no sign change.", nil
	}
	return fmt.Sprintf("Sign changes near %s. Try %s.", joinAnd(near), first), nil
}

func joinAnd(parts []string) string {
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// snapshotEnv copies the current values of body's free variables into a fresh scope. The lazy
// root refines after the call returns, and a later assignment must not change it. A jet stays a
// jet, since a binder inside a diff body integrates or solves its coefficients.
func snapshotEnv(env *Env, body Node, bound string) *Env {
	free := map[string]bool{}
	freeIdents(body, map[string]bool{bound: true}, free)

	snap := &Env{precision: env.precision, vars: map[string]*binding{}}
	for name := range free {
		if b := env.lookup(name); b != nil {
			snap.vars[name] = &binding{value: b.value, jet: b.jet}
		}
	}
	return snap
}

// freeIdents collects the identifiers in n that are not in bound. A nested binder's name is
// bound in its body but not in its range, which is evaluated in the outer scope.
func freeIdents(n Node, bound map[string]bool, out map[string]bool) {
	switch n := n.(type) {
	case *IdentNode:
		if !bound[n.Name.Value] {
			out[n.Name.Value] = true
		}
		return
	case *CallNode:
		// The parser accepts a binding only as the last argument of a binder call.
		if len(n.Args) > 0 {
			if b, ok := n.Args[len(n.Args)-1].(*BindingNode); ok {
				for _, c := range children(b) {
					freeIdents(c, bound, out)
				}
				inner := make(map[string]bool, len(bound)+1)
				for k := range bound {
					inner[k] = true
				}
				inner[b.Name.Value] = true
				for _, c := range n.Args[:len(n.Args)-1] {
					freeIdents(c, inner, out)
				}
				return
			}
		}
	}

	for _, c := range children(n) {
		freeIdents(c, bound, out)
	}
}

func ratReal(r *big.Rat) *unified.Real {
	return unified.New(constructive.One(), rational.FromRational(r))
}

// ratPow2 returns 2^e.
func ratPow2(e int) *big.Rat {
	if e >= 0 {
		return new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(e)))
	}
	return new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(-e)))
}

// roundToBits returns x / 2^p rounded to the nearest integer.
func roundToBits(x *big.Rat, p int) *big.Int {
	num := new(big.Int).Set(x.Num())
	den := new(big.Int).Set(x.Denom())
	if p < 0 {
		num.Lsh(num, uint(-p))
	} else {
		den.Lsh(den, uint(p))
	}

	// floor((2 num + den) / (2 den))
	num.Lsh(num, 1).Add(num, den)
	den.Lsh(den, 1)
	return num.Div(num, den)
}

func fmtRat(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	return new(big.Float).SetPrec(64).SetRat(r).Text('g', 6)
}
