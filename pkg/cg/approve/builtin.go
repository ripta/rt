package approve

// builtinDenyRules returns the conservative default-deny set that always leads
// the merged deny list. It covers shells and inline-code interpreter forms,
// whose program token alone (sh, bash) or program-plus-eval-flag (python -c)
// would otherwise wave through arbitrary code in an argument the rules do not
// introspect. Allowlisting an interpreter allowlists everything it can run.
//
// Rules are prefix kind, so the deny-side basename normalization catches the
// program however it is spelled (sh, /bin/sh, ./sh). The two-token interpreter
// rules additionally pin argv[1] to the eval flag.
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
	}

	return rules
}
