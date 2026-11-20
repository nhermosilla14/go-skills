package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nhermosilla14/go-skills/internal/skill"
)

// wellKnownSchemaV2 is the only index schema version this build understands. Any other $schema
// value rejects the index.
const wellKnownSchemaV2 = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"

// wellKnownUpdateHeader marks a discovery request made by update or check.
const wellKnownUpdateHeader = "X-Skills-Update-Check"

// wellKnownAgentPath and wellKnownSkillsPath are the documented index locations, preferred first.
const (
	wellKnownAgentPath  = "/.well-known/agent-skills/index.json"
	wellKnownSkillsPath = "/.well-known/skills/index.json"
)

// wellKnownIndexMax bounds one index body, which is JSON and never large.
const wellKnownIndexMax int64 = 5 << 20

// Provider limits are variables so tests can lower them without building huge fixtures.
var (
	// wellKnownBudget is the single budget shared by every candidate probe of one discovery.
	wellKnownBudget = 10 * time.Second
	// wellKnownMaxUnpacked bounds the bytes an archive artifact may extract to.
	wellKnownMaxUnpacked int64 = 50 << 20
	// wellKnownMaxEntries bounds the entries an archive artifact may contain.
	wellKnownMaxEntries = 1000
)

// wellKnownDigestRE is the only digest spelling the v0.2.0 schema accepts.
var wellKnownDigestRE = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// ErrWellKnownScopeNotFound reports a scoped source whose scoped index lists no skill, or that only
// publishes a root index. The provider refuses to widen a scoped path to the host catalog.
var ErrWellKnownScopeNotFound = errors.New("well-known scope not found")

// WellKnown is a host's parsed skill index.
type WellKnown struct {
	// BaseURL is the base that served the index: the scoped directory, or the origin for a root
	// index. It is what the lock records and re-fetches.
	BaseURL string
	// IndexURL is the exact index document that was read, for resolving relative references.
	IndexURL string
	// Skills are the usable entries, in index order.
	Skills []WellKnownSkill
}

// WellKnownSkill is one usable index entry. Type and URL are v0.2.0 fields; Files is the legacy
// file list, which carries no integrity data.
type WellKnownSkill struct {
	Name        string
	Description string
	Type        string // "skill-md" or "archive"; empty for the legacy file list
	URL         string // artifact URL, v0.2.0 only
	Digest      string // "sha256:<hex>", v0.2.0 only
	Files       []string
}

// WellKnownContent is what fetching a provider materialized: one directory per selected skill.
type WellKnownContent struct {
	BaseURL string
	// Entries is keyed by the sanitized index name.
	Entries map[string]WellKnownContentSkill
}

// WellKnownContentSkill is one materialized provider skill.
type WellKnownContentSkill struct {
	Name        string
	Description string
	Dir         string // absolute directory holding SKILL.md
	Rel         string // slash-separated directory relative to the tree root
	SkillPath   string // Rel plus the SKILL.md name
	Digest      string // index digest, empty for the legacy schema
}

// Skills lists the materialized skills in name order, shaped for the rest of the use cases.
func (c *WellKnownContent) Skills() []skill.Skill {
	names := make([]string, 0, len(c.Entries))
	for name := range c.Entries {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]skill.Skill, 0, len(names))
	for _, name := range names {
		e := c.Entries[name]
		out = append(out, skill.Skill{
			Name: e.Name, Description: e.Description, Dir: e.Dir, RelDir: e.Rel,
		})
	}
	return out
}

// Entry returns the materialized skill whose sanitized index name matches name.
func (c *WellKnownContent) Entry(name string) (WellKnownContentSkill, bool) {
	if c == nil {
		return WellKnownContentSkill{}, false
	}
	e, ok := c.Entries[skill.SanitizeName(name)]
	return e, ok
}

// List shapes the index entries as skill metadata, before anything is downloaded. It is what add
// lists and from which the user selects.
func (w *WellKnown) List() []skill.Skill {
	out := make([]skill.Skill, 0, len(w.Skills))
	for _, s := range w.Skills {
		name := skill.SanitizeName(s.Name)
		out = append(out, skill.Skill{Name: name, Description: s.Description, RelDir: name})
	}
	return out
}

// wellKnownCandidate is one index URL to probe.
type wellKnownCandidate struct {
	url  string
	base string // what the lock records when this candidate wins
	root bool   // the origin-root index, never used for a scoped source
}

// wellKnownScopeError is the documented refusal to widen a scoped path to the host root index.
type wellKnownScopeError struct {
	path string
	root string
}

func (e wellKnownScopeError) Error() string {
	return fmt.Sprintf("No skills found for the scoped path '%s' on %s. Not falling back to the root "+
		"skills index because that would install every skill the host publishes. "+
		"Use a URL with the index, or add a skill file or archive directly.", e.path, e.root)
}

func (e wellKnownScopeError) Unwrap() error { return ErrWellKnownScopeNotFound }

// ProbeWellKnown discovers a host's skill index. The candidates are probed in the documented order
// under one 10-second budget, and the highest-priority usable index wins. A host without an index
// yields (nil, nil), so the caller can fall back to a direct download; a scoped source never widens
// to the root catalog.
func ProbeWellKnown(ctx context.Context, rawURL string, updateCheck bool) (*WellKnown, error) {
	candidates, scoped, scopePath, root, ok := wellKnownCandidates(rawURL)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, wellKnownBudget)
	defer cancel()
	var first *WellKnown
	rootFound := false
	for _, c := range candidates {
		data, status, err := fetchWellKnownIndex(ctx, c.url, updateCheck)
		if err != nil {
			if ctx.Err() != nil {
				break // the shared budget is spent; the remaining candidates are aborted
			}
			continue
		}
		if status < 200 || status > 299 {
			continue
		}
		idx, valid := parseWellKnownIndex(c.url, data)
		if !valid {
			continue
		}
		idx.BaseURL = c.base
		idx.IndexURL = c.url
		if scoped && c.root {
			rootFound = true
			continue
		}
		if first == nil {
			first = idx
		}
	}
	if scoped {
		if first != nil && len(first.Skills) == 0 {
			return nil, wellKnownScopeError{path: scopePath, root: root}
		}
		if first != nil {
			return first, nil
		}
		if rootFound {
			return nil, wellKnownScopeError{path: scopePath, root: root}
		}
		return nil, nil
	}
	if first != nil && len(first.Skills) > 0 {
		return first, nil
	}
	return nil, nil
}

// wellKnownCandidates builds the probe list of a base URL, highest priority first. A bare origin
// produces only the two root candidates.
func wellKnownCandidates(rawURL string) (candidates []wellKnownCandidate, scoped bool, scopePath, root string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Host == "" {
		return nil, false, "", "", false
	}
	base := url.URL{Scheme: u.Scheme, Host: u.Host, Path: strings.TrimRight(u.Path, "/")}
	origin := url.URL{Scheme: u.Scheme, Host: u.Host}
	scoped = base.Path != ""
	specs := []struct {
		base   url.URL
		suffix string
		root   bool
	}{
		{base, wellKnownAgentPath, false},
		{origin, wellKnownAgentPath, true},
		{base, wellKnownSkillsPath, false},
		{origin, wellKnownSkillsPath, true},
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		target := spec.base
		target.Path = strings.TrimRight(spec.base.Path, "/") + spec.suffix
		key := target.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, wellKnownCandidate{url: key, base: spec.base.String(), root: spec.root})
	}
	return candidates, scoped, base.Path, origin.String(), true
}

// fetchWellKnownIndex performs one probe. A non-2xx response is returned as (nil, status, nil) so it
// only rejects that candidate.
func fetchWellKnownIndex(ctx context.Context, rawURL string, updateCheck bool) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	if updateCheck {
		req.Header.Set(wellKnownUpdateHeader, "1")
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, wellKnownIndexMax+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(data)) > wellKnownIndexMax {
		return nil, resp.StatusCode, errors.New("well-known index is too large")
	}
	return data, resp.StatusCode, nil
}

// parseWellKnownIndex decodes one index document. It reports whether the document is an index of a
// recognized shape; malformed v0.2.0 entries are skipped individually while the legacy schema is
// all-or-nothing.
func parseWellKnownIndex(indexURL string, data []byte) (*WellKnown, bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil || top == nil {
		return nil, false
	}
	rawSkills, present := top["skills"]
	if !present || !isJSONArray(rawSkills) {
		return nil, false
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawSkills, &entries); err != nil {
		return nil, false
	}
	rawSchema, hasSchema := top["$schema"]
	if hasSchema {
		var schema string
		if err := json.Unmarshal(rawSchema, &schema); err != nil || schema != wellKnownSchemaV2 {
			return nil, false
		}
		return parseWellKnownV2(indexURL, entries)
	}
	return parseWellKnownLegacy(entries)
}

// isJSONArray reports whether raw is a JSON array, so a null or an object is rejected.
func isJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

// v2Entry is the v0.2.0 per-skill shape.
type v2Entry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Digest      string `json:"digest"`
}

// parseWellKnownV2 parses the digest-carrying schema. An entry that fails validation is skipped; an
// index whose entries all fail normalizes to no index, while a literally empty list is a valid empty
// index.
func parseWellKnownV2(indexURL string, entries []json.RawMessage) (*WellKnown, bool) {
	idx := &WellKnown{IndexURL: indexURL}
	seen := map[string]bool{}
	for _, raw := range entries {
		var e v2Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			continue
		}
		if !validWellKnownName(e.Name) || seen[e.Name] {
			continue
		}
		if e.Type != "skill-md" && e.Type != "archive" {
			continue
		}
		if e.Description == "" || len([]rune(e.Description)) > 1024 {
			continue
		}
		if !wellKnownDigestRE.MatchString(e.Digest) {
			continue
		}
		artifact, err := resolveWellKnownURL(indexURL, e.URL)
		if err != nil {
			continue
		}
		seen[e.Name] = true
		idx.Skills = append(idx.Skills, WellKnownSkill{
			Name: e.Name, Description: e.Description, Type: e.Type, URL: artifact, Digest: e.Digest,
		})
	}
	if len(entries) > 0 && len(idx.Skills) == 0 {
		return nil, false
	}
	return idx, true
}

// legacyEntry is the v0.1.0 per-skill shape.
type legacyEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

// parseWellKnownLegacy parses the file-list schema. One invalid entry rejects the whole index.
func parseWellKnownLegacy(entries []json.RawMessage) (*WellKnown, bool) {
	idx := &WellKnown{}
	seen := map[string]bool{}
	for _, raw := range entries {
		var e legacyEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, false
		}
		if !validWellKnownName(e.Name) || seen[e.Name] || !validLegacyFiles(e.Files) {
			return nil, false
		}
		seen[e.Name] = true
		idx.Skills = append(idx.Skills, WellKnownSkill{
			Name: e.Name, Description: e.Description, Files: append([]string(nil), e.Files...),
		})
	}
	return idx, true
}

// validWellKnownName is the documented skill name contract.
func validWellKnownName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// validLegacyFiles checks a v0.1.0 file list. The list must be non-empty, every path must be a safe
// relative path, and one entry must be the root SKILL.md, case-insensitively.
func validLegacyFiles(files []string) bool {
	if len(files) == 0 {
		return false
	}
	hasSkill := false
	for _, f := range files {
		if !validWellKnownFile(f) {
			return false
		}
		if strings.EqualFold(f, skillFile) {
			hasSkill = true
		}
	}
	return hasSkill
}

// validWellKnownFile reports whether a declared file path stays inside the skill folder.
func validWellKnownFile(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) || strings.Contains(p, `\`) {
		return false
	}
	if strings.HasPrefix(p, "/") || path.IsAbs(p) || isWindowsAbs(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == ".." {
			return false
		}
	}
	return true
}

// resolveWellKnownURL resolves a declared URL against the index URL and requires http(s).
func resolveWellKnownURL(indexURL, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("empty URL")
	}
	base, err := url.Parse(indexURL)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(r)
	scheme := strings.ToLower(u.Scheme)
	if u.Host == "" || (scheme != "http" && scheme != "https") {
		return "", errors.New("unsupported URL")
	}
	return u.String(), nil
}

// Fetch materializes the named skills into dir, the empty directory the caller owns. An empty name
// list fetches every entry. A digest mismatch or a rejected archive is an error; the caller must not
// fall back to another source.
func (w *WellKnown) Fetch(ctx context.Context, dir string, names []string) (Tree, error) {
	if dir == "" {
		return Tree{}, errors.New("no temporary directory was provided for the well-known fetch")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Tree{}, fmt.Errorf("creating the well-known directory: %w", err)
	}
	want := map[string]bool{}
	for _, name := range names {
		want[skill.NormalizeName(name)] = true
	}
	content := &WellKnownContent{BaseURL: w.BaseURL, Entries: map[string]WellKnownContentSkill{}}
	for _, s := range w.Skills {
		if len(want) > 0 && !want[skill.NormalizeName(s.Name)] {
			continue
		}
		entry, err := w.fetchSkill(ctx, dir, s)
		if err != nil {
			return Tree{}, err
		}
		content.Entries[skill.SanitizeName(s.Name)] = entry
	}
	return Tree{Dir: dir, WellKnown: content}, nil
}

// fetchSkill downloads one index entry into dir/name.
func (w *WellKnown) fetchSkill(ctx context.Context, dir string, s WellKnownSkill) (WellKnownContentSkill, error) {
	name := skill.SanitizeName(s.Name)
	target := filepath.Join(dir, name)
	out := WellKnownContentSkill{
		Name: name, Description: s.Description, Dir: target, Rel: name,
		SkillPath: path.Join(name, skillFile), Digest: s.Digest,
	}
	if len(s.Files) > 0 {
		if err := w.fetchLegacyFiles(ctx, target, s); err != nil {
			return WellKnownContentSkill{}, err
		}
		return out, nil
	}
	switch s.Type {
	case "skill-md":
		body, err := fetchWellKnownBytes(ctx, s.URL, wellKnownMaxUnpacked)
		if err != nil {
			return WellKnownContentSkill{}, err
		}
		if err := verifyWellKnownDigest(s.Name, s.Digest, body); err != nil {
			return WellKnownContentSkill{}, err
		}
		meta, err := skill.ParseSkillMD(body)
		if err != nil {
			return WellKnownContentSkill{}, fmt.Errorf("skill %q: %w", s.Name, err)
		}
		out.Description = meta.Description
		if err := os.MkdirAll(target, 0o755); err != nil {
			return WellKnownContentSkill{}, fmt.Errorf("writing the well-known skill: %w", err)
		}
		if err := os.WriteFile(filepath.Join(target, skillFile), body, 0o644); err != nil {
			return WellKnownContentSkill{}, fmt.Errorf("writing the well-known skill: %w", err)
		}
		return out, nil
	case "archive":
		return w.fetchArchive(ctx, target, s, out)
	default:
		return WellKnownContentSkill{}, downloadError{"Unsupported archive format"}
	}
}

// fetchArchive downloads and extracts one archive artifact, requiring a root SKILL.md.
func (w *WellKnown) fetchArchive(ctx context.Context, target string, s WellKnownSkill,
	out WellKnownContentSkill) (WellKnownContentSkill, error) {
	body, err := fetchWellKnownBytes(ctx, s.URL, wellKnownMaxUnpacked)
	if err != nil {
		return WellKnownContentSkill{}, err
	}
	if err := verifyWellKnownDigest(s.Name, s.Digest, body); err != nil {
		return WellKnownContentSkill{}, err
	}
	format := sniffFormat(body)
	if format != formatZip && format != formatTarGzip {
		return WellKnownContentSkill{}, downloadError{"Unsupported archive format"}
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".wellknown-*")
	if err != nil {
		return WellKnownContentSkill{}, fmt.Errorf("writing the well-known archive: %w", err)
	}
	stagedPath := staged.Name()
	defer func() { _ = os.Remove(stagedPath) }()
	if _, err := staged.Write(body); err != nil {
		_ = staged.Close()
		return WellKnownContentSkill{}, fmt.Errorf("writing the well-known archive: %w", err)
	}
	if err := staged.Close(); err != nil {
		return WellKnownContentSkill{}, fmt.Errorf("writing the well-known archive: %w", err)
	}
	if err := extractWellKnownArchive(stagedPath, target, format); err != nil {
		return WellKnownContentSkill{}, err
	}
	meta, err := skillFromDir(target)
	if err != nil {
		return WellKnownContentSkill{}, err
	}
	out.Description = meta.Description
	return out, nil
}

// fetchLegacyFiles downloads every declared file of a legacy entry, resolving each against the
// index URL. The skill cannot be installed without all of them.
func (w *WellKnown) fetchLegacyFiles(ctx context.Context, target string, s WellKnownSkill) error {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("writing the well-known skill: %w", err)
	}
	for _, file := range s.Files {
		fileURL, err := resolveWellKnownURL(w.IndexURL, file)
		if err != nil {
			return legacyFileError(s.Name, file, 0, err)
		}
		body, status, err := fetchWellKnownFile(ctx, fileURL)
		if err != nil {
			if status != 0 {
				return legacyFileError(s.Name, file, status, err)
			}
			return legacyFileError(s.Name, file, 0, err)
		}
		rel, err := archivePath(file)
		if err != nil {
			return err
		}
		if strings.EqualFold(rel, skillFile) {
			rel = skillFile
		}
		dest, err := archiveTarget(target, rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("writing the well-known skill: %w", err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return fmt.Errorf("writing the well-known skill: %w", err)
		}
	}
	if _, err := skill.ParseSkillMD(mustReadFile(filepath.Join(target, skillFile))); err != nil {
		return fmt.Errorf("skill %q: %w", s.Name, err)
	}
	return nil
}

// legacyFileError is the documented failure of one listed file.
func legacyFileError(name, file string, status int, err error) error {
	if status != 0 {
		return downloadError{fmt.Sprintf("Failed to download %q for skill %q (HTTP %d). "+
			"The skill cannot be installed without all listed files.", file, name, status)}
	}
	return downloadError{fmt.Sprintf("Failed to download %q for skill %q: %s. "+
		"The skill cannot be installed without all listed files.",
		file, name, RedactCredentials(err.Error()))}
}

// fetchWellKnownBytes performs one artifact request under the limit.
func fetchWellKnownBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	data, _, err := fetchWellKnownBody(ctx, rawURL, limit)
	return data, err
}

// fetchWellKnownFile performs one legacy file request and reports the HTTP status on rejection.
func fetchWellKnownFile(ctx context.Context, rawURL string) ([]byte, int, error) {
	return fetchWellKnownBody(ctx, rawURL, wellKnownMaxUnpacked)
}

// fetchWellKnownBody performs one request, refusing bodies over limit.
func fetchWellKnownBody(ctx context.Context, rawURL string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid well-known URL: %s", requestDetail(err))
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("download failed: %s", requestDetail(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.StatusCode, downloadError{fmt.Sprintf("Download failed with HTTP %d", resp.StatusCode)}
	}
	if resp.ContentLength > limit {
		return nil, resp.StatusCode, fmt.Errorf("artifact is larger than %d bytes", limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("download failed: %s", requestDetail(err))
	}
	if int64(len(data)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("artifact is larger than %d bytes", limit)
	}
	return data, resp.StatusCode, nil
}

// verifyWellKnownDigest compares the exact artifact bytes with the index digest.
func verifyWellKnownDigest(name, expected string, body []byte) error {
	sum := sha256.Sum256(body)
	got := "sha256:" + hex.EncodeToString(sum[:])
	if strings.EqualFold(expected, got) {
		return nil
	}
	return downloadError{fmt.Sprintf("Digest mismatch for skill %q: expected %s, received %s", name, expected, got)}
}

// extractWellKnownArchive extracts an archive artifact with the provider limits, refusing links.
func extractWellKnownArchive(bodyPath, dest string, format archiveFormat) error {
	l := limits{extractBytes: wellKnownMaxUnpacked, extractFiles: wellKnownMaxEntries}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("extracting the archive: %w", err)
	}
	switch format {
	case formatZip:
		r, err := zip.OpenReader(bodyPath)
		if err != nil {
			return fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		defer func() { _ = r.Close() }()
		return extractZip(&r.Reader, dest, l)
	case formatTarGzip:
		f, err := os.Open(bodyPath)
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		defer func() { _ = f.Close() }()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		defer func() { _ = gz.Close() }()
		return extractTarStrict(gz, dest, l)
	default:
		return downloadError{"Unsupported archive format"}
	}
}

// extractTarStrict writes the regular entries of a tar stream and refuses links outright.
func extractTarStrict(r io.Reader, dest string, l limits) error {
	tr := tar.NewReader(r)
	var total int64
	for entries := 0; ; {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", errNotAnArchive, err)
		}
		entries++
		if entries > l.extractFiles {
			return tooManyEntriesError(entries, l.extractFiles)
		}
		switch hdr.Typeflag {
		case tar.TypeSymlink, tar.TypeLink:
			return downloadError{"Archive links are not supported"}
		case tar.TypeDir:
			rel, err := archivePath(hdr.Name)
			if err != nil {
				return err
			}
			target, err := archiveTarget(dest, rel)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("extracting the archive: %w", err)
			}
		case tar.TypeReg:
			rel, err := archivePath(hdr.Name)
			if err != nil {
				return err
			}
			target, err := archiveTarget(dest, rel)
			if err != nil {
				return err
			}
			written, err := writeArchiveFile(target, tr, l.extractBytes-total)
			if err != nil {
				return err
			}
			total += written
			if total > l.extractBytes {
				return extractTooLargeError(l.extractBytes)
			}
		}
	}
}

// skillFromDir parses the root SKILL.md of a materialized provider skill.
func skillFromDir(dir string) (skill.Meta, error) {
	info, err := os.Stat(filepath.Join(dir, skillFile))
	if err != nil || !info.Mode().IsRegular() {
		return skill.Meta{}, downloadError{"Archive missing root SKILL.md"}
	}
	meta, err := skill.ParseSkillMD(mustReadFile(filepath.Join(dir, skillFile)))
	if err != nil {
		return skill.Meta{}, fmt.Errorf("archive SKILL.md: %w", err)
	}
	return meta, nil
}

// mustReadFile reads a file whose presence was just checked; a read error yields nil so the caller
// reports the parse failure.
func mustReadFile(p string) []byte {
	data, _ := os.ReadFile(p)
	return data
}
