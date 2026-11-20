// Package lockfile reads and writes the project lock skills-lock.json (version 2).
package lockfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
)

// FileName is the project lock file name, placed at the project root.
const FileName = "skills-lock.json"

// Version is the project lock version written.
const Version = 2

// legacyVersion is the version 1 project lock, accepted and migrated in memory on read.
const legacyVersion = 1

// Entry describes one installed skill. Unknown fields are kept in extra and written back.
type Entry struct {
	Source          string
	SourceURL       string
	Ref             string
	Commit          string
	SourceType      string
	SkillPath       string
	ComputedHash    string
	SkillName       string
	Subagents       []string // Eve placement: "" is the root agent
	InstallMode     string
	SourceBaseURL   string
	WellKnownDigest string

	extra map[string]json.RawMessage
}

// File is a project lock document. Unknown top-level fields are kept in extra and written back.
type File struct {
	Skills map[string]Entry

	extra map[string]json.RawMessage
}

// New returns an empty lock document.
func New() *File {
	return &File{Skills: map[string]Entry{}}
}

// Load reads the lock at path. A missing file yields an empty document. A version 1 document is
// upgraded in memory and persisted by the next write; any other version or a malformed file is an
// error and is left alone.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	f, version, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if version == legacyVersion {
		f.migrateLegacy(filepath.Dir(path))
	}
	return f, nil
}

func decode(data []byte) (*File, int, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, 0, fmt.Errorf("invalid JSON: %w", err)
	}
	var version int
	if err := json.Unmarshal(top["version"], &version); err != nil ||
		(version != Version && version != legacyVersion) {
		return nil, 0, fmt.Errorf("unsupported lock version %s, expected %d", top["version"], Version)
	}
	f := New()
	if err := json.Unmarshal(top["skills"], &f.Skills); err != nil {
		return nil, 0, fmt.Errorf("invalid skills map: %w", err)
	}
	if f.Skills == nil {
		return nil, 0, errors.New("missing skills map")
	}
	delete(top, "version")
	delete(top, "skills")
	extra, err := compactAll(top)
	if err != nil {
		return nil, 0, err
	}
	f.extra = extra
	return f, version, nil
}

// Save writes the document to path atomically, with sorted keys and two-space indentation.
func (f *File) Save(path string) error {
	data, err := f.encode()
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

// Put stores the entry for an installed name. Unknown fields of the previous entry are kept.
func (f *File) Put(name string, e Entry) {
	if prev, ok := f.Skills[name]; ok {
		e.extra = prev.extra
	}
	f.Skills[name] = e
}

// Get returns the stored entry for name.
func (f *File) Get(name string) (Entry, bool) {
	e, ok := f.Skills[name]
	return e, ok
}

// Delete removes the entry for name. The remaining entries are left untouched.
func (f *File) Delete(name string) {
	delete(f.Skills, name)
}

// UnmarshalJSON reads an entry and keeps the fields it does not know.
func (e *Entry) UnmarshalJSON(data []byte) error {
	type plain Entry // same fields, no methods, so the decoder does not recurse
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, k := range entryKeys {
		delete(raw, k)
	}
	extra, err := compactAll(raw)
	if err != nil {
		return err
	}
	*e = Entry(p)
	e.extra = extra
	return nil
}

var entryKeys = []string{
	"source", "sourceUrl", "ref", "commit", "sourceType", "skillPath", "computedHash",
	"skillName", "subagents", "installMode", "installedAt", "updatedAt", "sourceBaseUrl",
	"wellKnownDigest",
}

// compactAll compacts the raw values so that rewriting them is stable, and returns nil for none.
func compactAll(raw map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]json.RawMessage, len(raw))
	for k, v := range raw {
		var buf bytes.Buffer
		if err := json.Compact(&buf, v); err != nil {
			return nil, err
		}
		out[k] = buf.Bytes()
	}
	return out, nil
}
