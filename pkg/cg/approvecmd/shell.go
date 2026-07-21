package approvecmd

import (
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// extractShellCommands parses src as a Bash command line and returns the argv
// of every simple command it contains. It walks the full syntax tree, not just
// the top-level statement list, so a command reached through chaining (&&, ||,
// |, ;, &), a subshell, a compound command body (if/for/while/case/{ }), or a
// command substitution ($(...) or backticks) is still found. That last case is
// the one --shell exists to close: a string like `echo $(rm -rf /)` looks like
// a single call at the top level, but the substitution runs a full command
// before echo ever does.
//
// Each argument word is resolved with expand.Literal under a zero-value
// expand.Config, which strips quoting (so a quoted literal like "fix &&
// feature" reads back as that literal string rather than being lost) without
// touching environment or executing anything: Env is nil, so $VAR expands to
// "", and CmdSubst is nil, so a $(...) or backtick substitution embedded in a
// word returns an error instead of running. The standalone-command case, a
// $(...) that is itself a full command rather than embedded in a larger word,
// is unaffected: it is walked and checked independently below, since it is
// its own *syntax.CallExpr in the tree. A word that fails to resolve this way
// becomes an empty token; that cannot match an exact/prefix rule and
// canonicalizes to a lookup failure, so a command built from unresolved
// dynamic content falls through to the existing prompt-or-refuse handling for
// an unmatched or unresolvable command rather than being silently allowed.
func extractShellCommands(src string) ([][]string, error) {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(src), "")
	if err != nil {
		return nil, err
	}

	var commands [][]string
	syntax.Walk(f, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		argv := make([]string, len(call.Args))
		for i, w := range call.Args {
			if lit, litErr := expand.Literal(nil, w); litErr == nil {
				argv[i] = lit
			}
		}
		commands = append(commands, argv)
		return true
	})

	return commands, nil
}
