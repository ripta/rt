package approve

import (
	"fmt"
	"os"
	"path/filepath"
)

// LoadOptions parameterizes layer resolution so callers and tests can inject
// paths instead of touching the real home directory or process environment. A
// zero value resolves to the default global and project locations.
type LoadOptions struct {
	// GlobalPath is the user-global rules file. Empty resolves to
	// ~/.config/cg/approve.yaml.
	GlobalPath string
	// ProjectRoot is the directory whose .cg/approve.yaml is the project layer.
	// Empty resolves to CLAUDE_PROJECT_DIR, falling back to the current
	// directory.
	ProjectRoot string
}

// DefaultGlobalPath returns ~/.config/cg/approve.yaml. The path is built from
// the home directory explicitly rather than os.UserConfigDir so it stays
// ~/.config on macOS.
func DefaultGlobalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}

	return filepath.Join(home, ".config", "cg", "approve.yaml"), nil
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

// ProjectPath returns the project rules file for a project root.
func ProjectPath(root string) string {
	return filepath.Join(root, ".cg", "approve.yaml")
}

// Load reads the global and project layers, merges them into a frozen ruleset,
// and returns the store. A missing layer file is not an error; it is treated as
// an empty layer. A present file that fails to parse or validate fails the load.
func Load(opts LoadOptions) (*Store, error) {
	globalPath := opts.GlobalPath
	if globalPath == "" {
		p, err := DefaultGlobalPath()
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

	global, err := loadLayer(globalPath)
	if err != nil {
		return nil, err
	}

	project, err := loadLayer(ProjectPath(projectRoot))
	if err != nil {
		return nil, err
	}

	rules, err := buildRuleset(global, project)
	if err != nil {
		return nil, err
	}

	return &Store{Global: global, Project: project, rules: rules}, nil
}

// loadLayer reads and parses one layer file. A missing file yields a layer with
// Present=false; any other read or parse error is returned with the path.
func loadLayer(path string) (Layer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Layer{Path: path, Present: false}, nil
		}
		return Layer{}, fmt.Errorf("reading %s: %w", path, err)
	}

	node, doc, err := ParseDocument(raw)
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
// evaluates. The project mode overrides the global mode; allow and deny entries
// from both layers are unioned. The built-in default-deny set leads the deny
// list, so it cannot be re-allowed, and deny is evaluated before allow, which
// gives deny precedence across layers.
func buildRuleset(global, project Layer) (*Ruleset, error) {
	rs := &Ruleset{Mode: ModeEnforce}

	if global.Doc != nil && global.Doc.Mode != "" {
		rs.Mode = global.Doc.Mode
	}
	if project.Doc != nil && project.Doc.Mode != "" {
		rs.Mode = project.Doc.Mode
	}

	rs.Deny = append(rs.Deny, builtinDenyRules()...)
	if global.Doc != nil {
		rs.Deny = append(rs.Deny, global.Doc.Deny...)
		rs.Allow = append(rs.Allow, global.Doc.Allow...)
	}
	if project.Doc != nil {
		rs.Deny = append(rs.Deny, project.Doc.Deny...)
		rs.Allow = append(rs.Allow, project.Doc.Allow...)
	}

	return rs, nil
}
