package approve

// builtinDenyRules returns the conservative default-deny set that always leads
// the merged deny list. It covers shells and inline-code interpreter forms,
// whose program token alone (sh, bash) or program-plus-eval-flag (python -c)
// would otherwise wave through arbitrary code in an argument the rules do not
// introspect. Allowlisting an interpreter allowlists everything it can run.
//
// Rules are bare-name prefix kind, so shape inference matches them against the
// invoked token's basename and catches the program however it is spelled (sh,
// /bin/sh, ./sh), even when /bin/sh is a symlink to dash or busybox. The
// two-token interpreter rules additionally pin argv[1] to the eval flag. The
// rules are compiled here so callers that build a ruleset directly, without going
// through buildRuleset, still match; the bare tokens never consult the project
// root, so an empty root is sufficient.
func builtinDenyRules() []Rule {
	tokens := [][]string{
		{"sh"},
		{"bash"},
		{"zsh"},
		{"env"},
		{"xargs"},
		{"python", "-c"},
		{"python3", "-c"},
		{"node", "-e"},
		{"node", "--eval"},
		{"perl", "-e"},
		{"ruby", "-e"},
	}

	rules := make([]Rule, len(tokens))
	for i, t := range tokens {
		rules[i] = Rule{Prefix: t, kind: KindPrefix}
		compileMatch(&rules[i], "")
	}

	return rules
}
