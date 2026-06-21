package approve

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ruleKindKeys are the four mutually exclusive rule-kind keys. Exactly one must
// appear in each allow/deny/restrict entry.
var ruleKindKeys = map[string]RuleKind{
	"exact":  KindExact,
	"prefix": KindPrefix,
	"glob":   KindGlob,
	"regex":  KindRegex,
}

// ruleSection identifies which top-level sequence a rule belongs to. It drives
// the field constraints: message is valid on deny and restrict, permit_unsafe_envs
// only on allow.
type ruleSection int

const (
	sectionDeny ruleSection = iota
	sectionAllow
	sectionRestrict
)

// ParseDocument decodes one approve.yaml layer's bytes into both a yaml.Node
// (retained for later round-trip writes) and a validated, compiled Document.
// Errors carry the offending line; callers wrap them with the file path.
func ParseDocument(raw []byte) (*yaml.Node, *Document, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, nil, err
	}

	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}

	if err := validateDocument(&node, &doc); err != nil {
		return nil, nil, err
	}

	return &node, &doc, nil
}

// validateDocument enforces the version and mode, then validates and compiles
// every allow/deny entry. It pairs each typed rule with its source node by
// index so error messages can carry a line number and so the single rule kind
// can be recorded on the typed rule.
func validateDocument(node *yaml.Node, doc *Document) error {
	if doc.Version != 1 {
		return ErrUnknownVersion
	}

	switch doc.Mode {
	case "", ModeEnforce, ModeAllowAll, ModeDenyAll:
	default:
		return fmt.Errorf("%w: %q", ErrUnknownMode, doc.Mode)
	}

	root := rootMapping(node)

	if err := validateEntries(findMapValue(root, "deny"), doc.Deny, sectionDeny); err != nil {
		return err
	}
	if err := validateEntries(findMapValue(root, "allow"), doc.Allow, sectionAllow); err != nil {
		return err
	}
	if err := validateEntries(findMapValue(root, "restrict"), doc.Restrict, sectionRestrict); err != nil {
		return err
	}

	return nil
}

// validateEntries validates each entry in a deny, allow, or restrict sequence,
// records its rule kind on the matching typed rule, and compiles glob/regex
// patterns. seq is the YAML sequence node and may be nil when the key is absent.
func validateEntries(seq *yaml.Node, rules []Rule, section ruleSection) error {
	for i := range rules {
		var entry *yaml.Node
		if seq != nil && i < len(seq.Content) {
			entry = seq.Content[i]
		}

		if err := validateRule(entry, &rules[i], section); err != nil {
			return err
		}
		if err := compileRule(&rules[i]); err != nil {
			return err
		}
	}

	return nil
}

// validateRule inspects the entry's mapping keys to enforce exactly one rule
// kind and the per-section field constraints, then records the kind on the typed
// rule. Inspecting the node keys distinguishes a present-but-empty value from an
// absent key, which the typed decode alone cannot.
func validateRule(entry *yaml.Node, rule *Rule, section ruleSection) error {
	line := 0
	if entry != nil {
		line = entry.Line
	}

	var kinds []RuleKind
	var hasMessage, hasPermit, hasAsBasename bool
	if entry != nil && entry.Kind == yaml.MappingNode {
		for k := 0; k+1 < len(entry.Content); k += 2 {
			key := entry.Content[k].Value
			if kind, ok := ruleKindKeys[key]; ok {
				kinds = append(kinds, kind)
				continue
			}
			switch key {
			case "message":
				hasMessage = true
			case "permit_unsafe_envs":
				hasPermit = true
			case "as_basename":
				hasAsBasename = true
			}
		}
	}

	switch {
	case len(kinds) == 0:
		return fmt.Errorf("line %d: %w", line, ErrNoRuleKind)
	case len(kinds) > 1:
		return fmt.Errorf("line %d: %w", line, ErrMultipleRuleKinds)
	}

	if hasMessage && section == sectionAllow {
		return fmt.Errorf("line %d: %w", line, ErrMessageOnAllow)
	}
	if hasPermit && section != sectionAllow {
		return fmt.Errorf("line %d: %w", line, ErrPermitNotOnAllow)
	}
	if hasAsBasename && (kinds[0] == KindExact || kinds[0] == KindPrefix) {
		return fmt.Errorf("line %d: %w", line, ErrAsBasenameOnTokens)
	}

	rule.kind = kinds[0]
	return nil
}

// compileRule compiles the RE2 pattern backing a glob or regex rule. exact and
// prefix rules need no compilation.
func compileRule(rule *Rule) error {
	switch rule.kind {
	case KindGlob:
		re, err := regexp.Compile(globToRegex(rule.Glob))
		if err != nil {
			return fmt.Errorf("compiling glob %q: %w", rule.Glob, err)
		}
		rule.compiled = re
	case KindRegex:
		re, err := regexp.Compile(rule.Regex)
		if err != nil {
			return fmt.Errorf("compiling regex %q: %w", rule.Regex, err)
		}
		rule.compiled = re
	}

	return nil
}

// globToRegex compiles a flat glob to an anchored RE2 pattern: '*' becomes '.*',
// '?' becomes '.', and every other regex metacharacter is escaped. The result
// is anchored at both ends because the prefix kind already covers leading-token
// matching, so a glob matches the whole quoted join.
func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteByte('^')
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteByte('$')

	return b.String()
}

// rootMapping returns the root mapping node of a decoded document, or nil if the
// content is not a mapping.
func rootMapping(node *yaml.Node) *yaml.Node {
	n := node
	if n != nil && n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}

	return n
}

// findMapValue returns the value node for key in a mapping node, or nil when the
// mapping or key is absent.
func findMapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}

	return nil
}
