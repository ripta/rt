package approve

import (
	"slices"
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
	// StepTieRefusal means equally specific allow rules all run the call and disagree on a field
	// other than the match and the env permit.
	StepTieRefusal
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
	case StepTieRefusal:
		return "tie refusal"
	default:
		return "none"
	}
}

// ChainLayer is one layer of a wrapper chain: the argv at an offset into the typed
// argv, from the top command inward. The last layer of a chain is the one behind
// the verdict.
//
// Candidates are the deciding set of the step that settled the layer: the ordinary
// allows no other allow outranks, or the wrapper rules the peel chose. They are the
// rules whether or not their env check passed. PathForm reports whether every
// candidate is a path-form rule. A caller that pins the inner executable reads both.
type ChainLayer struct {
	Offset int
	Argv   []string

	// Resolution is the inner layer's resolution. It is nil for the top layer, whose
	// resolution the caller supplied in the Subject.
	Resolution *model.Resolution

	// Wrappers are the wrapper rules peeled out of this layer: the longest match, then the
	// path-form ones, then the most specific layer. It is empty on the innermost layer.
	Wrappers []*Rule

	// Rule is the rule that settled this layer: a wrapper rule on a peeled layer, the
	// deciding rule on the last one. It is nil on a prompt.
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
//
// Deciding is the deciding set behind Rule: the matching rules no other matching rule
// outranks, in whole-rule order. A tie of allows, or of equally specific denies or
// restricts, holds more than one.
//
// Rule is the first of the ordinary tied rules by whole-rule key, or the rule an env refusal
// names. When a wrapper chain ties with ordinary allows, Rule is still an ordinary one, and the
// chain's inner rules follow in Deciding. It never depends on the order of the rules in a file.
//
// For a chain, Rule and Deciding come from the innermost layer. Step is the step that
// settled the typed command.
type Verdict struct {
	Decision   Decision
	Rule       *Rule
	Deciding   []*Rule
	Restricted bool
	BadEnvs    []string
	Step       Step
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
// At each layer the matching allows are ranked by specificity, and the deciding set
// is every allow that no other outranks. A wrapper peel joins the ranking as a chain
// whenever its inner layer settles on a matching allow. The deciding set decides the
// whole call, and a rule outside it supplies nothing:
//
//  1. One deciding rule runs the call when it passes its env check, and otherwise
//     refuses with its env refusal.
//  2. A tied set runs only when every rule passes its env check and the rules agree
//     on every field other than the match and the env permit. Otherwise it refuses.
//     An env refusal names the rule missing the fewest variables, then the rule
//     whose whole text sorts first.
//  3. A chain decides by its peel. Every wrapper rule it chose must permit the
//     override, and so must every layer inside it.
//  4. With no allow deciding, a matching restrict rule refuses. A peel that did not
//     compete is not an allow carve-out.
//  5. Otherwise the peel's verdict stands, prompt or refusal. With no peel, the
//     layer prompts.
//
// A prompt with a dangerous override becomes a refusal, since a command with no
// matching rule has no permit_unsafe_envs list to grant an exemption. Permits are
// never pooled across rules, and the deciding set does not depend on allow order.
//
// allow-all and deny-all short-circuit before any rule is consulted.
func (rs *Ruleset) Evaluate(subj Subject, env map[string]string, resolve Resolver) Verdict {
	if len(subj.Argv) == 0 {
		return Verdict{Decision: DecisionRefuse}
	}

	switch rs.Mode {
	case ModeAllowAll:
		return rs.EvaluateAllowAll(subj, resolve)
	case ModeDenyAll:
		return Verdict{Decision: DecisionRefuse}
	}

	return rs.newEvaluation(subj, env, resolve).run()
}

// EvaluateAllowAll is Evaluate's answer under mode allow-all, which the server also uses under
// --blindly-allow. Every command runs. The allow tier still ranks, so the deciding rule can supply its
// sandbox, and a wrapper still peels to the inner rule that decides.
//
// Deny, restrict, and the dangerous-environment gate stay off, as they are in this mode. A command no
// allow rule matches runs with no rule and so no sandbox. The one refusal is a tie of allow rules that
// disagree on a field, since picking one would let the order of the file choose the sandbox.
func (rs *Ruleset) EvaluateAllowAll(subj Subject, resolve Resolver) Verdict {
	if len(subj.Argv) == 0 {
		return Verdict{Decision: DecisionRefuse}
	}

	e := rs.newEvaluation(subj, nil, resolve)
	v := e.verdict(e.settle(0))

	switch {
	case v.Decision == DecisionRun:
		return v
	case v.Decision == DecisionRefuse && v.tieRefused():
		return v
	}

	return Verdict{Decision: DecisionRun}
}

// tieRefused reports whether the verdict is a refusal from allow rules that disagree, at the typed
// command or at a layer behind a wrapper.
func (v Verdict) tieRefused() bool {
	if v.Step == StepTieRefusal {
		return true
	}

	for _, layer := range v.Chain {
		if layer.Step == StepTieRefusal {
			return true
		}
	}

	return false
}

// Sandbox names the sandbox the verdict runs the command under, or "" for none. It comes from the
// deciding rule. A chain's deciding rule is the inner command's, so a wrapped command runs under the
// sandbox of its inner rule. A refusal and a prompt run nothing.
func (v Verdict) Sandbox() string {
	if v.Decision != DecisionRun || v.Rule == nil {
		return ""
	}

	return v.Rule.Sandbox
}

func (rs *Ruleset) newEvaluation(subj Subject, env map[string]string, resolve Resolver) *evaluation {
	return &evaluation{rs: rs, env: env, resolve: resolve, top: subj, layers: make(map[int]*layer)}
}

// run evaluates the typed command: the deny hoist first, then the allow tier from the top layer.
func (e *evaluation) run() Verdict {
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
//
// deciding is the deciding set behind rule.
//
// candidates is the deciding set of the step that settled this layer. Pinning reads it.
//
// members is non-empty only when the layer settled on a matching allow. It holds the ranks the layer
// competes at inside an outer peel.
//
// chainInner is set on a peel with members. It is the inner layer's deciding set, the rules the chain
// stands for. It differs from deciding when the peel refused because a wrapper lacks the env permit,
// since deciding then names that wrapper alone.
type settled struct {
	offset     int
	decision   Decision
	rule       *Rule
	restricted bool
	badEnvs    []string

	step       Step
	own        *Rule
	deciding   []*Rule
	candidates []*Rule
	members    []rank
	chainInner []*Rule

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

	l.canonical, l.resolved = e.rs.aliased(l.canonical), e.rs.aliased(l.resolved)

	e.findPeels(l)
	l.sortSkips()
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

// sortSkips orders a layer's skips by whole rule, so the report does not depend on list order.
func (l *layer) sortSkips() {
	slices.SortStableFunc(l.skips, func(a, b Skip) int {
		return strings.Compare(wholeRuleKey(a.Rule), wholeRuleKey(b.Rule))
	})
}

// allowPeel returns the wrapper rules the allow tier peels a layer through, and the tokens they
// remove.
//
// The longest match wins, then a path-form rule over a bare-name one, then the more specific layer.
//
// Rules equal on all three peel to the same inner argv and form a tie, in whole-rule order.
func (l *layer) allowPeel() ([]*Rule, int) {
	if l.listed || len(l.peels) == 0 {
		return nil, 0
	}

	best := l.peels[0]
	for _, p := range l.peels[1:] {
		if peelBetter(p, best) {
			best = p
		}
	}

	var chosen []*Rule
	for _, p := range l.peels {
		if !peelBetter(p, best) && !peelBetter(best, p) {
			chosen = append(chosen, p.rule)
		}
	}

	sortRules(chosen)

	return chosen, best.n
}

// peelBetter reports whether wrapper match a is chosen over b: longer, then path-form, then the more
// specific layer.
func peelBetter(a, b wrapperMatch) bool {
	if a.n != b.n {
		return a.n > b.n
	}

	ra, rb := a.rule.ranking(), b.rule.ranking()
	if ra.path != rb.path {
		return ra.path
	}

	return ra.layer < rb.layer
}

// peelEdge is a wrapper rule that reaches a layer from a parent layer.
type peelEdge struct {
	parent int
	rule   *Rule
	taken  bool
}

// betterEdge picks, among the wrapper rules that reach one layer, the path the deny hoist reports.
//
// It prefers a peel the allow tier takes, then the smaller parent offset, which is the outermost
// parent and the fewest hops. Then it prefers a path-form rule, then the more specific layer, then the
// rule's whole text. None of those depends on list order.
func betterEdge(a, b peelEdge) bool {
	if a.taken != b.taken {
		return a.taken
	}
	if a.parent != b.parent {
		return a.parent < b.parent
	}

	ra, rb := a.rule.ranking(), b.rule.ranking()
	if ra.path != rb.path {
		return ra.path
	}
	if ra.layer != rb.layer {
		return ra.layer < rb.layer
	}

	return wholeRuleKey(a.rule) < wholeRuleKey(b.rule)
}

// hoistDeny checks deny on the top command and on every layer reachable through a
// matching wrapper rule, before any allow is consulted. A deny on any layer refuses
// the call. It follows every matching rule, listed programs included, and checks
// layers whose argv[0] does not resolve, since a bare-name deny still matches them
// through the basename form.
//
// The refusal names the outermost layer with a matching deny: the smallest offset. The
// wrapper path to that layer is chosen by betterEdge, so it never depends on list order.
func (e *evaluation) hoistDeny() (Verdict, bool) {
	edges := make(map[int]peelEdge)

	for offset := 0; offset < len(e.top.Argv); offset++ {
		if _, reached := edges[offset]; offset > 0 && !reached {
			continue
		}

		l := e.layerAt(offset)
		if deciding := e.denying(l); len(deciding) > 0 {
			return e.denyVerdict(offset, edges, deciding), true
		}

		taken, _ := l.allowPeel()
		for _, p := range l.peels {
			edge := peelEdge{parent: offset, rule: p.rule, taken: slices.Contains(taken, p.rule)}
			child := offset + p.n

			if cur, ok := edges[child]; !ok || betterEdge(edge, cur) {
				edges[child] = edge
			}
		}
	}

	return Verdict{}, false
}

// denyVerdict builds the refusal for the deciding set of denies at offset, with the wrapper chain
// that leads there.
func (e *evaluation) denyVerdict(offset int, edges map[int]peelEdge, deciding []*Rule) Verdict {
	path := []int{offset}
	for o := offset; o != 0; {
		o = edges[o].parent
		path = append(path, o)
	}
	slices.Reverse(path)

	chain := make([]ChainLayer, 0, len(path))
	for i, off := range path {
		pl := e.layerAt(off)
		cl := ChainLayer{Offset: pl.offset, Argv: pl.argv, Resolution: pl.resolution}

		if i < len(path)-1 {
			cl.Step = StepPeel
			cl.Wrappers = []*Rule{edges[path[i+1]].rule}
		} else {
			cl.Rule, cl.Step = deciding[0], StepDeny
			cl.Candidates, cl.PathForm = deciding, pathForm(deciding)
		}

		chain = append(chain, cl)
	}

	v := Verdict{Decision: DecisionRefuse, Rule: deciding[0], Deciding: deciding, Step: StepDeny, Skipped: e.skipped()}
	if len(chain) > 1 {
		v.Chain = chain
	}

	return v
}

// denying returns the deciding set of the deny rules matching a layer, in whole-rule order. On an
// inner layer a path-form exact or prefix deny also matches by the basename of its first token. The
// wrapper looks the inner program up itself, so it may find the denied file even when the server PATH
// finds another. Such a deny ranks as a bare-name rule. The top layer keeps exact path matching,
// since the supervisor runs the path cg resolved.
func (e *evaluation) denying(l *layer) []*Rule {
	var items []ranked

	for i := range e.rs.Deny {
		rule := &e.rs.Deny[i]
		rk := rule.ranking()

		switch {
		case ruleMatches(rule, l.canonical, l.resolved, l.basename):
		case l.offset > 0 && rule.baseArgv != nil && (rule.kind == KindExact || rule.kind == KindPrefix) &&
			matchTokens(rule.baseArgv, l.basename, rule.kind == KindExact):
			rk.path = false
		default:
			continue
		}

		items = append(items, ranked{rule: rule, rank: rk})
	}

	return decidingRules(items)
}

// settle runs the allow tier for the layer at offset, memoized.
func (e *evaluation) settle(offset int) *settled {
	l := e.layerAt(offset)
	if l.settled == nil {
		l.settled = e.settleLayer(l)
	}

	return l.settled
}

// matching returns the rules of a tier that match a layer as ordinary rules.
func matching(rules []Rule, l *layer) []*Rule {
	var out []*Rule

	for i := range rules {
		if ruleMatches(&rules[i], l.canonical, l.resolved, l.basename) {
			out = append(out, &rules[i])
		}
	}

	return out
}

// rankedRules pairs each rule with its own rank.
func rankedRules(rules []*Rule) []ranked {
	out := make([]ranked, len(rules))
	for i, rule := range rules {
		out[i] = ranked{rule: rule, rank: rule.ranking()}
	}

	return out
}

// layerPeel settles the peel the allow tier takes through a layer, or returns nil when it takes none.
func (e *evaluation) layerPeel(l *layer) *settled {
	wrappers, n := l.allowPeel()
	if len(wrappers) == 0 {
		return nil
	}

	return e.peelOf(l, wrappers, e.settle(l.offset+n))
}

// decidingAllows ranks the matching allows of a layer against the chain its peel settles. It returns
// the ordinary allows in the deciding set, and whether the peel is in it too.
//
// Both the verdict and the explain call read it. Explain takes its level 0 from it and applies it
// again to the remainder for later levels, so the listing `cg check` prints cannot rank differently
// from the verdict. The matching list is an argument, never a result. The verdict reads only the
// deciding set it returns, and is never handed the rules the ranking left out.
func decidingAllows(matched []*Rule, peel *settled) (tied []*Rule, chainIn bool) {
	ordinary := decidingRules(rankedRules(matched))
	if peel == nil || len(peel.members) == 0 {
		return ordinary, false
	}

	return rankAgainstChain(matched, ordinary, peel.members)
}

func (e *evaluation) settleLayer(l *layer) *settled {
	peel := e.layerPeel(l)
	tied, chainIn := decidingAllows(matching(e.rs.Allow, l), peel)

	switch {
	case len(tied) > 0 && chainIn:
		return e.settleTie(l, tied, peel)
	case len(tied) > 0:
		return e.settleTie(l, tied, nil)
	case chainIn:
		return peel
	}

	if deciding := decidingRules(rankedRules(matching(e.rs.Restrict, l))); len(deciding) > 0 {
		return &settled{
			offset: l.offset, decision: DecisionRefuse, rule: deciding[0], own: deciding[0], step: StepRestrict, restricted: true,
			deciding: deciding, candidates: deciding,
		}
	}

	if peel != nil {
		return peel
	}

	s := &settled{offset: l.offset, decision: DecisionPrompt}
	if bad := (&Rule{}).DisallowedEnvs(e.env); len(bad) > 0 {
		s.decision, s.badEnvs, s.step = DecisionRefuse, bad, StepEnvRefusal
	}

	return s
}

// rankAgainstChain ranks a chain against the matching allows. It returns the ordinary allows left in
// the deciding set, which are those the chain does not outrank, and whether the chain itself stays
// in it, which is when no matching allow outranks the chain.
func rankAgainstChain(matched, ordinary []*Rule, members []rank) ([]*Rule, bool) {
	chainIn := true
	for _, rule := range matched {
		if compareChain(members, rule.ranking()) == allowAbove {
			chainIn = false
			break
		}
	}

	var kept []*Rule
	for _, rule := range ordinary {
		if compareChain(members, rule.ranking()) != chainAbove {
			kept = append(kept, rule)
		}
	}

	return kept, chainIn
}

// peelOf settles a layer by the peel through the chosen wrapper rules and the settled inner layer.
//
// Every chosen wrapper rule must permit the override. The peel runs only when they all do and the
// inner layer runs.
//
// The settled layer carries members only when the inner layer settled on a matching allow. That is
// when the peel competes with the ordinary allows.
func (e *evaluation) peelOf(l *layer, wrappers []*Rule, inner *settled) *settled {
	s := &settled{offset: l.offset, wrappers: wrappers, candidates: wrappers}

	var permitting, failing []*Rule
	for _, rule := range wrappers {
		if len(rule.DisallowedEnvs(e.env)) == 0 {
			permitting = append(permitting, rule)
		} else {
			failing = append(failing, rule)
		}
	}

	if len(inner.members) > 0 {
		s.members = chainMembers(wrappers, inner.members)
		s.chainInner = inner.deciding
	}

	switch {
	case len(failing) == 0 && inner.decision == DecisionRun:
		s.decision, s.rule, s.own, s.step, s.next = DecisionRun, inner.rule, permitting[0], StepPeel, inner
		s.deciding = inner.deciding
	case inner.decision == DecisionRefuse || len(failing) == 0:
		s.decision, s.rule, s.restricted, s.badEnvs = inner.decision, inner.rule, inner.restricted, inner.badEnvs
		s.step, s.next, s.deciding = StepInner, inner, inner.deciding
		if len(failing) == 0 {
			s.own = permitting[0]
		}
	default:
		s.own, s.badEnvs = leastDisallowed(failing, e.env)
		s.decision, s.rule, s.step, s.deciding = DecisionRefuse, s.own, StepEnvRefusal, []*Rule{s.own}
	}

	return s
}

// allowFields is every per-rule field of an allow rule other than the match and permit_unsafe_envs.
// Add a field here and the tie check compares it.
type allowFields struct {
	Sandbox string
}

// fieldsOf reads the fields a tied rule is compared on.
func fieldsOf(r *Rule) allowFields { return allowFields{Sandbox: r.Sandbox} }

// fieldsAgree reports whether every rule of a tied set carries the same fields. A chain's fields are
// those of its inner deciding rules.
func fieldsAgree(tied []*Rule, chain *settled) bool {
	rules := tied
	if chain != nil {
		rules = append(slices.Clone(tied), chain.deciding...)
	}

	for _, rule := range rules[1:] {
		if fieldsOf(rule) != fieldsOf(rules[0]) {
			return false
		}
	}

	return true
}

// settleTie settles a layer whose deciding set holds the ordinary allows in tied and, when chain is
// not nil, the peel.
//
// Every member must run after its env check, and the members must agree on every field. Otherwise the
// call is refused.
//
// Among env refusals, the one missing the fewest variables is named. Then the rule whose whole text
// sorts first is named.
func (e *evaluation) settleTie(l *layer, tied []*Rule, chain *settled) *settled {
	type refusal struct {
		rule *Rule
		bad  []string
		peel bool
	}

	var refusals []refusal
	for _, rule := range tied {
		if bad := rule.DisallowedEnvs(e.env); len(bad) > 0 {
			refusals = append(refusals, refusal{rule: rule, bad: bad})
		}
	}

	if chain != nil && chain.decision != DecisionRun {
		refusals = append(refusals, refusal{rule: chain.rule, bad: chain.badEnvs, peel: true})
	}

	s := &settled{offset: l.offset, candidates: slices.Clone(tied), members: rankList(rankedRules(tied))}

	deciding := slices.Clone(tied)
	if chain != nil {
		s.candidates = append(s.candidates, chain.candidates...)
		s.members = append(s.members, chain.members...)
		deciding = append(deciding, chain.deciding...)
	}
	sortRules(deciding)
	s.deciding = deciding

	if len(refusals) > 0 {
		best := refusals[0]
		for _, r := range refusals[1:] {
			if len(r.bad) < len(best.bad) || (len(r.bad) == len(best.bad) && wholeRuleKey(r.rule) < wholeRuleKey(best.rule)) {
				best = r
			}
		}

		if best.peel {
			return chain
		}

		s.decision, s.rule, s.own, s.step, s.badEnvs = DecisionRefuse, best.rule, best.rule, StepEnvRefusal, best.bad

		return s
	}

	s.rule, s.own = tied[0], tied[0]
	s.decision, s.step = DecisionRun, StepAllow

	if !fieldsAgree(tied, chain) {
		s.decision, s.step = DecisionRefuse, StepTieRefusal
	}

	return s
}

// rankList returns the ranks of ranked rules.
func rankList(items []ranked) []rank {
	out := make([]rank, len(items))
	for i, it := range items {
		out[i] = it.rank
	}

	return out
}

// leastDisallowed picks, from a set of wrapper rules none of which permits the
// override, the rule closest to permitting it: the one missing the fewest variables,
// with ties broken by the rule's whole text. The choice does not depend on allow
// order, which remember re-sorts, so the refusal names the same fix every time.
func leastDisallowed(rules []*Rule, env map[string]string) (*Rule, []string) {
	var best *Rule
	var bestBad []string

	for _, rule := range rules {
		bad := rule.DisallowedEnvs(env)
		if best == nil || len(bad) < len(bestBad) ||
			(len(bad) == len(bestBad) && wholeRuleKey(rule) < wholeRuleKey(best)) {
			best, bestBad = rule, bad
		}
	}

	return best, bestBad
}

// verdict turns the top layer's outcome into a Verdict, walking the peel chain
// when a wrapper rule was involved.
func (e *evaluation) verdict(top *settled) Verdict {
	v := Verdict{
		Decision:   top.decision,
		Rule:       top.rule,
		Deciding:   top.deciding,
		Restricted: top.restricted,
		BadEnvs:    top.badEnvs,
		Step:       top.step,
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
