package approve

import (
	"os"
	"path/filepath"
	"strings"
)

// checkout is one working tree of a git repository. Top is the directory holding its .git entry,
// and Common is the canonical path of the git directory every checkout of the repository shares.
type checkout struct {
	Top    string
	Common string
}

// findCheckout locates the checkout that contains dir by walking up to the nearest .git entry. The
// nearest entry decides, so a repository nested inside a worktree is its own checkout. It reports
// false when there is none, or when the entry is a .git file that fails linkedCheckout.
//
// It reads git's on-disk layout and never runs git, so a judgment does not depend on the server
// PATH or on a repository's config.
func findCheckout(dir string) (checkout, bool) {
	for {
		entry := filepath.Join(dir, ".git")

		info, err := os.Stat(entry)
		if err == nil {
			if info.IsDir() {
				common, err := filepath.EvalSymlinks(entry)
				if err != nil {
					return checkout{}, false
				}

				return checkout{Top: dir, Common: common}, true
			}

			return linkedCheckout(dir, entry)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return checkout{}, false
		}

		dir = parent
	}
}

// linkedCheckout reads the .git file of a linked worktree. The file alone proves nothing, because
// anyone can copy it, and a rule would then allow a file in a directory git knows nothing about.
//
// Git keeps an admin directory for each linked worktree at <common>/worktrees/<name>. Its commondir
// file names the shared git directory, and its gitdir file points back at the worktree's .git file.
// Both must hold: the admin directory sits in that repository's worktrees directory, and it points
// back at this checkout. See gitrepository-layout(5).
func linkedCheckout(top, gitFile string) (checkout, bool) {
	raw, err := os.ReadFile(gitFile)
	if err != nil {
		return checkout{}, false
	}

	target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
	if !ok {
		return checkout{}, false
	}

	admin, err := filepath.EvalSymlinks(resolveAgainst(top, strings.TrimSpace(target)))
	if err != nil {
		return checkout{}, false
	}

	commondir, err := os.ReadFile(filepath.Join(admin, "commondir"))
	if err != nil {
		return checkout{}, false
	}

	common, err := filepath.EvalSymlinks(resolveAgainst(admin, strings.TrimSpace(string(commondir))))
	if err != nil {
		return checkout{}, false
	}

	if filepath.Dir(admin) != filepath.Join(common, "worktrees") {
		return checkout{}, false
	}

	backlink, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil {
		return checkout{}, false
	}

	linked, err := filepath.EvalSymlinks(resolveAgainst(admin, strings.TrimSpace(string(backlink))))
	if err != nil {
		return checkout{}, false
	}

	self, err := filepath.EvalSymlinks(gitFile)
	if err != nil || linked != self {
		return checkout{}, false
	}

	return checkout{Top: top, Common: common}, true
}

func resolveAgainst(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(base, path)
}

// newCheckoutAlias returns the function that maps an executable in another checkout of the
// project's repository to the same path under the project's own checkout. It returns empty for any
// other path, including one already in the project's checkout.
//
// A project-relative rule names a path in the project, and every checkout of the project holds that
// path. So a rule, allow or deny alike, means the same file in each one. A rule that named only the
// project root's copy would prompt in a worktree, and a deny would silently stop applying there.
//
// Each call reads the disk, so a worktree created after the rules loaded is seen.
func newCheckoutAlias(projectRoot string) func(path string) string {
	return func(path string) string {
		if !filepath.IsAbs(path) {
			return ""
		}

		project, ok := findCheckout(projectRoot)
		if !ok {
			return ""
		}

		other, ok := findCheckout(filepath.Dir(path))
		if !ok || other.Common != project.Common || other.Top == project.Top {
			return ""
		}

		rel, err := filepath.Rel(other.Top, path)
		if err != nil {
			return ""
		}

		return filepath.Join(project.Top, rel)
	}
}
