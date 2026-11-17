package approve

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultProjectFile is the project rules file path, relative to the project
// root, that takes priority when no candidate is configured. It is also the
// write target when no candidate exists on disk.
const DefaultProjectFile = ".cg.yaml"

// DefaultProjectFiles returns the project rules file paths searched, in priority
// order, when LoadOptions.ProjectFiles is empty. The legacy .cg/approve.yaml is
// kept as a fallback so existing checkouts keep working.
func DefaultProjectFiles() []string {
	return []string{
		DefaultProjectFile,
		".cg/approve.yaml",
		".claude/cg.yaml",
	}
}

// LoadOptions parameterizes layer resolution so callers and tests can inject
// paths instead of touching the real home directory or process environment. A
// zero value resolves to the default global and project locations.
type LoadOptions struct {
	// GlobalPath is the user-global rules file. Empty resolves to
	// ~/.config/cg/approve.yaml.
	GlobalPath string
	// ProjectRoot is the directory the project rules file is resolved against.
	// Empty resolves to CLAUDE_PROJECT_DIR, falling back to the current
	// directory.
	ProjectRoot string
	// ProjectFiles lists candidate project rules file paths, relative to
	// ProjectRoot, in preference order. The first path that exists is loaded as
	// the project layer. An empty list resolves to DefaultProjectFiles.
	ProjectFiles []string
	// Home reads the home directory a leading ~ expands to. It is called once per load. Nil
	// resolves to os.UserHomeDir, the lookup that locates the global rules file.
	Home func() (string, error)
	// LookupUser reads a named user's home directory for ~user. It is consulted even when the
	// user is the one running cg. Nil resolves to the system user database.
	LookupUser func(name string) (string, error)
}

// DefaultGlobalPath returns ~/.config/cg/approve.yaml. The path is built from
// the home directory explicitly rather than os.UserConfigDir so it stays
// ~/.config on macOS.
func DefaultGlobalPath() (string, error) {
	env := newTildeEnv(nil, nil)

	return env.globalPath()
}

// globalPath returns the default global rules file under the home directory the load read.
func (e *tildeEnv) globalPath() (string, error) {
	if e.homeErr != nil {
		return "", e.homeErr
	}

	return filepath.Join(e.home, ".config", "cg", "approve.yaml"), nil
}

// DefaultProjectRoot returns CLAUDE_PROJECT_DIR when set and non-empty,
// otherwise the current working directory.
func DefaultProjectRoot() (string, error) {
	if dir := os.Getenv("CLAUDE_PROJECT_DIR"); dir != "" {
		return dir, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving working directory: %w", err)
	}

	return cwd, nil
}

// ProjectPath returns the project rules file for a project root. rel is the
// file path relative to root; an empty rel resolves to DefaultProjectFile. An absolute rel names
// that exact file, since joining it onto root would nest it.
func ProjectPath(root, rel string) string {
	if rel == "" {
		rel = DefaultProjectFile
	}
	if filepath.IsAbs(rel) {
		return filepath.Clean(rel)
	}

	return filepath.Join(root, rel)
}

// resolveProjectPath picks the effective project rules file from rels, each
// relative to root, in preference order: the first path that exists wins. A
// path is skipped only when it is genuinely absent, so an unreadable file is
// still chosen and surfaces its error at load. When none exist, the first listed
// path is the write target so a remembered rule lands at the operator's
// preferred location. An empty list resolves to DefaultProjectFiles.
//
// A value that starts with ~ is expanded, and one that is absolute after expansion names that
// exact file. Every value is expanded first, so an unresolvable one fails even when an earlier
// candidate exists.
func resolveProjectPath(root string, rels []string, env *tildeEnv) (string, error) {
	if len(rels) == 0 {
		rels = DefaultProjectFiles()
	}

	paths := make([]string, len(rels))
	for i, rel := range rels {
		p, err := env.projectFile(root, rel)
		if err != nil {
			return "", err
		}
		paths[i] = p
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil || !os.IsNotExist(err) {
			return p, nil
		}
	}

	return paths[0], nil
}

// Load reads the global and project layers, merges them into a frozen ruleset,
// and returns the store. A missing layer file is not an error; it is treated as
// an empty layer. A present file that fails to parse or validate fails the load.
func Load(opts LoadOptions) (*Store, error) {
	env := newTildeEnv(opts.Home, opts.LookupUser)

	globalPath := opts.GlobalPath
	if globalPath == "" {
		p, err := env.globalPath()
		if err != nil {
			return nil, err
		}
		globalPath = p
	}

	projectRoot := opts.ProjectRoot
	if projectRoot == "" {
		root, err := DefaultProjectRoot()
		if err != nil {
			return nil, err
		}
		projectRoot = root
	}

	projectPath, err := resolveProjectPath(projectRoot, opts.ProjectFiles, env)
	if err != nil {
		return nil, err
	}

	global, err := loadLayer(globalPath, env)
	if err != nil {
		return nil, err
	}

	project, err := loadLayer(projectPath, env)
	if err != nil {
		return nil, err
	}

	rules, err := buildRuleset(global, project, projectRoot, env)
	if err != nil {
		return nil, err
	}

	s := &Store{Global: global, Project: project, projectRoot: projectRoot, tilde: env}
	s.rules.Store(rules)

	return s, nil
}

// LayerDiagnosis is one layer's lint result: whether the file is present, and
// its parse/validation error, nil when the layer is clean or absent. Warnings holds
// the findings about single rules in the layer, and is empty when the layer failed
// to parse.
type LayerDiagnosis struct {
	Path     string
	Present  bool
	Err      error
	Warnings []Warning

	layer Layer
	root  string
	tilde *tildeEnv
}

// Diagnose validates the global and project layers independently for `cg
// lint`, so a broken layer does not prevent reporting problems in the other.
// Unlike Load, it never returns early on a layer error: both layers are
// always attempted. The outer error return is reserved for the operational
// failures Load also surfaces before it can even locate the layer files, such
// as a home directory or working directory lookup failure.
func Diagnose(opts LoadOptions) (global, project LayerDiagnosis, err error) {
	env := newTildeEnv(opts.Home, opts.LookupUser)

	globalPath := opts.GlobalPath
	if globalPath == "" {
		p, err := env.globalPath()
		if err != nil {
			return LayerDiagnosis{}, LayerDiagnosis{}, err
		}
		globalPath = p
	}

	projectRoot := opts.ProjectRoot
	if projectRoot == "" {
		root, err := DefaultProjectRoot()
		if err != nil {
			return LayerDiagnosis{}, LayerDiagnosis{}, err
		}
		projectRoot = root
	}

	projectPath, err := resolveProjectPath(projectRoot, opts.ProjectFiles, env)
	if err != nil {
		return LayerDiagnosis{}, LayerDiagnosis{}, err
	}

	globalLayer, globalErr := loadLayer(globalPath, env)
	projectLayer, projectErr := loadLayer(projectPath, env)

	// loadLayer returns a zero-value Layer on a read or parse error, path and
	// Present included, since Load only needs the error and never reads those
	// fields in that case. Diagnose needs both regardless: the path resolved
	// above stands in for the possibly-empty one on the returned Layer, and a
	// non-nil error implies the file was found, since the only path that
	// reports it absent, os.IsNotExist, returns a nil error.
	return LayerDiagnosis{
			Path:     globalPath,
			Present:  globalLayer.Present || globalErr != nil,
			Err:      globalErr,
			Warnings: documentWarnings(globalLayer.Doc),
			layer:    globalLayer,
			root:     projectRoot,
		},
		LayerDiagnosis{
			Path:     projectPath,
			Present:  projectLayer.Present || projectErr != nil,
			Err:      projectErr,
			Warnings: documentWarnings(projectLayer.Doc),
			layer:    projectLayer,
			root:     projectRoot,
			tilde:    env,
		},
		nil
}

// MergedWarnings reports the findings that need both layers at once, such as a prefix
// allow that covers a wrapper rule in the other layer. It reports ok=false without
// checking when either layer failed to parse, since the merged ruleset does not exist
// then. Findings about single rules belong to each layer's own Warnings.
func MergedWarnings(global, project LayerDiagnosis) (warnings []Warning, ok bool) {
	if global.Err != nil || project.Err != nil {
		return nil, false
	}

	rs, err := buildRuleset(global.layer, project.layer, project.root, project.tilde)
	if err != nil {
		return nil, false
	}

	return rs.coverageWarnings(), true
}

// loadLayer reads and parses one layer file. A missing file yields a layer with
// Present=false; any other read or parse error is returned with the path.
func loadLayer(path string, env *tildeEnv) (Layer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Layer{Path: path, Present: false}, nil
		}
		return Layer{}, fmt.Errorf("reading %s: %w", path, err)
	}

	node, doc, err := parseDocument(raw, env)
	if err != nil {
		return Layer{}, fmt.Errorf("%s: %w", path, err)
	}

	return Layer{
		Path:     path,
		Node:     node,
		Doc:      doc,
		Snapshot: raw,
		Present:  true,
	}, nil
}

// buildRuleset merges the two layers into the frozen ruleset the matcher
// evaluates. The project mode overrides the global mode; deny, allow, and
// restrict entries from both layers are unioned. Deny is evaluated before allow,
// which gives deny precedence across layers, so the built-in default-deny set
// cannot be re-allowed.
//
// Each tier lists project rules, then global rules, then the built-in deny set.
// Each rule records the layer it came from, which is the last step of the specificity
// ranking. The list order never changes a decision or the rule a verdict names.
// Restrict stays a tier below allow, so any allow from either layer carves out of any
// restrict from either layer.
func buildRuleset(global, project Layer, projectRoot string, env *tildeEnv) (*Ruleset, error) {
	rs := &Ruleset{Mode: ModeEnforce, checkoutAlias: newCheckoutAlias(projectRoot), projectRoot: projectRoot, tilde: env}

	if global.Doc != nil && global.Doc.Mode != "" {
		rs.Mode = global.Doc.Mode
	}
	if project.Doc != nil && project.Doc.Mode != "" {
		rs.Mode = project.Doc.Mode
	}

	if project.Doc != nil {
		rs.Deny = appendLayer(rs.Deny, project.Doc.Deny, layerProject)
		rs.Allow = appendLayer(rs.Allow, project.Doc.Allow, layerProject)
		rs.Restrict = appendLayer(rs.Restrict, project.Doc.Restrict, layerProject)
	}
	if global.Doc != nil {
		rs.Deny = appendLayer(rs.Deny, global.Doc.Deny, layerGlobal)
		rs.Allow = appendLayer(rs.Allow, global.Doc.Allow, layerGlobal)
		rs.Restrict = appendLayer(rs.Restrict, global.Doc.Restrict, layerGlobal)
	}
	rs.Deny = appendLayer(rs.Deny, builtinDenyRules(), layerBuiltin)

	for i := range rs.Deny {
		compileMatch(&rs.Deny[i], projectRoot)
	}
	for i := range rs.Allow {
		compileMatch(&rs.Allow[i], projectRoot)
	}
	for i := range rs.Restrict {
		compileMatch(&rs.Restrict[i], projectRoot)
	}

	return rs, nil
}

// appendLayer appends src to dst and marks each appended rule with the layer it came from.
func appendLayer(dst, src []Rule, layer ruleLayer) []Rule {
	start := len(dst)
	dst = append(dst, src...)

	for i := start; i < len(dst); i++ {
		dst[i].layer = layer
	}

	return dst
}
