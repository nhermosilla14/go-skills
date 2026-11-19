package install

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// renameInstalled writes the installed name into the SKILL.md of dir. Symlinks and non-files are
// left alone, and only a changed file is rewritten.
func renameInstalled(name, dir string) error {
	if name == "" {
		return nil
	}
	p := filepath.Join(dir, "SKILL.md")
	info, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	out := skill.SetFrontmatterName(raw, name)
	if bytes.Equal(out, raw) {
		return nil
	}
	return fsutil.WriteFileAtomic(p, out, info.Mode().Perm())
}
