package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
	"github.com/nhermosilla14/go-skills/internal/hash"
)

// categoryDirs are agent container directories; a skill is never installed over them.
var categoryDirs = map[string]bool{".curated": true, ".experimental": true, ".system": true}

// IsCategoryDir reports whether base is one of the agent category directories, which are never
// install targets and are never removed.
func IsCategoryDir(base string) bool { return categoryDirs[base] }

var (
	// ErrCategoryDir reports an install destination that is a category directory.
	ErrCategoryDir = errors.New("destination is a category directory")
	// ErrOverlap reports a source and destination that are the same place or contain one another.
	ErrOverlap = errors.New("source and destination overlap")
)

// replaceDir copies the skill files of src to dst, replacing whatever is there. The copy is built
// in a sibling temporary directory and renamed into place, so dst is never half written. dst must
// lie inside root.
func replaceDir(root, src, dst string) error {
	if categoryDirs[filepath.Base(dst)] {
		return fmt.Errorf("%s: %w", dst, ErrCategoryDir)
	}
	overlap, err := overlaps(src, dst)
	if err != nil {
		return err
	}
	if overlap {
		return fmt.Errorf("%s: %w", dst, ErrOverlap)
	}
	parent := filepath.Dir(dst)
	if err := fsutil.EnsureDir(parent); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dst)+".tmp-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := copyFiles(src, tmp); err != nil {
		return err
	}
	if err := fsutil.RemoveAllWithin(root, dst); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// overlaps reports whether a and b are the same real location or one contains the other.
func overlaps(a, b string) (bool, error) {
	ra, err := fsutil.RealPath(a)
	if err != nil {
		return false, err
	}
	rb, err := fsutil.RealPath(b)
	if err != nil {
		return false, err
	}
	return fsutil.Within(ra, rb) || fsutil.Within(rb, ra), nil
}

// copyFiles copies every file that counts towards the content hash from src into dst. Symlinks
// are dereferenced, so the copy is independent of the source tree.
func copyFiles(src, dst string) error {
	files, err := hash.CollectFiles(src)
	if err != nil {
		return err
	}
	for _, f := range files {
		target := filepath.Join(dst, filepath.FromSlash(f.Rel))
		if err := fsutil.EnsureDir(filepath.Dir(target)); err != nil {
			return err
		}
		if err := copyFile(f.Path, target); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
