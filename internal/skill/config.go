package skill

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
)

// skillsShFile declares extra skill containers at the source root.
const skillsShFile = "skills.sh.json"

// skillsShDirs returns the valid containers declared by skills.sh.json, in declared order. Entries
// must start with "./", stay inside the source and point to a directory; anything else is ignored.
func skillsShDirs(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, skillsShFile))
	if err != nil {
		return nil
	}
	var doc struct {
		Paths []json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var dirs []string
	for _, raw := range doc.Paths {
		var entry string
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		rel, ok := cleanRelativePath(entry)
		if !ok || seen[rel] {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if !fsutil.Within(root, dir) || !isDir(dir) {
			continue
		}
		seen[rel] = true
		dirs = append(dirs, rel)
	}
	return dirs
}

// cleanRelativePath validates a "./"-prefixed path and returns its normalized slash-separated form.
func cleanRelativePath(entry string) (string, bool) {
	if !strings.HasPrefix(entry, "./") || strings.Contains(entry, `\`) {
		return "", false
	}
	rel := strings.TrimSuffix(path.Clean(entry), "/")
	if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
