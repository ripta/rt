package parser

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ripta/reals/pkg/constructive"
	"github.com/ripta/reals/pkg/unified"
)

// kinkSign is the sign of v: 0 for an exact zero, otherwise -1 or 1. A value that cannot be told
// from zero at p is compared again at doubling precisions, from |p| + solveExtraBits bits up to
// solveEscalations doublings. It returns errNearZero when it is still undecided.
// nearZeroSign extends only one step, which cannot tell a point like 10**-200 from 0. Solve's
// own ladder also re-evaluates the body at each precision, which a plain value cannot do.
func kinkSign(ctx context.Context, v *unified.Real, p int) (int, error) {
	if v.IsZero() {
		return 0, nil
	}

	zero := unified.Zero().Constructive()
	depth := max(-p, 0) + solveExtraBits
	for i := -1; i <= solveEscalations; i++ {
		q := p
		if i >= 0 {
			q = -(depth << i)
		}

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

// kinkSet is where a non-smooth entry has no derivative.
type kinkSet int

const (
	// kinkZero is the single point 0, as for abs and signum.
	kinkZero kinkSet = iota

	// kinkInteger is every integer, as for floor and ceil, which jump there.
	kinkInteger

	// kinkNonzeroInteger is every integer but 0. trunc is flat through 0, so it is smooth there.
	kinkNonzeroInteger

	// kinkHalfInteger is every k + 1/2. round jumps there and is flat through the integers.
	kinkHalfInteger
)

// kinkNear finds the candidate kink nearest to u, and returns it with the sign of u minus it. It
// errors when u is at the candidate, or cannot be told from it. A candidate that is not a kink
// returns a zero side.
//
// The side is meaningful for kinkZero and kinkInteger, which are the sets whose rules read it.
//
// A message names the point as k, or as "of = k" when u is derived from the arguments.
func kinkNear(ctx context.Context, env *Env, name, of string, set kinkSet, u *unified.Real) (*big.Rat, int, error) {
	k := new(big.Rat)
	if set != kinkZero {
		q, err := approxRat(ctx, u, env.precision)
		if err != nil {
			return nil, 0, err
		}
		k = nearestCandidate(set, q)
	}
	if set == kinkNonzeroInteger && k.Sign() == 0 {
		return k, 0, nil
	}

	at := "at " + fmtRat(k)
	if of != "" {
		at = "where " + of + " = " + fmtRat(k)
	}

	side, err := kinkSign(ctx, u.Subtract(ratReal(k)), env.precision)
	switch {
	case errors.Is(err, errNearZero):
		return nil, 0, fmt.Errorf("%s is not differentiable %s, and the argument is too close to it to tell whether it is there", name, at)
	case err != nil:
		return nil, 0, err
	case side == 0:
		return nil, 0, fmt.Errorf("%s is not differentiable %s", name, at)
	}
	return k, side, nil
}

// nearestCandidate is the member of the set nearest to q.
func nearestCandidate(set kinkSet, q *big.Rat) *big.Rat {
	half := big.NewRat(1, 2)
	if set == kinkHalfInteger {
		// The nearest k + 1/2 is the nearest integer to q - 1/2, plus 1/2.
		n := roundRat(new(big.Rat).Sub(q, half))
		return new(big.Rat).Add(new(big.Rat).SetInt(n), half)
	}
	return new(big.Rat).SetInt(roundRat(q))
}

// roundRat rounds to the nearest integer, with halves going up.
func roundRat(q *big.Rat) *big.Int {
	shifted := new(big.Rat).Add(q, big.NewRat(1, 2))
	return new(big.Int).Div(shifted.Num(), shifted.Denom())
}

// kinkDerivative is the derivative rule of an entry that is flat wherever it is smooth.
func kinkDerivative(name string, set kinkSet) derivFn {
	return func(ctx context.Context, env *Env, u jv) (jv, error) {
		_, _, err := kinkNear(ctx, env, name, "", set, u.value())
		return zeroJ(), err
	}
}

// dAbs is signum(u). The sign is constant near a point that is not a kink.
func dAbs(ctx context.Context, env *Env, u jv) (jv, error) {
	_, side, err := kinkNear(ctx, env, "abs", "", kinkZero, u.value())
	return intJ(int64(side)), err
}

// dMod is the pair of partials of a % b = a - b*floor(a/b), which has floor(a/b) constant near
// a point where a/b is not an integer.
func dMod(ctx context.Context, env *Env, args []jv) ([]jv, error) {
	a, b := args[0].value(), args[1].value()
	if b.IsZero() {
		return nil, errDivideByZero
	}

	k, side, err := kinkNear(ctx, env, "%", "the first argument divided by the second", kinkInteger, a.Divide(b))
	if err != nil {
		return nil, err
	}
	if side < 0 {
		k.Sub(k, big.NewRat(1, 1))
	}
	return []jv{intJ(1), jneg(scalarJ(ratReal(k)))}, nil
}

// extremumPartials is the one-hot partials of min or max. The argument that has the extreme value
// has partial 1 and the others have 0. A tie that involves a live argument is a kink, because the
// two sides can have different slopes. A tie between constants is harmless.
func extremumPartials(name string, wantMax bool) partialsFn {
	return func(ctx context.Context, env *Env, args []jv) ([]jv, error) {
		vals := make([]*unified.Real, len(args))
		for i, a := range args {
			vals[i] = a.value()
		}

		sel := 0
		for i := 1; i < len(vals); i++ {
			// An undecided comparison leaves the selection alone. The pass below reports it.
			s, err := kinkSign(ctx, vals[i].Subtract(vals[sel]), env.precision)
			if err != nil && !errors.Is(err, errNearZero) {
				return nil, err
			}
			if (wantMax && s > 0) || (!wantMax && s < 0) {
				sel = i
			}
		}

		for i := range args {
			if i == sel {
				continue
			}

			s, err := kinkSign(ctx, vals[i].Subtract(vals[sel]), env.precision)
			tied := s == 0 && err == nil
			undecided := errors.Is(err, errNearZero)
			switch {
			case err != nil && !undecided:
				return nil, err
			case !tied && !undecided:
				continue
			case args[i].co == nil && args[sel].co == nil:
				continue
			}

			at, err := approxRat(ctx, vals[sel], env.precision)
			if err != nil {
				return nil, err
			}
			if undecided {
				return nil, fmt.Errorf("%s is not differentiable where two arguments are equal, and two are too close to %s to tell whether they are", name, fmtRat(at))
			}
			return nil, fmt.Errorf("%s is not differentiable where two arguments are equal, as at %s", name, fmtRat(at))
		}

		out := make([]jv, len(args))
		for i := range out {
			out[i] = zeroJ()
		}
		out[sel] = intJ(1)
		return out, nil
	}
}
