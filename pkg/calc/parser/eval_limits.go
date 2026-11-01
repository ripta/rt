package parser

import (
	"context"
	"errors"
	"fmt"

	"github.com/ripta/reals/pkg/unified"

	"github.com/ripta/rt/pkg/calc/tokens"
)

// MaxBodyEvaluations caps the body evaluations of one top-level expression. Nested binders share
// the count.
const MaxBodyEvaluations = 1_000_000

var (
	// ErrEvaluationBudget reports that an expression used up its body evaluations.
	ErrEvaluationBudget = fmt.Errorf("evaluation budget of %d body evaluations exceeded", MaxBodyEvaluations)

	// ErrCancelled reports that the context ended an evaluation. It matches context.Canceled.
	ErrCancelled = fmt.Errorf("evaluation cancelled: %w", context.Canceled)
)

type budgetKey struct{}

type budget struct {
	used, limit int
}

// WithBudget returns a context that carries a body-evaluation count. A context that already
// holds one is returned as it is, so work deferred to a later step of the same line charges that
// line's count. Without a count, body evaluations are not counted.
func WithBudget(ctx context.Context) context.Context {
	if _, ok := ctx.Value(budgetKey{}).(*budget); ok {
		return ctx
	}
	return withBudgetLimit(ctx, MaxBodyEvaluations)
}

func withBudgetLimit(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, budgetKey{}, &budget{limit: limit})
}

// checkCtx returns a stop error once ctx has ended, and nil before that.
func checkCtx(ctx context.Context) error {
	err := ctx.Err()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return ErrCancelled
	default:
		return fmt.Errorf("evaluation stopped: %w", err)
	}
}

// isStop reports whether err is a budget or cancellation error, which must never be rewritten
// or recovered from.
func isStop(err error) bool {
	return errors.Is(err, ErrEvaluationBudget) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// loopCheckEvery is how many iterations of a big-integer loop pass between context checks.
const loopCheckEvery = 1024

// loopCheck checks ctx on entry and then every loopCheckEvery calls to tick.
type loopCheck struct {
	ctx context.Context
	n   int
}

func newLoopCheck(ctx context.Context) (*loopCheck, error) {
	return &loopCheck{ctx: ctx}, checkCtx(ctx)
}

func (l *loopCheck) tick() error {
	l.n++
	if l.n%loopCheckEvery == 0 {
		return checkCtx(l.ctx)
	}
	return nil
}

// Bound is an evaluated range bound or point value. Inf is 0 for a finite Value, otherwise the
// sign of the infinity.
type Bound struct {
	Value *unified.Real
	Inf   int
}

// Binding is a binding argument whose bounds were evaluated in the caller's scope. A point
// binding keeps its value in Lo.
type Binding struct {
	Name  string
	Range bool
	Lo    Bound
	Hi    Bound
}

// binderFunc evaluates a binder call. body holds the call's arguments before the binding,
// unevaluated.
type binderFunc func(ctx context.Context, env *Env, body []Node, bind Binding) (*unified.Real, error)

// evalBody evaluates body with name bound to val in a child scope. It charges the budget and
// checks ctx first.
func evalBody(ctx context.Context, env *Env, body Node, name string, val *unified.Real) (*unified.Real, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}

	if b, ok := ctx.Value(budgetKey{}).(*budget); ok {
		b.used++
		if b.used > b.limit {
			return nil, ErrEvaluationBudget
		}
	}

	return body.Eval(ctx, env.child(name, val))
}

func evalBound(ctx context.Context, env *Env, n Node) (Bound, error) {
	if infBound(n) != nil {
		if u, ok := n.(*UnaryNode); ok && u.Op.Type == tokens.OP_MINUS {
			return Bound{Inf: -1}, nil
		}
		return Bound{Inf: 1}, nil
	}

	v, err := n.Eval(ctx, env)
	if err != nil {
		return Bound{}, err
	}
	return Bound{Value: v}, nil
}

func evalBinding(ctx context.Context, env *Env, b *BindingNode) (Binding, error) {
	out := Binding{Name: b.Name.Value, Range: b.Hi != nil}

	var err error
	if out.Lo, err = evalBound(ctx, env, b.Lo); err != nil {
		return Binding{}, err
	}
	if out.Range {
		if out.Hi, err = evalBound(ctx, env, b.Hi); err != nil {
			return Binding{}, err
		}
	}
	return out, nil
}
