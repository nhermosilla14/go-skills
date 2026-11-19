// Package hash implements the skill content hash recorded as computedHash in the project lock.
// The algorithm is a published contract: see docs/spec and the vectors in hash_test.go.
package hash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const metadataFile = "metadata.json"

// excludedDirs are directory names skipped at any depth. Only directories are excluded by name.
var excludedDirs = map[string]bool{".git": true, "__pycache__": true, "__pypackages__": true}

// File is one file that counts towards the content hash.
type File struct {
	Rel  string // slash-separated path relative to the skill root
	Path string // real path to read the content from, with symlinks already resolved
}

// isExcluded reports whether an entry is left out of the content hash. Copies inherit the same
// rule through CollectFiles.
func isExcluded(name string, isDir bool) bool {
	if name == metadataFile {
		return true
	}
	return isDir && excludedDirs[name]
}

// CollectFiles lists the files under root that count towards the content hash, sorted by the
// unsigned bytes of their relative paths. Symlinks are followed when they stay inside root; links
// that escape, are broken, or form a cycle are skipped.
func CollectFiles(root string) ([]File, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	c := &collector{root: resolved}
	if err := c.walk(resolved, "", map[string]bool{resolved: true}); err != nil {
		return nil, err
	}
	sort.Slice(c.files, func(i, j int) bool { return c.files[i].Rel < c.files[j].Rel })
	return c.files, nil
}

// Dir returns the lowercase hex SHA-256 of the skill directory root.
func Dir(root string) (string, error) {
	files, err := CollectFiles(root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return "", err
		}
		data = normalizeLineEndings(data)
		h.Write([]byte(f.Rel))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(data))))
		h.Write([]byte{0})
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SingleFile returns the hash of a skill installed as one flat markdown file, framed as the
// "SKILL.md" entry of Dir so a flat install hashes like the directory form of the same content.
func SingleFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	data = normalizeLineEndings(data)
	h := sha256.New()
	h.Write([]byte("SKILL.md"))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(len(data))))
	h.Write([]byte{0})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// normalizeLineEndings turns CRLF into LF in text files only. A file containing a NUL byte is
// binary and is returned untouched.
func normalizeLineEndings(data []byte) []byte {
	if bytes.IndexByte(data, 0) >= 0 {
		return data
	}
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

type collector struct {
	root  string
	files []File
}

// walk visits dir, whose path relative to the root is prefix, and keeps the real paths of the
// directories on the current branch in ancestors to break symlink cycles.
func (c *collector) walk(dir, prefix string, ancestors map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		resolved, info, ok := c.resolve(filepath.Join(dir, name))
		if !ok || isExcluded(name, info.IsDir()) {
			continue
		}
		if !info.IsDir() {
			c.files = append(c.files, File{Rel: prefix + name, Path: resolved})
			continue
		}
		if ancestors[resolved] {
			continue
		}
		ancestors[resolved] = true
		err := c.walk(resolved, prefix+name+"/", ancestors)
		delete(ancestors, resolved)
		if err != nil {
			return err
		}
	}
	return nil
}

// resolve returns the real path and the target info of an entry. ok is false when the entry must
// be skipped: broken, outside the root, or not a regular file or directory.
func (c *collector) resolve(p string) (resolved string, info fs.FileInfo, ok bool) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", nil, false
	}
	if resolved != c.root && !strings.HasPrefix(resolved, c.root+string(filepath.Separator)) {
		return "", nil, false
	}
	info, err = os.Stat(resolved)
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return "", nil, false
	}
	return resolved, info, true
}
