//go:build !windows

package install

import (
	"os"
	"path/filepath"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
)

func platformLink(link, target string) error {
	return createRelativeLink(link, target)
}

// createRelativeLink links link to target with a path relative to the link's real parent, so the
// link still resolves after the project is moved.
func createRelativeLink(link, target string) error {
	realParent, err := fsutil.RealPath(filepath.Dir(link))
	if err != nil {
		return err
	}
	realTarget, err := fsutil.RealPath(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(realParent, realTarget)
	if err != nil {
		return err
	}
	return os.Symlink(rel, link)
}
