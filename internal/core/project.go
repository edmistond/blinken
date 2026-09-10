package core

import (
	"os"
	"path/filepath"
)

// ProjectRoot walks upward from dir looking for a VCS boundary.
// Git worktrees have a .git *file*, so any entry named .git counts.
// Falls back to dir itself when no marker is found.
func ProjectRoot(dir string) (root string, marker string) {
	dir, _ = filepath.Abs(dir)
	cur := dir
	for {
		for _, m := range []string{".blinken.toml", ".jj", ".git"} {
			if _, err := os.Lstat(filepath.Join(cur, m)); err == nil {
				return cur, m
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir, ""
		}
		cur = parent
	}
}

// RelPath returns path relative to root, or "" if it is outside root.
func RelPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || len(rel) >= 2 && rel[:2] == ".." {
		return ""
	}
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
