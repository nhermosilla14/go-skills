package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/hash"
)

// legacyExcludedDirs are the directories the version 1 hash skipped, whatever their depth.
var legacyExcludedDirs = map[string]bool{".git": true, "node_modules": true}

// legacyFile is one regular file of a version 1 hash input.
type legacyFile struct {
	rel  string
	path string
}

// legacyHash reproduces the version 1 project lock hash: regular files only, directories named
// .git or node_modules skipped, slash-separated paths ordered by UTF-16 code units, and the path
// bytes followed immediately by the content bytes, with no framing. Line endings are not
// normalised and metadata.json is not excluded.
func legacyHash(root string) (string, error) {
	var files []legacyFile
	if err := walkLegacy(root, "", &files); err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return lessUTF16(files[i].rel, files[j].rel) })
	h := sha256.New()
	for _, f := range files {
		fh, err := os.Open(f.path)
		if err != nil {
			return "", err
		}
		h.Write([]byte(f.rel))
		_, err = io.Copy(h, fh)
		closeErr := fh.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// walkLegacy collects dir's regular files under prefix. Symlinks and other special entries are
// left out, because the legacy walk only counted files and directories.
func walkLegacy(dir, prefix string, files *[]legacyFile) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if legacyExcludedDirs[name] {
				continue
			}
			if err := walkLegacy(filepath.Join(dir, name), prefix+name+"/", files); err != nil {
				return err
			}
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		*files = append(*files, legacyFile{rel: prefix + name, path: filepath.Join(dir, name)})
	}
	return nil
}

// migrateLegacy upgrades a version 1 document in memory. The stored hash is trusted only when the
// folder on disk still hashes to it; otherwise the entry gets an empty hash, which forces a
// re-fetch and makes a frozen install fail. Nothing is written here.
func (f *File) migrateLegacy(root string) {
	for name, entry := range f.Skills {
		entry.ComputedHash = migratedHash(root, name, entry)
		f.Skills[name] = entry
	}
}

// migratedHash returns the version 2 hash of an entry that matches its legacy hash, or "".
func migratedHash(root, name string, entry Entry) string {
	if entry.ComputedHash == "" {
		return ""
	}
	dir := legacyInstallDir(root, name, entry)
	if dir == "" {
		return ""
	}
	stored, err := legacyHash(dir)
	if err != nil || !strings.EqualFold(stored, entry.ComputedHash) {
		return ""
	}
	digest, err := hash.Dir(dir)
	if err != nil {
		return ""
	}
	return digest
}

// legacyInstallDir is the installed folder a version 1 entry describes: the canonical folder of a
// project, or the local source when the install was a no-op because source and destination are the
// same directory.
func legacyInstallDir(root, name string, entry Entry) string {
	candidates := []string{filepath.Join(root, ".agents", "skills", name)}
	if entry.SourceType == "local" && entry.Source != "" {
		source := filepath.FromSlash(entry.Source)
		if !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
		candidates = append(candidates, source)
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil && !info.IsDir() {
			return dir
		}
	}
	return ""
}
