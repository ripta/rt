package approve

import "path/filepath"

// contractBreakingPrograms are token prefixes for programs that do not re-execute
// their arguments as an argv. They run the arguments through a shell, run them in
// another filesystem, read a package spec instead of an argv[0], or load
// environment variables from project files the gate never sees. A wrapper rule
// that matches a subject headed by one of these never peels in the allow tier,
// since judging the tail as a command would judge something that does not run.
//
// The list is a backstop. Entries are token prefixes, so combined short flags and
// flags in the middle, such as `sudo -Es` or `sudo -u bob -s`, evade it.
var contractBreakingPrograms = [][]string{
	{"watch"},
	{"ssh"},
	{"su"},
	{"script"},
	{"sudo", "-s"},
	{"sudo", "-i"},
	{"flock", "-c"},
	{"tmux", "new-session"},
	{"nix-shell", "--run"},
	{"parallel"},
	{"entr", "-s"},
	{"xterm", "-e"},
	{"docker", "exec"},
	{"docker", "run"},
	{"podman", "run"},
	{"kubectl", "exec"},
	{"chroot"},
	{"nsenter"},
	{"npx"},
	{"npm", "exec"},
	{"direnv", "exec"},
	{"mise", "exec"},
	{"pipenv", "run"},
	{"nix", "develop"},
	{"fish"},
	{"dash"},
	{"ksh"},
	{"csh"},
	{"tcsh"},
}

// breaksWrapperContract reports whether a layer is headed by a listed program.
// Each of the canonical, resolved, and basename forms is checked with its first
// token reduced to a basename, so a symlink alias of a listed program is caught.
func breaksWrapperContract(forms ...matchForm) bool {
	return listedEntry(forms...) != nil
}

// listedEntry returns the first listed entry that heads one of the forms, or nil
// when none does.
func listedEntry(forms ...matchForm) []string {
	for _, form := range forms {
		if !form.ok {
			continue
		}

		for _, entry := range contractBreakingPrograms {
			if matchesListed(entry, form) {
				return entry
			}
		}
	}

	return nil
}

// matchesListed reports whether the form starts with entry, comparing the first
// token by basename and the rest byte-exact.
func matchesListed(entry []string, form matchForm) bool {
	if form.length() < len(entry) || filepath.Base(form.head) != entry[0] {
		return false
	}

	for i := 1; i < len(entry); i++ {
		if form.at(i) != entry[i] {
			return false
		}
	}

	return true
}

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
