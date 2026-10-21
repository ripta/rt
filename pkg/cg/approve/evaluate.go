package approve

import (
	"sort"
	"strings"

	"github.com/ripta/rt/pkg/cg/model"
)

// Resolver resolves the executable of an inner argv, the tokens a wrapper rule
// leaves after its prefix. The matcher never touches disk, so callers inject one.
// Both the gate and `cg check` build theirs with CommandResolver, so a wrapped
// command resolves the same way in each. A nil Resolver leaves inner layers
// unresolved, which lets bare-name rules match them and path rules not.
type Resolver func(argv []string) *model.Resolution

// CommandResolver returns a Resolver that resolves an argv the way a top-level
// command is resolved: against the server PATH and the call's working directory.
func CommandResolver(cwd string) Resolver {
	return func(argv []string) *model.Resolution {
		res, _ := model.ResolveCommand(argv, cwd)

		return res
	}
}

// Step names the part of the evaluation that settled a layer.
type Step int

const (
	// StepNone means no rule settled the layer, as for a prompt.
	StepNone Step = iota
	// StepDeny means a deny rule refused the layer.
	StepDeny
	// StepAllow means an ordinary allow rule ran the layer. Nothing below it is peeled.
	StepAllow
	// StepPeel means a wrapper rule ran the layer and the inner layer's verdict carries up.
	StepPeel
	// StepEnvRefusal means a matching allow rule, or a wrapper rule, did not permit a
	// dangerous environment override, or no rule matched and the override cannot be
	// permitted.
	StepEnvRefusal
	// StepRestrict means a restrict rule refused the layer.
	StepRestrict
	// StepInner means the inner layer's prompt or refusal stands for this layer.
	StepInner
)

// String renders a Step for display.
func (s Step) String() string {
	switch s {
	case StepDeny:
		return "deny"
	case StepAllow:
		return "allow"
	case StepPeel:
		return "peel"
	case StepEnvRefusal:
		return "env refusal"
	case StepRestrict:
		return "restrict"
	case StepInner:
		return "inner verdict"
	default:
		return "none"
	}
}

// ChainLayer is one layer of a wrapper chain: the argv at an offset into the typed
// argv, from the top command inward. The last layer of a chain is the one behind
// the verdict.
//
// Candidates are the rules of the step that settled the layer: the ordinary allows
// that ran it, or the tied wrapper rules whose peel ran. PathForm reports whether
// every candidate is a path-form rule. A caller that pins the inner executable
// reads both.
type ChainLayer struct {
	Offset int
	Argv   []string

	// Resolution is the inner layer's resolution. It is nil for the top layer, whose
	// resolution the caller supplied in the Subject.
	Resolution *model.Resolution

	// Wrappers are the wrapper rules peeled out of this layer: the tied set of the
	// longest match. It is empty on the innermost layer.
	Wrappers []*Rule

	// Rule is the rule that settled this layer: the permitting wrapper rule on a peeled
	// layer, the deciding rule on the last one. It is nil on a prompt.
	Rule *Rule

	Step       Step
	Candidates []*Rule
	PathForm   bool
}

// SkipReason says why a matching wrapper rule did not peel.
type SkipReason int

const (
	// SkipContract means the layer is headed by a program on the built-in list of
	// programs that do not re-execute their arguments as an argv.
	SkipContract SkipReason = iota + 1
	// SkipDash means the inner command starts with '-', so cg and the wrapper
	// disagree about where the command starts.
	SkipDash
)

// String renders a SkipReason for display.
func (r SkipReason) String() string {
	switch r {
	case SkipContract:
		return "the program breaks the wrapper contract (built-in list)"
	case SkipDash:
		return "the inner command starts with '-'"
	default:
		return "unknown"
	}
}

// Skip records a wrapper rule that matched a layer and did not peel.
type Skip struct {
	Offset int
	Rule   *Rule
	Reason SkipReason
}

// Verdict is Evaluate's outcome: a decision plus the environment-override
// disposition layered on top of it. BadEnvs is non-nil when Decision was forced
// to DecisionRefuse by a dangerous environment override that no rule permits.
// Rule and Restricted carry the same meaning as in MatchResult; Rule is nil
// when the refusal came from the env gate on an unmatched command rather than
// from a deny, allow, or restrict rule.
//
// When a wrapper chain settled the call, Chain lists every layer from the typed
// command inward, and Rule is the rule behind the verdict at the last layer. Chain
// is nil when the top layer settled the call alone. Skipped lists the wrapper rules
// that matched a layer and did not peel, whether or not they affected the verdict.
type Verdict struct {
	Decision   Decision
	Rule       *Rule
	Restricted bool
	BadEnvs    []string
	Chain      []ChainLayer
	Skipped    []Skip
}

// Evaluate applies the tiers and the dangerous-environment gate to subj.
//
// Deny is checked first on the top command and on every layer a wrapper rule can
// peel out of it. Then the allow tier settles the top layer, recursing into the
// inner layer when a wrapper rule peels. resolve supplies the resolution of each
// inner layer and may be nil.
//
// At each layer the allow tier evaluates every matching ordinary allow and the peel
// of the longest wrapper match, each through the env gate, and settles in order:
//
//  1. An ordinary allow that runs after its env check decides the layer.
//  2. Otherwise a peel that runs decides it, and the inner layer's verdict carries up.
//  3. Otherwise a matching ordinary allow refuses with its env refusal.
//  4. Otherwise a matching restrict rule refuses. A wrapper rule whose peel did not
//     run is not an allow carve-out.
//  5. Otherwise the peel's verdict stands, prompt or refusal. With no peel, the
//     layer prompts.
//
// A prompt with a dangerous override becomes a refusal, since a command with no
// matching rule has no permit_unsafe_envs list to grant an exemption. A peel runs
// only when a wrapper rule in its tied set permits every dangerous variable on its
// own and the inner layer runs, so every layer of a chain must permit the override.
// Permits are never pooled across rules, which keeps the verdict independent of
// allow order.
//
// allow-all and deny-all short-circuit before any rule is consulted.
func (rs *Ruleset) Evaluate(subj Subject, env map[string]string, resolve Resolver) Verdict {
	if len(subj.Argv) == 0 {
		return Verdict{Decision: DecisionRefuse}
	}

	switch rs.Mode {
	case ModeAllowAll:
		return Verdict{Decision: DecisionRun}
	case ModeDenyAll:
		return Verdict{Decision: DecisionRefuse}
	}

	e := &evaluation{rs: rs, env: env, resolve: resolve, top: subj, layers: make(map[int]*layer)}
	if v, ok := e.hoistDeny(); ok {
		return v
	}

	return e.verdict(e.settle(0))
}

// evaluation is the state of one Evaluate call. A layer is fully determined by its
// offset into the typed argv, so layers are memoized by offset. That bounds the
// work by the argv's length however many wrapper rules overlap.
type evaluation struct {
	rs      *Ruleset
	env     map[string]string
	resolve Resolver
	top     Subject
	layers  map[int]*layer
}

// layer is one suffix of the typed argv with its forms and wrapper matches. argv
// and the forms share the typed argv's backing array, so the layers of a chain
// together cost memory near-linear in the argv's length.
type layer struct {
	offset     int
	argv       []string
	resolution *model.Resolution

	canonical, resolved, basename matchForm

	// peels are the wrapper matches that pass the '-' guard. The deny hoist follows all of
	// them. The allow tier follows the longest, unless the layer is listed.
	peels  []wrapperMatch
	skips  []Skip
	listed bool

	settled *settled
}

// wrapperMatch is one wrapper rule matching a layer, with the number of tokens it
// peels.
type wrapperMatch struct {
	rule *Rule
	n    int
}

// settled is the allow tier's outcome for one layer. rule, restricted, and badEnvs
// are what the verdict reports and carry up from an inner layer. own is the rule
// that settled this layer itself.
type settled struct {
	offset     int
	decision   Decision
	rule       *Rule
	restricted bool
	badEnvs    []string

	step       Step
	own        *Rule
	candidates []*Rule

	wrappers []*Rule
	next     *settled
}

// layerAt returns the layer at offset, building it on first use. Layer 0 is the
// caller's subject. An inner layer is resolved through the injected resolver.
func (e *evaluation) layerAt(offset int) *layer {
	if l, ok := e.layers[offset]; ok {
		return l
	}

	l := &layer{offset: offset, argv: e.top.Argv[offset:]}
	if offset == 0 {
		l.canonical, l.resolved, l.basename = e.top.forms()
	} else {
		l.resolution = e.resolveInner(l.argv)
		l.canonical, l.resolved, l.basename = resolutionForms(l.argv, l.resolution)
	}

	e.findPeels(l)
	e.layers[offset] = l

	return l
}

// resolveInner resolves an inner argv, falling back to an empty resolution when no
// resolver was injected.
func (e *evaluation) resolveInner(argv []string) *model.Resolution {
	if e.resolve != nil {
		if res := e.resolve(argv); res != nil {
			return res
		}
	}

	return &model.Resolution{Argv: argv}
}

// innerStartsWithDash reports whether the inner command, which begins at argv[n], starts with '-'.
// Such a command would be read as a flag of the wrapper, so a wrapper rule never peels it.
func innerStartsWithDash(argv []string, n int) bool {
	return n < len(argv) && strings.HasPrefix(argv[n], "-")
}

// findPeels collects the wrapper rules that match a layer. A match whose inner
// command starts with '-' is recorded as skipped and does not peel, so the guard
// applies before the length comparison and a guarded long rule cannot hide a
// correct short one.
func (e *evaluation) findPeels(l *layer) {
	for i := range e.rs.Allow {
		rule := &e.rs.Allow[i]
		if rule.kind != KindWrapper || !tokenRuleMatches(rule, l.canonical, l.resolved, l.basename) {
			continue
		}

		n := len(rule.cmpArgv)
		if innerStartsWithDash(l.argv, n) {
			l.skips = append(l.skips, Skip{Offset: l.offset, Rule: rule, Reason: SkipDash})
			continue
		}

		l.peels = append(l.peels, wrapperMatch{rule: rule, n: n})
	}

	if len(l.peels) == 0 {
		return
	}

	l.listed = breaksWrapperContract(l.canonical, l.resolved, l.basename)
	if l.listed {
		for _, p := range l.peels {
			l.skips = append(l.skips, Skip{Offset: l.offset, Rule: p.rule, Reason: SkipContract})
		}
	}
}

// allowPeel returns the tied set of the longest wrapper match the allow tier may
// peel, and the number of tokens it removes. Equal-length matches can differ only
// in the shape of their first token, so they peel to the same inner argv.
func (l *layer) allowPeel() ([]*Rule, int) {
	if l.listed {
		return nil, 0
	}

	var tied []*Rule
	longest := 0
	for _, p := range l.peels {
		switch {
		case p.n > longest:
			longest = p.n
			tied = []*Rule{p.rule}
		case p.n == longest:
			tied = append(tied, p.rule)
		}
	}

	return tied, longest
}

// hoistDeny checks deny on the top command and on every layer reachable through a
// matching wrapper rule, before any allow is consulted. A deny on any layer refuses
// the call. It follows every matching rule, listed programs included, and checks
// layers whose argv[0] does not resolve, since a bare-name deny still matches them
// through the basename form.
func (e *evaluation) hoistDeny() (Verdict, bool) {
	type parent struct {
		offset int
		rule   *Rule
	}

	parents := make(map[int]parent)
	seen := map[int]bool{0: true}
	queue := []int{0}

	for len(queue) > 0 {
		offset := queue[0]
		queue = queue[1:]

		l := e.layerAt(offset)
		if rule := e.denyingRule(l); rule != nil {
			path := []int{offset}
			for o := offset; o != 0; o = parents[o].offset {
				path = append(path, parents[o].offset)
			}

			chain := make([]ChainLayer, 0, len(path))
			for i := len(path) - 1; i >= 0; i-- {
				pl := e.layerAt(path[i])
				cl := ChainLayer{Offset: pl.offset, Argv: pl.argv, Resolution: pl.resolution, Step: StepPeel}
				if i > 0 {
					cl.Wrappers = []*Rule{parents[path[i-1]].rule}
				} else {
					cl.Rule, cl.Step = rule, StepDeny
				}
				chain = append(chain, cl)
			}

			v := Verdict{Decision: DecisionRefuse, Rule: rule, Skipped: e.skipped()}
			if len(chain) > 1 {
				v.Chain = chain
			}

			return v, true
		}

		for _, p := range l.peels {
			child := offset + p.n
			if seen[child] {
				continue
			}

			seen[child] = true
			parents[child] = parent{offset: offset, rule: p.rule}
			queue = append(queue, child)
		}
	}

	return Verdict{}, false
}

// denyingRule returns the first deny rule matching a layer. On an inner layer a
// path-form exact or prefix deny also matches by the basename of its first token.
// The wrapper looks the inner program up itself, so it may find the denied file
// even when the server PATH finds another. The top layer keeps exact path matching,
// since the supervisor runs the path cg resolved.
func (e *evaluation) denyingRule(l *layer) *Rule {
	for i := range e.rs.Deny {
		rule := &e.rs.Deny[i]
		if ruleMatches(rule, l.canonical, l.resolved, l.basename) {
			return rule
		}

		if l.offset > 0 && rule.baseArgv != nil && (rule.kind == KindExact || rule.kind == KindPrefix) &&
			matchTokens(rule.baseArgv, l.basename, rule.kind == KindExact) {
			return rule
		}
	}

	return nil
}

// settle runs the allow tier for the layer at offset, memoized.
func (e *evaluation) settle(offset int) *settled {
	l := e.layerAt(offset)
	if l.settled == nil {
		l.settled = e.settleLayer(l)
	}

	return l.settled
}

func (e *evaluation) settleLayer(l *layer) *settled {
	s := &settled{offset: l.offset}

	var matched, ran []*Rule
	for i := range e.rs.Allow {
		rule := &e.rs.Allow[i]
		if !ruleMatches(rule, l.canonical, l.resolved, l.basename) {
			continue
		}

		matched = append(matched, rule)
		if len(rule.DisallowedEnvs(e.env)) == 0 {
			ran = append(ran, rule)
		}
	}

	if len(ran) > 0 {
		s.decision, s.rule, s.own, s.step, s.candidates = DecisionRun, ran[0], ran[0], StepAllow, ran

		return s
	}

	var permitting []*Rule
	var inner *settled

	tied, n := l.allowPeel()
	if len(tied) > 0 {
		for _, rule := range tied {
			if len(rule.DisallowedEnvs(e.env)) == 0 {
				permitting = append(permitting, rule)
			}
		}

		inner = e.settle(l.offset + n)
	}

	if len(permitting) > 0 && inner.decision == DecisionRun {
		s.decision, s.rule, s.own, s.step, s.candidates = DecisionRun, inner.rule, permitting[0], StepPeel, permitting
		s.wrappers, s.next = tied, inner

		return s
	}

	if len(matched) > 0 {
		s.decision, s.rule, s.own, s.step = DecisionRefuse, matched[0], matched[0], StepEnvRefusal
		s.badEnvs = matched[0].DisallowedEnvs(e.env)
		s.candidates = matched

		return s
	}

	for i := range e.rs.Restrict {
		rule := &e.rs.Restrict[i]
		if ruleMatches(rule, l.canonical, l.resolved, l.basename) {
			s.decision, s.rule, s.own, s.step, s.restricted = DecisionRefuse, rule, rule, StepRestrict, true

			return s
		}
	}

	if len(tied) > 0 {
		s.wrappers = tied

		switch {
		case inner.decision == DecisionRefuse || len(permitting) > 0:
			s.decision, s.rule, s.restricted, s.badEnvs = inner.decision, inner.rule, inner.restricted, inner.badEnvs
			s.step, s.next = StepInner, inner
			if len(permitting) > 0 {
				s.own, s.candidates = permitting[0], permitting
			}
		default:
			s.own, s.badEnvs = leastDisallowed(tied, e.env)
			s.decision, s.rule, s.step = DecisionRefuse, s.own, StepEnvRefusal
		}

		return s
	}

	s.decision = DecisionPrompt
	if bad := (&Rule{}).DisallowedEnvs(e.env); len(bad) > 0 {
		s.decision, s.badEnvs, s.step = DecisionRefuse, bad, StepEnvRefusal
	}

	return s
}

// verdict turns the top layer's outcome into a Verdict, walking the peel chain
// when a wrapper rule was involved.
func (e *evaluation) verdict(top *settled) Verdict {
	v := Verdict{
		Decision:   top.decision,
		Rule:       top.rule,
		Restricted: top.restricted,
		BadEnvs:    top.badEnvs,
		Skipped:    e.skipped(),
	}

	if len(top.wrappers) == 0 {
		return v
	}

	for s := top; s != nil; s = s.next {
		l := e.layerAt(s.offset)
		v.Chain = append(v.Chain, ChainLayer{
			Offset:     s.offset,
			Argv:       l.argv,
			Resolution: l.resolution,
			Wrappers:   s.wrappers,
			Rule:       s.own,
			Step:       s.step,
			Candidates: s.candidates,
			PathForm:   pathForm(s.candidates),
		})
	}

	return v
}

// skipped lists the wrapper rules that matched an evaluated layer and did not peel,
// outermost layer first.
func (e *evaluation) skipped() []Skip {
	offsets := make([]int, 0, len(e.layers))
	for offset := range e.layers {
		offsets = append(offsets, offset)
	}
	sort.Ints(offsets)

	var out []Skip
	for _, offset := range offsets {
		out = append(out, e.layers[offset].skips...)
	}

	return out
}

// leastDisallowed picks, from a tied set of wrapper rules none of which permits
// the override, the rule closest to permitting it: the one missing the fewest
// variables, with ties broken by the variable names. The choice does not depend on
// allow order, which remember re-sorts, so the refusal names the same fix every
// time.
func leastDisallowed(rules []*Rule, env map[string]string) (*Rule, []string) {
	var best *Rule
	var bestBad []string

	for _, rule := range rules {
		bad := rule.DisallowedEnvs(env)
		if best == nil || len(bad) < len(bestBad) ||
			(len(bad) == len(bestBad) && strings.Join(bad, ",") < strings.Join(bestBad, ",")) {
			best, bestBad = rule, bad
		}
	}

	return best, bestBad
}

// pathForm reports whether every rule in a non-empty candidate set matches by path
// rather than by basename.
func pathForm(rules []*Rule) bool {
	if len(rules) == 0 {
		return false
	}

	for _, rule := range rules {
		if rule.wantBasename {
			return false
		}
	}

	return true
}
