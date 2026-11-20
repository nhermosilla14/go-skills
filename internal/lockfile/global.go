package lockfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
)

// GlobalFileName is the global lock file name, placed in the state directory or ~/.agents.
const GlobalFileName = ".skill-lock.json"

// GlobalVersion is the global lock version written. Older versions are read as empty.
const GlobalVersion = 3

// GlobalEntry describes one installed global skill. Unknown fields are kept in extra.
type GlobalEntry struct {
	Source          string
	SourceType      string
	SourceURL       string
	Ref             string
	Commit          string
	SkillPath       string
	SkillName       string
	SkillFolderHash string
	InstalledAt     string
	UpdatedAt       string
	PluginName      string
	SourceBaseURL   string
	WellKnownDigest string
	InstallMode     string

	extra map[string]json.RawMessage
}

// GlobalFile is a global lock document. Unknown top-level fields, dismissed prompts and the last
// agent selection are preserved.
type GlobalFile struct {
	Skills             map[string]GlobalEntry
	Dismissed          map[string]json.RawMessage
	LastSelectedAgents []string

	extra map[string]json.RawMessage
}

// NewGlobal returns an empty global lock document.
func NewGlobal() *GlobalFile {
	return &GlobalFile{Skills: map[string]GlobalEntry{}}
}

// LoadGlobal reads the global lock at path. A missing file yields an empty document; a version
// below 3 is read as empty, as upstream wipes it, but the file is left alone until the next write.
// A malformed file is an error and is never rewritten.
func LoadGlobal(path string) (*GlobalFile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewGlobal(), nil
	}
	if err != nil {
		return nil, err
	}
	f, err := decodeGlobal(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func decodeGlobal(data []byte) (*GlobalFile, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	var version int
	if err := json.Unmarshal(top["version"], &version); err != nil {
		return nil, fmt.Errorf("invalid version %s", top["version"])
	}
	if version < GlobalVersion {
		return NewGlobal(), nil
	}
	f := NewGlobal()
	if err := json.Unmarshal(top["skills"], &f.Skills); err != nil {
		return nil, fmt.Errorf("invalid skills map: %w", err)
	}
	if f.Skills == nil {
		return nil, errors.New("missing skills map")
	}
	if raw, ok := top["dismissed"]; ok {
		if err := json.Unmarshal(raw, &f.Dismissed); err != nil {
			return nil, fmt.Errorf("invalid dismissed map: %w", err)
		}
		if f.Dismissed == nil {
			return nil, errors.New("invalid dismissed map: expected an object")
		}
	}
	if raw, ok := top["lastSelectedAgents"]; ok {
		if err := json.Unmarshal(raw, &f.LastSelectedAgents); err != nil {
			return nil, fmt.Errorf("invalid lastSelectedAgents: %w", err)
		}
		if f.LastSelectedAgents == nil {
			return nil, errors.New("invalid lastSelectedAgents: expected an array")
		}
	}
	delete(top, "version")
	delete(top, "skills")
	delete(top, "dismissed")
	delete(top, "lastSelectedAgents")
	extra, err := compactAll(top)
	if err != nil {
		return nil, err
	}
	f.extra = extra
	return f, nil
}

// Save writes the document atomically, sorted by UTF-16 key order, with two-space indentation and
// a trailing newline. Nothing is written when the document cannot be encoded.
func (f *GlobalFile) Save(path string) error {
	data, err := f.encode()
	if err != nil {
		return err
	}
	if err := fsutil.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

// Put stores the entry for an installed name. Unknown fields of the previous entry are kept.
func (f *GlobalFile) Put(name string, e GlobalEntry) {
	if prev, ok := f.Skills[name]; ok {
		e.extra = prev.extra
	}
	f.Skills[name] = e
}

// Get returns the stored entry for name.
func (f *GlobalFile) Get(name string) (GlobalEntry, bool) {
	e, ok := f.Skills[name]
	return e, ok
}

// Delete removes the entry for name.
func (f *GlobalFile) Delete(name string) {
	delete(f.Skills, name)
}

// SetLastSelectedAgents records the agent selection. A nil slice keeps the field absent.
func (f *GlobalFile) SetLastSelectedAgents(ids []string) {
	if ids == nil {
		ids = []string{}
	}
	f.LastSelectedAgents = ids
}

// DismissedPrompt reports whether the named one-time prompt is recorded as dismissed. Only a JSON
// true counts; any other stored value leaves the prompt enabled.
func (f *GlobalFile) DismissedPrompt(name string) bool {
	raw, ok := f.Dismissed[name]
	if !ok {
		return false
	}
	var dismissed bool
	return json.Unmarshal(raw, &dismissed) == nil && dismissed
}

// DismissPrompt records the dismissal of the named one-time prompt.
func (f *GlobalFile) DismissPrompt(name string) {
	if f.Dismissed == nil {
		f.Dismissed = map[string]json.RawMessage{}
	}
	f.Dismissed[name] = json.RawMessage("true")
}

// UnmarshalJSON reads an entry and keeps the fields it does not know.
func (e *GlobalEntry) UnmarshalJSON(data []byte) error {
	type plain GlobalEntry
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, k := range globalEntryKeys {
		delete(raw, k)
	}
	extra, err := compactAll(raw)
	if err != nil {
		return err
	}
	*e = GlobalEntry(p)
	e.extra = extra
	return nil
}

var globalEntryKeys = []string{
	"source", "sourceType", "sourceUrl", "ref", "commit", "skillPath", "skillName",
	"skillFolderHash", "installedAt", "updatedAt", "pluginName", "sourceBaseUrl",
	"wellKnownDigest", "installMode",
}

// encode renders the document with two-space indentation and a trailing newline.
func (f *GlobalFile) encode() ([]byte, error) {
	skills := make([]member, 0, len(f.Skills))
	for _, name := range sortedKeys(f.Skills) {
		skills = append(skills, member{name, object(f.Skills[name].members())})
	}
	top := []member{{"version", fmt.Sprint(GlobalVersion)}, {"skills", object(skills)}}
	if f.Dismissed != nil {
		top = append(top, member{"dismissed", object(rawMembers(f.Dismissed))})
	}
	if f.LastSelectedAgents != nil {
		top = append(top, member{"lastSelectedAgents", stringArray(f.LastSelectedAgents)})
	}
	for _, k := range sortedKeys(f.extra) {
		top = append(top, member{k, string(f.extra[k])})
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(object(top)), "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// members lists the entry fields in upstream's insertion order.
func (e GlobalEntry) members() []member {
	var ms []member
	add := func(key, value string, always bool) {
		if value != "" || always {
			ms = append(ms, member{key, quote(value)})
		}
	}
	add("source", e.Source, false)
	add("sourceType", e.SourceType, true)
	add("sourceUrl", e.SourceURL, false)
	add("ref", e.Ref, false)
	add("commit", e.Commit, false)
	add("skillPath", e.SkillPath, false)
	add("skillName", e.SkillName, false)
	add("skillFolderHash", e.SkillFolderHash, true)
	add("installedAt", e.InstalledAt, false)
	add("updatedAt", e.UpdatedAt, false)
	add("pluginName", e.PluginName, false)
	add("sourceBaseUrl", e.SourceBaseURL, false)
	add("wellKnownDigest", e.WellKnownDigest, false)
	add("installMode", e.InstallMode, false)
	for _, k := range sortedKeys(e.extra) {
		ms = append(ms, member{k, string(e.extra[k])})
	}
	return ms
}

// rawMembers turns a raw-value map into members in UTF-16 key order.
func rawMembers(raw map[string]json.RawMessage) []member {
	ms := make([]member, 0, len(raw))
	for _, k := range sortedKeys(raw) {
		ms = append(ms, member{k, string(raw[k])})
	}
	return ms
}

// stringArray encodes a JSON array of strings.
func stringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, quote(v))
	}
	return "[" + strings.Join(parts, ",") + "]"
}
