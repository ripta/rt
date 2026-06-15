package approve

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WriteStrategy selects how a remembered rule is reconciled with the project
// file when it is persisted.
type WriteStrategy int

const (
	// WriteDirect writes cg's in-memory document plus the new rule. Used when
	// the on-disk file has not diverged from the load-time snapshot.
	WriteDirect WriteStrategy = iota
	// WriteOverwrite writes cg's in-memory document plus the new rule, dropping
	// any on-disk changes made since load.
	WriteOverwrite
	// WriteReloadMerge rebases on the current on-disk file, unions in the
	// in-memory rules absent from disk, then appends the new rule.
	WriteReloadMerge
)

// multiVerbTools are programs whose first argument is a subcommand, so a useful
// suggested prefix covers argv[0..1] rather than argv[0] alone.
var multiVerbTools = map[string]struct{}{
	"go":      {},
	"git":     {},
	"cargo":   {},
	"kubectl": {},
	"make":    {},
}

// SuggestPrefix derives the prefix rule pre-filled into the approval prompt:
// argv[0], extended to argv[0..1] when the program is a known multi-verb tool,
// a second argument is present, and that argument is not a flag. Flag arguments
// are dropped to avoid rules like [make, -j8] or [git, --no-pager] that match
// only on incidental invocation details rather than the actual operation.
// argv[0] is kept as written so the suggestion reflects what ran; the user can
// edit it before saving.
func SuggestPrefix(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	if _, ok := multiVerbTools[filepath.Base(argv[0])]; ok && len(argv) >= 2 {
		if !strings.HasPrefix(argv[1], "-") {
			return []string{argv[0], argv[1]}
		}
	}

	return []string{argv[0]}
}

// CheckProjectDivergence re-reads the project file and reports whether it
// differs from the snapshot captured at load. current holds the on-disk bytes,
// or nil when the file is absent. Re-reading is only to detect change; the
// content is never silently adopted into the live matcher.
func (s *Store) CheckProjectDivergence() (changed bool, current []byte, err error) {
	raw, err := os.ReadFile(s.Project.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.Project.Present, nil, nil
		}
		return false, nil, fmt.Errorf("reading project rules: %w", err)
	}

	return !bytes.Equal(raw, s.Project.Snapshot), raw, nil
}

// AppendProjectAllowPrefix appends a prefix allow rule to the project file using
// strategy, then swaps the new rule into the live ruleset so a subsequent
// matching command does not re-prompt this session. The write is atomic and
// serialized; the in-memory project layer is refreshed from the bytes written.
func (s *Store) AppendProjectAllowPrefix(tokens []string, strategy WriteStrategy) error {
	if len(tokens) == 0 {
		return fmt.Errorf("prefix rule must have at least one token")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	doc, err := s.baseDocument(strategy)
	if err != nil {
		return err
	}

	root := rootMapping(doc)
	if root == nil {
		return fmt.Errorf("project document has no root mapping")
	}
	asBasename := prefixWantsBasename(tokens)
	allowSeq := ensureSeq(root, "allow")
	allowSeq.Content = append(allowSeq.Content, buildPrefixEntry(tokens, asBasename))
	canonicalizeRuleSeqs(root)

	data, err := renderDocument(doc)
	if err != nil {
		return fmt.Errorf("rendering project rules: %w", err)
	}
	if err := atomicWrite(s.Project.Path, data); err != nil {
		return fmt.Errorf("writing project rules: %w", err)
	}

	node, parsed, err := ParseDocument(data)
	if err != nil {
		return fmt.Errorf("re-parsing written project rules: %w", err)
	}
	s.Project.Node = node
	s.Project.Doc = parsed
	s.Project.Snapshot = data
	s.Project.Present = true

	s.appendLiveAllow(tokens, asBasename)

	return nil
}

// prefixWantsBasename reports whether a remembered prefix rule should match by
// basename. A name-based program token, one with no slash, is matched by the
// invoked basename so the rule fires however the program is spelled or resolved;
// a path-bearing token describes an install location and matches the canonical
// path instead.
func prefixWantsBasename(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}

	return !strings.ContainsRune(tokens[0], filepath.Separator) && !strings.ContainsRune(tokens[0], '/')
}

// baseDocument returns the document node the new rule is appended to. The direct
// and overwrite strategies build on cg's in-memory view; reload-merge rebases on
// the current on-disk file and unions in-memory rules back in.
func (s *Store) baseDocument(strategy WriteStrategy) (*yaml.Node, error) {
	if strategy == WriteReloadMerge {
		return s.reloadMergeBase()
	}

	return s.projectDocOrEmpty(), nil
}

// projectDocOrEmpty returns the in-memory project document, or a fresh empty one
// when no project file was present at load.
func (s *Store) projectDocOrEmpty() *yaml.Node {
	if s.Project.Present && s.Project.Node != nil {
		return s.Project.Node
	}

	return newEmptyProjectDoc()
}

// reloadMergeBase rebases on the current on-disk project file so its hand-edits
// and comments survive, then unions in the in-memory rules that are absent from
// disk. The new rule is appended by the caller.
func (s *Store) reloadMergeBase() (*yaml.Node, error) {
	raw, err := os.ReadFile(s.Project.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return s.projectDocOrEmpty(), nil
		}
		return nil, fmt.Errorf("reading project rules: %w", err)
	}

	node, disk, err := ParseDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing on-disk project rules: %w", err)
	}
	root := rootMapping(node)
	if root == nil {
		return nil, fmt.Errorf("on-disk project rules have no root mapping")
	}

	if s.Project.Doc != nil {
		mergeAbsent(ensureSeq(root, "allow"), s.Project.Doc.Allow, disk.Allow, false)
		if len(s.Project.Doc.Deny) > 0 {
			mergeAbsent(ensureSeq(root, "deny"), s.Project.Doc.Deny, disk.Deny, true)
		}
	}

	return node, nil
}

// mergeAbsent appends nodes for each rule in mem not already present in disk to
// seq, preserving the order the in-memory rules carry.
func mergeAbsent(seq *yaml.Node, mem, disk []Rule, isDeny bool) {
	for i := range mem {
		if !containsRule(disk, &mem[i]) {
			seq.Content = append(seq.Content, buildRuleEntry(&mem[i], isDeny))
		}
	}
}

// appendLiveAllow swaps a new ruleset with tokens appended to Allow into the
// atomic pointer. The deny slice is shared because it is never mutated; the
// allow slice is copied so the live snapshot the matcher reads stays immutable.
// asBasename mirrors what was written to disk so the live rule matches the same
// commands the reloaded file would.
func (s *Store) appendLiveAllow(tokens []string, asBasename bool) {
	rule := Rule{Prefix: slices.Clone(tokens), AsBasename: asBasename, kind: KindPrefix}
	cur := s.rules.Load()
	next := &Ruleset{
		Mode:  cur.Mode,
		Deny:  cur.Deny,
		Allow: append(slices.Clone(cur.Allow), rule),
	}
	s.rules.Store(next)
}

// containsRule reports whether rules holds a rule equal to r by kind and the
// kind's payload.
func containsRule(rules []Rule, r *Rule) bool {
	for i := range rules {
		if ruleEqual(&rules[i], r) {
			return true
		}
	}

	return false
}

// ruleEqual compares two rules by kind and the matching payload. Message and
// permit_unsafe_envs are not part of identity: a rule edited only in its message
// is still the same rule for union purposes.
func ruleEqual(a, b *Rule) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case KindExact:
		return slices.Equal(a.Exact, b.Exact)
	case KindPrefix:
		return slices.Equal(a.Prefix, b.Prefix)
	case KindGlob:
		return a.Glob == b.Glob
	case KindRegex:
		return a.Regex == b.Regex
	}

	return false
}

// buildPrefixEntry builds the mapping node for a prefix allow rule, marking it
// as_basename when the rule should match by basename.
func buildPrefixEntry(tokens []string, asBasename bool) *yaml.Node {
	return buildRuleEntry(&Rule{Prefix: tokens, AsBasename: asBasename, kind: KindPrefix}, false)
}

// buildRuleEntry builds the YAML mapping node for a rule: its single kind key,
// an as_basename flag when set, plus any message (deny) or permit_unsafe_envs
// (allow). Every scalar carries an explicit !!str tag so a token like yes or 123
// round-trips as a string rather than re-parsing as a bool or int.
func buildRuleEntry(rule *Rule, isDeny bool) *yaml.Node {
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	switch rule.kind {
	case KindExact:
		entry.Content = append(entry.Content, strScalar("exact"), flowSeq(rule.Exact))
	case KindPrefix:
		entry.Content = append(entry.Content, strScalar("prefix"), flowSeq(rule.Prefix))
	case KindGlob:
		entry.Content = append(entry.Content, strScalar("glob"), strScalar(rule.Glob))
	case KindRegex:
		entry.Content = append(entry.Content, strScalar("regex"), strScalar(rule.Regex))
	}

	if rule.AsBasename {
		entry.Content = append(entry.Content, strScalar("as_basename"), boolScalar(true))
	}
	if isDeny && rule.Message != "" {
		entry.Content = append(entry.Content, strScalar("message"), strScalar(rule.Message))
	}
	if !isDeny && len(rule.PermitUnsafeEnvs) > 0 {
		entry.Content = append(entry.Content, strScalar("permit_unsafe_envs"), flowSeq(rule.PermitUnsafeEnvs))
	}

	return entry
}

// canonicalizeRuleSeqs sorts and de-duplicates the deny and allow rule
// sequences in root so the written file stays tidy: entries are ordered by their
// command text and exact duplicates are dropped, keeping the first occurrence so
// its comments and any message survive. Rule order does not affect matching,
// which refuses on any deny match and runs on any allow match, so reordering is
// safe.
func canonicalizeRuleSeqs(root *yaml.Node) {
	for _, key := range []string{"deny", "allow"} {
		if seq := findSeq(root, key); seq != nil {
			seq.Content = sortDedupeEntries(seq.Content)
		}
	}
}

// findSeq returns the sequence value node for key in root, or nil when key is
// absent or its value is not a sequence.
func findSeq(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.SequenceNode {
			return root.Content[i+1]
		}
	}

	return nil
}

// sortDedupeEntries returns entries sorted by command text with exact-duplicate
// rules removed, preserving each kept entry's node so its comments stay
// attached. An entry that does not decode to a single recognized rule kind is
// left at the end in its original order and never deduplicated.
func sortDedupeEntries(entries []*yaml.Node) []*yaml.Node {
	type keyed struct {
		node       *yaml.Node
		sortKey    string
		ident      string
		recognized bool
		idx        int
	}

	ks := make([]keyed, len(entries))
	for i, n := range entries {
		sortKey, ident, ok := ruleKeys(n)
		ks[i] = keyed{node: n, sortKey: sortKey, ident: ident, recognized: ok, idx: i}
	}

	sort.SliceStable(ks, func(i, j int) bool {
		if ks[i].recognized != ks[j].recognized {
			return ks[i].recognized
		}
		if !ks[i].recognized {
			return ks[i].idx < ks[j].idx
		}
		if ks[i].sortKey != ks[j].sortKey {
			return ks[i].sortKey < ks[j].sortKey
		}
		return ks[i].ident < ks[j].ident
	})

	out := make([]*yaml.Node, 0, len(ks))
	seen := make(map[string]bool, len(ks))
	for _, k := range ks {
		if k.recognized {
			if seen[k.ident] {
				continue
			}
			seen[k.ident] = true
		}
		out = append(out, k.node)
	}

	return out
}

// ruleKeys derives a sort key, the rule's command text, and an identity key, its
// kind plus payload, from a rule entry node. ok is false when the node does not
// decode to exactly one recognized rule kind.
func ruleKeys(n *yaml.Node) (sortKey, ident string, ok bool) {
	var r Rule
	if err := n.Decode(&r); err != nil {
		return "", "", false
	}

	switch {
	case len(r.Exact) > 0:
		return strings.Join(r.Exact, " "), "exact\x00" + strings.Join(r.Exact, "\x00"), true
	case len(r.Prefix) > 0:
		return strings.Join(r.Prefix, " "), "prefix\x00" + strings.Join(r.Prefix, "\x00"), true
	case r.Glob != "":
		return r.Glob, "glob\x00" + r.Glob, true
	case r.Regex != "":
		return r.Regex, "regex\x00" + r.Regex, true
	}

	return "", "", false
}

// ensureSeq returns the sequence value node for key in root, creating it (or
// replacing a non-sequence value such as an empty null) when needed.
func ensureSeq(root *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			if root.Content[i+1].Kind == yaml.SequenceNode {
				return root.Content[i+1]
			}
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			root.Content[i+1] = seq
			return seq
		}
	}

	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, strScalar(key), seq)

	return seq
}

// newEmptyProjectDoc builds a minimal valid project document for the case where
// no project file existed at load. version is required by the loader, so the
// freshly written file parses on the next start.
func newEmptyProjectDoc() *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = []*yaml.Node{
		strScalar("version"), {Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"},
		strScalar("allow"), {Kind: yaml.SequenceNode, Tag: "!!seq"},
	}

	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
}

// strScalar builds a string scalar node with an explicit tag.
func strScalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// boolScalar builds a boolean scalar node with an explicit tag.
func boolScalar(v bool) *yaml.Node {
	val := "false"
	if v {
		val = "true"
	}

	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: val}
}

// flowSeq builds a flow-style string sequence node ([a, b]) so token lists
// render compactly, matching the canonical format the proposal documents.
func flowSeq(tokens []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, t := range tokens {
		seq.Content = append(seq.Content, strScalar(t))
	}

	return seq
}

// renderDocument marshals a document node to canonical bytes using the repo's
// two-space indent.
func renderDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// atomicWrite writes data to path via a temp file in the same directory and a
// rename, creating the parent directory when absent. The rename keeps concurrent
// readers and writers from observing a torn file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "approve.yaml.tmp-*")
	if err != nil {
		return fmt.Errorf("creating tmpfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing tmpfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing tmpfile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming tmpfile: %w", err)
	}

	return nil
}
