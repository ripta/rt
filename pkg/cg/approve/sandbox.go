package approve

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ripta/rt/pkg/cg/model"
)

// AlreadySandboxed says what a sandboxed run does when cg itself runs inside a sandbox, which
// Seatbelt cannot nest.
type AlreadySandboxed string

const (
	// AlreadyRun runs the command under the outer sandbox only, without the profile.
	AlreadyRun AlreadySandboxed = "run"

	// AlreadyRefuse refuses the run.
	AlreadyRefuse AlreadySandboxed = "refuse"

	// AlreadyAsk prompts whether to run without the profile.
	AlreadyAsk AlreadySandboxed = "ask"
)

// SandboxDef is one named sandbox as a rules file writes it: one block per backend.
type SandboxDef struct {
	Seatbelt *SeatbeltDef `yaml:"seatbelt,omitempty"`
}

// SeatbeltDef is the Seatbelt block. Profile and AlreadySandboxed are required. Proxy is the URL a
// profile that allows only loopback traffic needs its commands to use. Params are extra string
// parameters passed to the profile with -D.
type SeatbeltDef struct {
	Profile          string            `yaml:"profile"`
	AlreadySandboxed AlreadySandboxed  `yaml:"already_sandboxed"`
	Proxy            string            `yaml:"proxy,omitempty"`
	Params           map[string]string `yaml:"params,omitempty"`
}

// Sandbox is a merged sandbox definition, frozen at load.
//
// File is the rules file whose definition won the merge. Declared lists the backends the definition
// has a block for. Seatbelt is set only when this platform has the backend and the definition has a
// block for it. It is nil otherwise, and a run under such a sandbox is refused.
type Sandbox struct {
	Name     string
	File     string
	Declared []string
	Seatbelt *Seatbelt
}

// Seatbelt is a Seatbelt block with everything a run needs resolved at load.
//
// Profile is the expanded path. Text is the file's bytes at load, which every run applies instead of
// the file on disk. Defines holds the -D parameters, each KEY=VALUE, with HOME and PROJECT_PATH first
// and symlinks resolved. Class and ProbeStderr are the load probe's classification.
type Seatbelt struct {
	Profile          string
	Text             string
	Defines          []string
	AlreadySandboxed AlreadySandboxed
	Proxy            string
	Class            model.SandboxClass
	ProbeStderr      string
}

// Spec builds the supervisor-side description of running command under the sandbox. resolved is the
// command's own resolution. It returns false when this platform cannot apply the sandbox.
func (sb *Sandbox) Spec(command []string, resolved *model.Resolution) (*model.SandboxSpec, bool) {
	if sb == nil || sb.Seatbelt == nil || resolved == nil {
		return nil, false
	}

	return &model.SandboxSpec{
		Name:      sb.Name,
		Command:   command,
		Resolved:  resolved.Resolved,
		Canonical: resolved.Canonical,
		Profile:   sb.Seatbelt.Text,
		Defines:   slices.Clone(sb.Seatbelt.Defines),
	}, true
}

// Sandbox returns the frozen definition of a sandbox by name.
func (rs *Ruleset) Sandbox(name string) (*Sandbox, bool) {
	sb, ok := rs.Sandboxes[name]

	return sb, ok
}

// SandboxNames lists the defined sandbox names in order.
func (rs *Ruleset) SandboxNames() []string {
	names := make([]string, 0, len(rs.Sandboxes))
	for name := range rs.Sandboxes {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// reservedParams are the parameters cg sets itself.
var reservedParams = map[string]bool{"HOME": true, "PROJECT_PATH": true}

const (
	// sandboxArgMax is the macOS limit on the bytes of arguments and environment together. The frozen
	// profile text travels in argv, so it counts against it.
	sandboxArgMax = 1 << 20

	// sandboxArgMargin is what cg leaves for the environment and the command's own arguments. It is a
	// fixed reserve, since neither is known at load.
	sandboxArgMargin = 256 << 10
)

// sandboxBackend is the Seatbelt backend selection and its probe, injected so a test can run on any
// platform and outside a sandbox. A zero backend resolves to the running OS and the real probe.
type sandboxBackend struct {
	platform string
	probe    func(profile string, defines []string) model.SandboxProbe
}

func newSandboxBackend(opts LoadOptions) sandboxBackend {
	b := sandboxBackend{platform: opts.Platform, probe: opts.Probe}
	if b.platform == "" {
		b.platform = runtime.GOOS
	}
	if b.probe == nil {
		b.probe = model.ProbeSeatbelt
	}

	return b
}

// hasSeatbelt reports whether this platform has the Seatbelt backend.
func (b sandboxBackend) hasSeatbelt() bool { return b.platform == "darwin" }

// validateSandboxes validates the sandboxes map of a version 2 document, collecting every problem.
// Names and params keys are data. Nothing here reads a profile.
func validateSandboxes(root *yaml.Node, doc *Document) []error {
	node := findMapValue(root, "sandboxes")
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	var errs []error

	for i := 0; i+1 < len(node.Content); i += 2 {
		name, def := node.Content[i], node.Content[i+1]
		if name.Value == "" {
			errs = append(errs, fmt.Errorf("line %d: a sandbox name must not be empty", name.Line))
		}

		if def.Kind != yaml.MappingNode {
			continue
		}

		block := findMapValue(def, "seatbelt")
		if block == nil || block.Kind != yaml.MappingNode {
			continue
		}

		errs = append(errs, validateSeatbelt(name.Value, block, doc.Sandboxes[name.Value].Seatbelt)...)
	}

	return errs
}

// validateSeatbelt checks one seatbelt block against its node, so an absent key can be told from an
// empty one and a params value from its YAML type.
func validateSeatbelt(name string, block *yaml.Node, def *SeatbeltDef) []error {
	var errs []error

	if v := findMapValue(block, "profile"); v == nil || v.Kind != yaml.ScalarNode || v.Value == "" {
		errs = append(errs, fmt.Errorf("line %d: sandbox %q: seatbelt needs a profile path", block.Line, name))
	}

	switch v := findMapValue(block, "already_sandboxed"); {
	case v == nil:
		errs = append(errs, fmt.Errorf("line %d: sandbox %q: seatbelt needs already_sandboxed, one of run, refuse, or ask", block.Line, name))
	case def == nil || !validAlreadySandboxed(def.AlreadySandboxed):
		errs = append(errs, fmt.Errorf("line %d: sandbox %q: already_sandboxed %q is not one of run, refuse, or ask", v.Line, name, v.Value))
	}

	if v := findMapValue(block, "proxy"); v != nil {
		if err := validateProxy(v.Value); err != nil {
			errs = append(errs, fmt.Errorf("line %d: sandbox %q: %w", v.Line, name, err))
		}
	}

	if params := findMapValue(block, "params"); params != nil {
		errs = append(errs, validateParams(name, params)...)
	}

	return errs
}

func validAlreadySandboxed(v AlreadySandboxed) bool {
	switch v {
	case AlreadyRun, AlreadyRefuse, AlreadyAsk:
		return true
	}

	return false
}

func validateProxy(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("proxy %q is not a URL with a scheme and host, such as http://localhost:3128", value)
	}

	return nil
}

// validateParams checks a params mapping. SBPL tests a parameter for being set, not for its value,
// so a YAML boolean, number, or null would silently set it. Requiring a string makes that an error.
func validateParams(name string, params *yaml.Node) []error {
	if params.Kind != yaml.MappingNode {
		return []error{fmt.Errorf("line %d: sandbox %q: params must be a mapping of strings", params.Line, name)}
	}

	var errs []error

	for i := 0; i+1 < len(params.Content); i += 2 {
		key, value := params.Content[i], params.Content[i+1]

		switch {
		case key.Value == "" || strings.Contains(key.Value, "="):
			errs = append(errs, fmt.Errorf("line %d: sandbox %q: params key %q must be a non-empty name without '='", key.Line, name, key.Value))
		case reservedParams[key.Value]:
			errs = append(errs, fmt.Errorf("line %d: sandbox %q: params key %q is set by cg and cannot be overridden", key.Line, name, key.Value))
		}

		if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" {
			errs = append(errs, fmt.Errorf("line %d: sandbox %q: params value for %q must be a YAML string. SBPL treats any set parameter as true, so quote the value, as in \"1\"", value.Line, name, key.Value))
		}
	}

	return errs
}

// checkGlobalProfiles rejects a relative profile path in the global file. A relative path there would
// point into whichever project is open, so a cloned repository could supply the profile.
func checkGlobalProfiles(doc *Document) []error {
	var errs []error

	for _, name := range sortedKeys(doc.Sandboxes) {
		block := doc.Sandboxes[name].Seatbelt
		if block == nil || block.Profile == "" {
			continue
		}

		if !isTilde(block.Profile) && !filepath.IsAbs(block.Profile) {
			errs = append(errs, fmt.Errorf("sandbox %q: relative profile path %q in the global rules file; use a path starting with ~/ or an absolute path", name, block.Profile))
		}
	}

	return errs
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}

// strictKeys reports every key a version 2 document does not know, with its line. A typo such as
// `sanbox: go` would otherwise fail open. Names under sandboxes and keys under params are data.
func strictKeys(root *yaml.Node) []error {
	if root == nil {
		return nil
	}

	var errs []error

	check := func(m *yaml.Node, where string, known func(string) bool) {
		for i := 0; m != nil && m.Kind == yaml.MappingNode && i+1 < len(m.Content); i += 2 {
			if key := m.Content[i]; !known(key.Value) {
				errs = append(errs, fmt.Errorf("line %d: unknown key %q in %s", key.Line, key.Value, where))
			}
		}
	}

	in := func(names ...string) func(string) bool {
		return func(k string) bool { return slices.Contains(names, k) }
	}

	check(root, "the rules file", in("version", "mode", "deny", "allow", "restrict", "sandboxes"))

	isRuleKey := func(k string) bool {
		if _, ok := ruleKindKeys[k]; ok {
			return true
		}

		return slices.Contains([]string{"as_basename", "message", "permit_unsafe_envs", "sandbox"}, k)
	}

	for _, section := range []string{"deny", "allow", "restrict"} {
		seq := findMapValue(root, section)
		for i := 0; seq != nil && seq.Kind == yaml.SequenceNode && i < len(seq.Content); i++ {
			check(seq.Content[i], "a "+section+" rule", isRuleKey)
		}
	}

	sandboxes := findMapValue(root, "sandboxes")
	for i := 0; sandboxes != nil && sandboxes.Kind == yaml.MappingNode && i+1 < len(sandboxes.Content); i += 2 {
		name, def := sandboxes.Content[i].Value, sandboxes.Content[i+1]

		check(def, fmt.Sprintf("sandbox %q", name), in("seatbelt"))
		check(findMapValue(def, "seatbelt"), fmt.Sprintf("the seatbelt block of sandbox %q", name),
			in("profile", "already_sandboxed", "proxy", "params"))
	}

	return errs
}

// sandboxSource is a sandbox definition and the file it came from.
type sandboxSource struct {
	def  SandboxDef
	file string
	// global is set when the definition came from the global file.
	global bool
}

// freezeSandboxes merges the two layers' definitions, project winning, and freezes the winners. It
// checks that every rule's sandbox is defined in one of the files. Only a winning definition is read
// or probed.
func freezeSandboxes(global, project Layer, projectRoot string, env *tildeEnv, backend sandboxBackend) (map[string]*Sandbox, error) {
	sources := sandboxSources(global, project)

	errs := undefinedSandboxes(sources, project, global)

	frozen := make(map[string]*Sandbox, len(sources))

	for _, name := range sortedKeys(sources) {
		sb, err := freezeSandbox(name, sources[name], projectRoot, env, backend)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		frozen[name] = sb
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return frozen, nil
}

// undefinedSandboxes reports each allow rule, in either layer, that names a sandbox no definition in
// sources provides.
func undefinedSandboxes(sources map[string]sandboxSource, layers ...Layer) []error {
	var errs []error

	for _, layer := range layers {
		if layer.Doc == nil {
			continue
		}

		for i := range layer.Doc.Allow {
			rule := &layer.Doc.Allow[i]
			if _, ok := sources[rule.Sandbox]; rule.Sandbox != "" && !ok {
				errs = append(errs, fmt.Errorf("%s: %s names sandbox %q, which neither the project nor the global rules file defines",
					layer.Path, describeRule(rule), rule.Sandbox))
			}
		}
	}

	return errs
}

// sandboxSources merges the layers' definitions, project winning.
func sandboxSources(global, project Layer) map[string]sandboxSource {
	sources := make(map[string]sandboxSource)

	if global.Doc != nil {
		for name, def := range global.Doc.Sandboxes {
			sources[name] = sandboxSource{def: def, file: global.Path, global: true}
		}
	}
	if project.Doc != nil {
		for name, def := range project.Doc.Sandboxes {
			sources[name] = sandboxSource{def: def, file: project.Path}
		}
	}

	return sources
}

// freezeSandbox reads, checks, and probes one sandbox's profile. The error names the sandbox and the
// file that defines it.
func freezeSandbox(name string, src sandboxSource, projectRoot string, env *tildeEnv, backend sandboxBackend) (*Sandbox, error) {
	sb := &Sandbox{Name: name, File: src.file}

	block := src.def.Seatbelt
	if block == nil {
		return sb, nil
	}

	sb.Declared = append(sb.Declared, "seatbelt")

	if !backend.hasSeatbelt() {
		return sb, nil
	}

	fail := func(format string, args ...any) (*Sandbox, error) {
		return nil, fmt.Errorf("%s: sandbox %q: %s", src.file, name, fmt.Sprintf(format, args...))
	}

	path, err := profilePath(env, projectRoot, block.Profile, src.global)
	if err != nil {
		return fail("%v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fail("reading profile: %v", err)
	}

	defines, err := seatbeltDefines(env, projectRoot, block.Params)
	if err != nil {
		return fail("%v", err)
	}

	if size := argBytes(string(raw), defines); size > sandboxArgMax-sandboxArgMargin {
		return fail("profile %s with its parameters needs %d bytes of argument space, over the %d bytes cg allows. "+
			"Move part of the profile into a file it imports", path, size, sandboxArgMax-sandboxArgMargin)
	}

	probe := backend.probe(string(raw), defines)
	if probe.CompileFailed {
		return fail("profile %s does not compile: %s", path, probe.Stderr)
	}

	sb.Seatbelt = &Seatbelt{
		Profile:          path,
		Text:             string(raw),
		Defines:          defines,
		AlreadySandboxed: block.AlreadySandboxed,
		Proxy:            block.Proxy,
		Class:            probe.Class,
		ProbeStderr:      probe.Stderr,
	}

	return sb, nil
}

// profilePath expands a profile path. A relative path resolves against the project root in the
// project file. The global file rejects it, which checkGlobalProfiles reports at parse time.
func profilePath(env *tildeEnv, projectRoot, value string, global bool) (string, error) {
	path, err := env.expand(value)
	if err != nil {
		return "", fmt.Errorf("profile path: %w", err)
	}

	switch {
	case filepath.IsAbs(path):
		return filepath.Clean(path), nil
	case global:
		return "", fmt.Errorf("relative profile path %q in the global rules file; use a path starting with ~/ or an absolute path", value)
	}

	return filepath.Join(projectRoot, path), nil
}

// seatbeltDefines builds the -D parameters. HOME and PROJECT_PATH come first, with symlinks
// resolved so a profile's path filters match the real paths. The block's own params follow, sorted.
func seatbeltDefines(env *tildeEnv, projectRoot string, params map[string]string) ([]string, error) {
	if env == nil || env.homeErr != nil {
		err := errors.New("no home directory is configured")
		if env != nil {
			err = env.homeErr
		}

		return nil, fmt.Errorf("the profile needs HOME: %w", err)
	}

	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving the project root: %w", err)
	}

	defines := []string{"HOME=" + realPath(env.home), "PROJECT_PATH=" + realPath(root)}
	for _, key := range sortedKeys(params) {
		defines = append(defines, key+"="+params[key])
	}

	return defines, nil
}

// realPath resolves symlinks, and keeps the path as given when it cannot be resolved.
func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}

	return path
}

// argBytes counts the bytes the profile text and its parameters take in argv.
func argBytes(text string, defines []string) int {
	n := len(text) + len("-p") + 2
	for _, define := range defines {
		n += len("-D") + len(define) + 2
	}

	return n
}
