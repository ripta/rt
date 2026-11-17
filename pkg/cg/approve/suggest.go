package approve

import (
	"path/filepath"
	"strings"

	"github.com/ripta/rt/pkg/cg/model"
)

// Suggestion is the allow rule an approval prompt pre-fills for a command.
//
// Every command gets its whole argv, with the path the rule pins as the first token. The kind of
// the rule that gets saved is not part of the suggestion. It follows from the edit, which
// CheckEdit judges. A command whose program heads a wrapper rule also carries a note, because a
// rule for it must stay exact.
type Suggestion struct {
	Tokens []string

	// HeadsWrapper reports that the command's program heads a wrapper rule, so a rule for it
	// must stay exact.
	HeadsWrapper bool

	// NearMiss is the wrapper rule that matched the command but did not peel, because the
	// inner command starts with '-'. It is nil otherwise.
	NearMiss *Rule
}

// Suggest derives the rule pre-filled into the prompt for argv, resolved as res. The rule is
// the whole argv. Its first token is the program's path, spelled as portableHead chooses:
// project-relative, ~/..., or absolute. It is argv[0] as written when resolution found no path.
//
// "Heads a wrapper rule" reads the first token the way matching does: a bare token compares
// with the invoked basename, and a path token compares with the canonical or resolved path.
func (rs *Ruleset) Suggest(argv []string, res *model.Resolution) Suggestion {
	if len(argv) == 0 {
		return Suggestion{}
	}

	execPath := res.RulePath()
	canonical, resolved, basename := resolutionForms(argv, res)
	canonical, resolved = rs.aliased(canonical), rs.aliased(resolved)

	var heads bool
	var near *Rule

	for i := range rs.Allow {
		rule := &rs.Allow[i]
		if rule.kind != KindWrapper || len(rule.cmpArgv) == 0 || !headMatches(rule, canonical, resolved, basename) {
			continue
		}

		heads = true

		if !tokenRuleMatches(rule, canonical, resolved, basename) || !innerStartsWithDash(argv, len(rule.cmpArgv)) {
			continue
		}

		if near == nil || len(rule.cmpArgv) > len(near.cmpArgv) {
			near = rule
		}
	}

	head := rs.portableHead(execPath, canonical, resolved, basename)
	if head == "" {
		head = argv[0]
	}

	tokens := append([]string{head}, argv[1:]...)

	return Suggestion{Tokens: tokens, HeadsWrapper: heads, NearMiss: near}
}

// portableHead spells a program as a path that works for anyone, in any checkout and on any
// machine. The project file is committed, and an absolute path would hold one person's home
// directory and one worktree's name.
//
// A program in the project gets a project-relative spelling. A program under the home directory
// and outside the project gets ~/... instead. Each spelling is offered only when it matches the
// command, so a project root or home directory reached through a symlink keeps the absolute path
// that does. Any other path comes back unchanged.
func (rs *Ruleset) portableHead(path string, canonical, resolved, basename matchForm) string {
	if !filepath.IsAbs(path) {
		return path
	}

	if rs.projectRoot != "" {
		if rel, inside := relativeTo(rs.projectRoot, rs.aliasedPath(path)); inside {
			if head := rs.relativeHead("./"+rel, canonical, resolved, basename); head != "" {
				return head
			}

			return path
		}
	}

	// A root home would claim every absolute path as ~/...
	if rs.tilde == nil || rs.tilde.home == "" || filepath.Dir(rs.tilde.home) == rs.tilde.home {
		return path
	}

	if rel, inside := relativeTo(rs.tilde.home, path); inside {
		if head := rs.relativeHead("~/"+rel, canonical, resolved, basename); head != "" {
			return head
		}
	}

	return path
}

// aliasedPath maps a path in a linked worktree to the same file in the project checkout.
func (rs *Ruleset) aliasedPath(path string) string {
	if rs.checkoutAlias != nil {
		if alias := rs.checkoutAlias(path); alias != "" {
			return alias
		}
	}

	return path
}

// relativeTo returns the slash-separated path below dir, and whether path lies strictly inside it.
func relativeTo(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return filepath.ToSlash(rel), true
}

// relativeHead compiles a candidate first token the way a load does, and returns it only when it
// matches the command. A ~/ candidate takes its home from the ruleset's own environment.
func (rs *Ruleset) relativeHead(candidate string, canonical, resolved, basename matchForm) string {
	rule := Rule{Prefix: []string{candidate}, kind: KindPrefix}

	if err := rs.tilde.expandRule(&rule, sectionAllow); err != nil || rule.inert {
		return ""
	}

	compileMatch(&rule, rs.projectRoot)

	if !tokenRuleMatches(&rule, canonical, resolved, basename) {
		return ""
	}

	return candidate
}

// headMatches reports whether a wrapper rule's first token names the program in the forms.
func headMatches(rule *Rule, canonical, resolved, basename matchForm) bool {
	first := rule.cmpArgv[0]

	if rule.wantBasename {
		return basename.ok && basename.head == first
	}

	if (canonical.ok && canonical.head == first) || (resolved.ok && resolved.head == first) {
		return true
	}

	return rule.relative && ((canonical.ok && canonical.alias == first) || (resolved.ok && resolved.alias == first))
}
