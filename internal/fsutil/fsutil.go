// Package fsutil holds the filesystem primitives shared by install and lock code: atomic writes,
// containment checks and guarded removal.
package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrOutsideRoot reports a path that is not inside the root it must stay within.
var ErrOutsideRoot = errors.New("path is outside the allowed root")

// WriteFileAtomic writes data to path through a temporary file in the same directory, fsyncs it
// and renames it over path, so readers see either the old or the new content. On Windows a rename
// that fails with a sharing violation is retried a few times.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Chmod(perm); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return renameWithRetry(tmpName, path)
}

// renameWithRetry renames tmp over path. A Windows sharing violation is retried for a short while;
// other platforms rename once.
func renameWithRetry(tmp, path string) error {
	err := os.Rename(tmp, path)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	for attempt := 1; attempt <= 4; attempt++ {
		time.Sleep(time.Duration(15*attempt) * time.Millisecond)
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
	}
	return err
}

// Within reports whether path is root or lies below it. Both paths must be absolute. On Windows
// the comparison ignores case.
func Within(root, path string) bool {
	root, path = filepath.Clean(root), filepath.Clean(path)
	if runtime.GOOS == "windows" {
		root, path = strings.ToLower(root), strings.ToLower(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// RealPath resolves symlinks in the longest existing prefix of path and appends the rest, so it
// also works for paths that do not exist yet.
func RealPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var rest []string
	for p := abs; ; {
		resolved, err := filepath.EvalSymlinks(p)
		if err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}
		parent := filepath.Dir(p)
		if !errors.Is(err, fs.ErrNotExist) || parent == p {
			return "", err
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}

// EnsureDir creates path and any missing parents.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// RemoveAllWithin removes path recursively, but only when it lies strictly inside root. A symlink
// at path is removed itself, never followed.
func RemoveAllWithin(root, path string) error {
	if filepath.Clean(path) == filepath.Clean(root) || !Within(root, path) {
		return fmt.Errorf("refusing to remove %s: %w", path, ErrOutsideRoot)
	}
	return os.RemoveAll(path)
}
