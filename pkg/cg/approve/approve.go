// Package approve implements the cg_run approval gate: a layered YAML rules
// file, a load-once config store, and an argv-native matcher that decides
// whether a command runs, is refused, or needs an interactive prompt.
//
// The matcher operates on the real argv tokens cg execs, so there is no command
// string an attacker-controlled argument can break out of. Pattern kinds
// (glob/regex) match against a canonical quoted-argv join that is built only for
// matching and never passed to a shell.
package approve

import (
	"errors"
	"regexp"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Mode is the top-level enforcement mode for a ruleset.
type Mode string

const (
	// ModeEnforce consults deny/allow rules and prompts on no match. It is the
	// default when a file omits the mode key.
	ModeEnforce Mode = "enforce"
	// ModeAllowAll short-circuits every command to run.
	ModeAllowAll Mode = "allow-all"
	// ModeDenyAll short-circuits every command to refuse.
	ModeDenyAll Mode = "deny-all"
)

// RuleKind identifies which of the four matching strategies a rule uses. Each
// rule carries exactly one; the loader enforces that.
type RuleKind int

const (
	// KindExact compares the full argv element-wise.
	KindExact RuleKind = iota
	// KindPrefix compares the leading argv tokens element-wise.
	KindPrefix
	// KindGlob matches a flat glob, compiled to RE2, against the quoted join.
	KindGlob
	// KindRegex matches an RE2 pattern against the quoted join.
	KindRegex
)

// Rule is one allow or deny entry. Exactly one of Exact/Prefix/Glob/Regex is
// populated after validation; kind records which. Message is valid only on deny
// rules and PermitUnsafeEnvs only on allow rules. AsBasename, valid on both,
// matches the basename form of the subject instead of the canonical form.
type Rule struct {
	Exact  []string `yaml:"exact,omitempty"`
	Prefix []string `yaml:"prefix,omitempty"`
	Glob   string   `yaml:"glob,omitempty"`
	Regex  string   `yaml:"regex,omitempty"`

	AsBasename       bool     `yaml:"as_basename,omitempty"`
	Message          string   `yaml:"message,omitempty"`
	PermitUnsafeEnvs []string `yaml:"permit_unsafe_envs,omitempty"`

	kind     RuleKind
	compiled *regexp.Regexp
}

// Kind reports which matching strategy this rule uses, set by the loader.
func (r *Rule) Kind() RuleKind { return r.kind }

// Document is the typed decode of one approve.yaml layer.
type Document struct {
	Version int    `yaml:"version"`
	Mode    Mode   `yaml:"mode,omitempty"`
	Deny    []Rule `yaml:"deny,omitempty"`
	Allow   []Rule `yaml:"allow,omitempty"`
}

// Layer is one decoded file plus its provenance. Node and Snapshot are held for
// later round-trip writes and divergence detection; the matcher never reads
// them. A missing file yields Present=false with the other fields zero.
type Layer struct {
	Path     string
	Node     *yaml.Node
	Doc      *Document
	Snapshot []byte
	Present  bool
}

// Ruleset is the frozen, merged policy the matcher evaluates. It is built once
// at load and never mutated; the matcher consults only this and never touches
// disk.
type Ruleset struct {
	Mode  Mode
	Deny  []Rule
	Allow []Rule
}

// Store owns the two layers and the live ruleset. It performs disk I/O at load
// time and again only when a remembered rule is persisted to the project file.
//
// The ruleset is held behind an atomic pointer so the matcher always reads an
// immutable snapshot: a remember-accept builds a new ruleset and swaps the
// pointer rather than mutating the live one, which keeps Match race-free against
// a concurrent persist. mu serializes the project write path so concurrent
// writers cannot interleave a read-modify-write of the file.
type Store struct {
	Global  Layer
	Project Layer

	mu    sync.Mutex
	rules atomic.Pointer[Ruleset]
}

// Ruleset returns the current ruleset the matcher evaluates.
func (s *Store) Ruleset() *Ruleset { return s.rules.Load() }

// Subject is the command representation the matcher evaluates. Argv is the
// original argv as invoked. Canonical is the resolved, symlink-evaluated argv
// whose first element is the absolute executable path and whose tail mirrors
// Argv[1:]; it is nil when the executable could not be resolved or canonicalized.
//
// Rules match Canonical by default, so a non-basename rule cannot match when
// Canonical is nil. A rule with AsBasename set instead matches a form derived
// from filepath.Base(Argv[0]), the invoked token, so name-based rules still
// evaluate even when canonicalization fails.
type Subject struct {
	Argv      []string
	Canonical []string
}

// Decision is the matcher's verdict for a command.
type Decision int

const (
	// DecisionRun means the command is allowed (allow-all or an allow match).
	DecisionRun Decision = iota
	// DecisionRefuse means the command is blocked (deny-all or a deny match).
	DecisionRefuse
	// DecisionPrompt means nothing matched; the caller should elicit approval.
	DecisionPrompt
)

// MatchResult carries the verdict plus the rule that produced it. Rule is the
// matched deny or allow rule, and is nil for allow-all, deny-all, and prompt.
// Callers read Rule.Message on a deny and Rule.PermitUnsafeEnvs on an allow.
type MatchResult struct {
	Decision Decision
	Rule     *Rule
}

// Loader and merge errors. They are wrapped with the file path and, where a
// node is available, the offending line.
var (
	ErrUnknownVersion    = errors.New("unknown version (expected 1)")
	ErrUnknownMode       = errors.New("unknown mode (expected enforce, allow-all, or deny-all)")
	ErrNoRuleKind        = errors.New("rule has no rule-kind key (need one of exact, prefix, glob, or regex)")
	ErrMultipleRuleKinds = errors.New("rule has more than one rule-kind key")
	ErrMessageOnAllow    = errors.New("message is not valid on an allow rule; use a YAML comment instead")
	ErrPermitOnDeny      = errors.New("permit_unsafe_envs is not valid on a deny rule")
)
