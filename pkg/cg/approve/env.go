package approve

import "sort"

// dangerousEnvs is the built-in set of environment-variable names that can
// inject code into, or redirect the internals of, an otherwise-genuine allowed
// binary. The top-level program is already pinned to the server's PATH by
// exec.Command before any override is applied, so the residual vectors are
// dynamic-linker injection, subprocess PATH resolution, and interpreter
// injection. The set is a fixed constant; per-rule exemptions go through an
// allow rule's permit_unsafe_envs.
var dangerousEnvs = map[string]struct{}{
	"DYLD_INSERT_LIBRARIES": {},
	"DYLD_LIBRARY_PATH":     {},
	"DYLD_FRAMEWORK_PATH":   {},
	"LD_PRELOAD":            {},
	"LD_LIBRARY_PATH":       {},
	"LD_AUDIT":              {},
	"GCONV_PATH":            {},
	"PATH":                  {},
	"PYTHONPATH":            {},
	"NODE_OPTIONS":          {},
	"PERL5LIB":              {},
	"RUBYOPT":               {},
}

// DisallowedEnvs returns the dangerous environment-variable names present in env
// that this allow rule does not permit through permit_unsafe_envs, sorted. It
// returns nil when env carries no dangerous override or every dangerous key is
// permitted. Only allow rules carry permit_unsafe_envs; on any other rule the
// permit set is empty, so every dangerous key is reported.
func (r *Rule) DisallowedEnvs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}

	permitted := make(map[string]struct{}, len(r.PermitUnsafeEnvs))
	for _, name := range r.PermitUnsafeEnvs {
		permitted[name] = struct{}{}
	}

	var bad []string
	for name := range env {
		if _, dangerous := dangerousEnvs[name]; !dangerous {
			continue
		}
		if _, ok := permitted[name]; ok {
			continue
		}
		bad = append(bad, name)
	}
	sort.Strings(bad)

	return bad
}
