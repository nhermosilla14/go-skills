package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const wellKnownTestDigest = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// indexServer serves a fixed body per path and records every request.
type indexServer struct {
	*httptest.Server
	mu       sync.Mutex
	routes   map[string]string
	requests []string
	headers  []http.Header
}

func newIndexServer(t *testing.T, routes map[string]string) *indexServer {
	t.Helper()
	s := &indexServer{routes: routes}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, r.URL.Path)
		s.headers = append(s.headers, r.Header.Clone())
		body, ok := s.routes[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *indexServer) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

func (s *indexServer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = nil
	s.headers = nil
}

func v2Index(entries ...string) string {
	return `{"$schema":"` + wellKnownSchemaV2 + `","skills":[` + strings.Join(entries, ",") + `]}`
}

func v2JSONEntry(name, kind, url, digest string) string {
	return fmt.Sprintf(`{"name":%q,"type":%q,"description":"a skill","url":%q,"digest":%q}`,
		name, kind, url, digest)
}

func legacyIndex(entries ...string) string {
	return `{"skills":[` + strings.Join(entries, ",") + `]}`
}

func legacyJSONEntry(name string, files ...string) string {
	quoted := make([]string, len(files))
	for i, f := range files {
		quoted[i] = fmt.Sprintf("%q", f)
	}
	return fmt.Sprintf(`{"name":%q,"description":"a skill","files":[%s]}`, name, strings.Join(quoted, ","))
}

func digestBody(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestProbeWellKnownCandidateOrderAndPriority(t *testing.T) {
	srv := newIndexServer(t, map[string]string{
		"/s/alpha/.well-known/agent-skills/index.json": v2Index(
			v2JSONEntry("alpha", "skill-md", "https://example.invalid/alpha.md", wellKnownTestDigest)),
		"/.well-known/agent-skills/index.json": v2Index(
			v2JSONEntry("root", "skill-md", "https://example.invalid/root.md", wellKnownTestDigest)),
		"/s/alpha/.well-known/skills/index.json": legacyIndex(legacyJSONEntry("legacy", "SKILL.md")),
	})
	origin := srv.URL

	idx, err := ProbeWellKnown(context.Background(), origin+"/s/alpha", false)
	if err != nil {
		t.Fatalf("ProbeWellKnown: %v", err)
	}
	if idx == nil || len(idx.Skills) != 1 || idx.Skills[0].Name != "alpha" {
		t.Fatalf("index = %+v, want the highest-priority scoped index", idx)
	}
	if idx.BaseURL != origin+"/s/alpha" {
		t.Errorf("BaseURL = %q, want the scoped base", idx.BaseURL)
	}
	want := []string{
		"/s/alpha/.well-known/agent-skills/index.json",
		"/.well-known/agent-skills/index.json",
		"/s/alpha/.well-known/skills/index.json",
		"/.well-known/skills/index.json",
	}
	if got := srv.requested(); !equalStrings(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}

	// A bare origin only has the two root candidates.
	srv.reset()
	idx, err = ProbeWellKnown(context.Background(), origin, false)
	if err != nil {
		t.Fatalf("ProbeWellKnown origin: %v", err)
	}
	if idx == nil || idx.Skills[0].Name != "root" {
		t.Fatalf("index = %+v, want the root index", idx)
	}
	if got := srv.requested(); !equalStrings(got, []string{
		"/.well-known/agent-skills/index.json", "/.well-known/skills/index.json",
	}) {
		t.Errorf("requests = %v, want the two root candidates", got)
	}
}

func TestProbeWellKnownSendsTheUpdateHeader(t *testing.T) {
	srv := newIndexServer(t, map[string]string{
		"/.well-known/agent-skills/index.json": v2Index(),
	})
	if _, err := ProbeWellKnown(context.Background(), srv.URL, true); err != nil {
		t.Fatalf("ProbeWellKnown: %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.headers) == 0 || srv.headers[0].Get(wellKnownUpdateHeader) != "1" {
		t.Errorf("headers = %v, want the update check header", srv.headers)
	}
}

func TestProbeWellKnownSharesOneBudget(t *testing.T) {
	previous := wellKnownBudget
	wellKnownBudget = 50 * time.Millisecond
	defer func() { wellKnownBudget = previous }()

	var (
		mu      sync.Mutex
		handled int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		handled++
		first := handled == 1
		mu.Unlock()
		if first {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(v2Index()))
	}))
	defer srv.Close()

	start := time.Now()
	idx, err := ProbeWellKnown(context.Background(), srv.URL, false)
	if err != nil {
		t.Fatalf("ProbeWellKnown: %v", err)
	}
	if idx != nil {
		t.Errorf("index = %+v, want none after the budget expired", idx)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("probe took %v, want the shared budget to end it", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if handled != 1 {
		t.Errorf("handled %d requests, want the rest aborted by the shared budget", handled)
	}
}

func TestParseWellKnownIndexV2(t *testing.T) {
	indexURL := "https://example.com/s/alpha/.well-known/agent-skills/index.json"
	doc := v2Index(
		v2JSONEntry("alpha", "skill-md", "artifacts/alpha.md", wellKnownTestDigest),
		v2JSONEntry("archive-skill", "archive", "https://cdn.example.com/a.zip", wellKnownTestDigest),
		`{"name":"bad name","type":"skill-md","description":"d","url":"x.md","digest":"`+wellKnownTestDigest+`"}`,
		`{"name":"bad-digest","type":"skill-md","description":"d","url":"x.md","digest":"sha256:zz"}`,
		`{"name":"bad-type","type":"tarball","description":"d","url":"x.md","digest":"`+wellKnownTestDigest+`"}`,
		`{"name":"no-desc","type":"skill-md","url":"x.md","digest":"`+wellKnownTestDigest+`"}`,
	)
	idx, ok := parseWellKnownIndex(indexURL, []byte(doc))
	if !ok {
		t.Fatal("parseWellKnownIndex rejected a valid v2 index")
	}
	if len(idx.Skills) != 2 {
		t.Fatalf("skills = %+v, want the two valid entries", idx.Skills)
	}
	if got, want := idx.Skills[0].URL, "https://example.com/s/alpha/.well-known/agent-skills/artifacts/alpha.md"; got != want {
		t.Errorf("resolved URL = %q, want %q", got, want)
	}
	if idx.Skills[0].Digest != wellKnownTestDigest || idx.Skills[1].Type != "archive" {
		t.Errorf("entries = %+v", idx.Skills)
	}

	if _, ok := parseWellKnownIndex(indexURL, []byte(
		`{"$schema":"https://schemas.agentskills.io/discovery/9.9.9/schema.json","skills":[]}`)); ok {
		t.Error("a different schema was accepted")
	}
	if _, ok := parseWellKnownIndex(indexURL, []byte(`not json`)); ok {
		t.Error("a non-JSON body was accepted")
	}
	if _, ok := parseWellKnownIndex(indexURL, []byte(`{"skills":{}}`)); ok {
		t.Error("a non-array skills field was accepted")
	}
	empty, ok := parseWellKnownIndex(indexURL, []byte(v2Index()))
	if !ok || len(empty.Skills) != 0 {
		t.Errorf("empty index = %+v, %v, want a valid empty index", empty, ok)
	}
	if _, ok := parseWellKnownIndex(indexURL, []byte(v2Index(
		`{"name":"bad","type":"skill-md","description":"d","url":"x.md","digest":"nope"}`))); ok {
		t.Error("an index whose entries all fail was accepted")
	}
}

func TestParseWellKnownIndexLegacyAllOrNothing(t *testing.T) {
	valid := legacyIndex(
		legacyJSONEntry("alpha", "SKILL.md", "reference/a.md"),
		legacyJSONEntry("beta", "skill.md"),
	)
	idx, ok := parseWellKnownIndex("https://example.com/index.json", []byte(valid))
	if !ok || len(idx.Skills) != 2 {
		t.Fatalf("index = %+v, %v, want both legacy entries", idx, ok)
	}
	if idx.Skills[1].Files[0] != "skill.md" {
		t.Errorf("files = %v", idx.Skills[1].Files)
	}

	rejected := []string{
		legacyIndex(legacyJSONEntry("alpha", "SKILL.md"), legacyJSONEntry("Bad_Name", "SKILL.md")),
		legacyIndex(legacyJSONEntry("alpha")),
		legacyIndex(legacyJSONEntry("alpha", "../escape.md", "SKILL.md")),
		legacyIndex(legacyJSONEntry("alpha", "/abs/SKILL.md", "SKILL.md")),
		legacyIndex(legacyJSONEntry("alpha", "SKILL.md", "bad\x00name")),
		legacyIndex(legacyJSONEntry("alpha", "reference/a.md")),
		legacyIndex(legacyJSONEntry("-alpha", "SKILL.md")),
		legacyIndex(legacyJSONEntry("a--b", "SKILL.md")),
		`{"skills":[{"name":`,
	}
	for _, doc := range rejected {
		if _, ok := parseWellKnownIndex("https://example.com/index.json", []byte(doc)); ok {
			t.Errorf("index was accepted:\n%s", doc)
		}
	}
}

func TestFetchWellKnownDigestMismatchIsFatal(t *testing.T) {
	srv := newIndexServer(t, map[string]string{"/alpha.md": "tampered\n"})
	idx := &WellKnown{
		BaseURL:  srv.URL,
		IndexURL: srv.URL + "/.well-known/agent-skills/index.json",
		Skills: []WellKnownSkill{{
			Name: "alpha", Type: "skill-md", URL: srv.URL + "/alpha.md",
			Digest: "sha256:" + strings.Repeat("0", 64),
		}},
	}
	dir := t.TempDir()
	_, err := idx.Fetch(context.Background(), dir, []string{"alpha"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "digest mismatch") {
		t.Fatalf("err = %v, want a digest mismatch", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a mismatched skill left content behind: %v", entries)
	}
}

func TestFetchWellKnownLegacyFetchesOnlySelectedSkills(t *testing.T) {
	doc := "---\nname: alpha\ndescription: alpha skill\n---\n"
	srv := newIndexServer(t, map[string]string{
		"/idx/SKILL.md": doc,
		"/idx/ref.md":   "reference\n",
	})
	idx := &WellKnown{
		BaseURL:  srv.URL + "/idx",
		IndexURL: srv.URL + "/idx/index.json",
		Skills: []WellKnownSkill{
			{Name: "alpha", Files: []string{"SKILL.md", "ref.md"}},
			{Name: "beta", Files: []string{"SKILL.md", "missing.md"}},
		},
	}
	dir := t.TempDir()
	tree, err := idx.Fetch(context.Background(), dir, []string{"alpha"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	entry, ok := tree.WellKnown.Entry("alpha")
	if !ok || entry.Digest != "" {
		t.Fatalf("entry = %+v, %v, want a legacy entry without a digest", entry, ok)
	}
	if got, err := os.ReadFile(filepath.Join(entry.Dir, "ref.md")); err != nil || string(got) != "reference\n" {
		t.Errorf("reference = %q, %v", got, err)
	}
	for _, path := range srv.requested() {
		if path == "/idx/missing.md" {
			t.Error("an unselected skill's file was fetched")
		}
	}

	_, err = idx.Fetch(context.Background(), t.TempDir(), []string{"beta"})
	if err == nil || !strings.Contains(err.Error(), "missing.md") ||
		!strings.Contains(err.Error(), "without all listed files") {
		t.Fatalf("err = %v, want the documented file failure", err)
	}
}

func TestFetchWellKnownArchiveRules(t *testing.T) {
	doc := []byte("---\nname: alpha\ndescription: archive skill\n---\n")

	t.Run("root skill", func(t *testing.T) {
		body := zipArchive(t, map[string][]byte{"SKILL.md": doc, "ref.md": []byte("ref\n")}, false)
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		if _, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"}); err != nil {
			t.Fatalf("Fetch: %v", err)
		}
	})

	t.Run("missing root skill", func(t *testing.T) {
		body := zipArchive(t, map[string][]byte{"pkg/SKILL.md": doc}, false)
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		_, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
		if err == nil || !strings.Contains(err.Error(), "root SKILL.md") {
			t.Fatalf("err = %v, want the missing-root rejection", err)
		}
	})

	t.Run("zip link", func(t *testing.T) {
		body := zipArchive(t, map[string][]byte{"SKILL.md": doc, "link": nil}, true)
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		_, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
		if err == nil || !strings.Contains(err.Error(), "links are not supported") {
			t.Fatalf("err = %v, want the link rejection", err)
		}
	})

	t.Run("tar.gz link", func(t *testing.T) {
		body := tarGzArchive(t, doc, true)
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		_, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
		if err == nil || !strings.Contains(err.Error(), "links are not supported") {
			t.Fatalf("err = %v, want the link rejection", err)
		}
	})

	t.Run("entry limit", func(t *testing.T) {
		previous := wellKnownMaxEntries
		wellKnownMaxEntries = 1
		defer func() { wellKnownMaxEntries = previous }()
		body := zipArchive(t, map[string][]byte{"SKILL.md": doc, "ref.md": []byte("ref\n")}, false)
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		_, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
		if err == nil || !strings.Contains(err.Error(), "too many files") {
			t.Fatalf("err = %v, want the entry limit", err)
		}
	})

	t.Run("extracted size limit", func(t *testing.T) {
		previous := wellKnownMaxUnpacked
		wellKnownMaxUnpacked = 1000
		defer func() { wellKnownMaxUnpacked = previous }()
		big := append(append([]byte(nil), doc...), bytes.Repeat([]byte(" "), 5000)...)
		body := zipArchive(t, map[string][]byte{"SKILL.md": big}, false)
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		_, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
		if err == nil || !strings.Contains(err.Error(), "extracts to more than") {
			t.Fatalf("err = %v, want the extracted size limit", err)
		}
	})

	t.Run("unsupported format", func(t *testing.T) {
		body := []byte("plain text\n")
		idx := &WellKnown{Skills: []WellKnownSkill{{
			Name: "alpha", Type: "archive", URL: serveBody(t, body), Digest: digestBody(body),
		}}}
		_, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
		if err == nil || !strings.Contains(err.Error(), "Unsupported archive format") {
			t.Fatalf("err = %v, want the format rejection", err)
		}
	})
}

func TestFetchWellKnownLegacyNormalizesSkillMDCase(t *testing.T) {
	doc := "---\nname: alpha\ndescription: case skill\n---\n"
	srv := newIndexServer(t, map[string]string{"/idx/skill.md": doc})
	idx := &WellKnown{
		BaseURL:  srv.URL + "/idx",
		IndexURL: srv.URL + "/idx/index.json",
		Skills:   []WellKnownSkill{{Name: "alpha", Files: []string{"skill.md"}}},
	}
	tree, err := idx.Fetch(context.Background(), t.TempDir(), []string{"alpha"})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	entry, _ := tree.WellKnown.Entry("alpha")
	if got, err := os.ReadFile(filepath.Join(entry.Dir, "SKILL.md")); err != nil || string(got) != doc {
		t.Errorf("SKILL.md = %q, %v, want the declared file under the canonical name", got, err)
	}
}

func TestProbeWellKnownDoesNotWidenAScopedSource(t *testing.T) {
	srv := newIndexServer(t, map[string]string{
		"/.well-known/agent-skills/index.json": v2Index(
			v2JSONEntry("root", "skill-md", "https://example.invalid/root.md", wellKnownTestDigest)),
	})
	_, err := ProbeWellKnown(context.Background(), srv.URL+"/s/alpha", false)
	if !errors.Is(err, ErrWellKnownScopeNotFound) {
		t.Fatalf("err = %v, want the scope refusal", err)
	}

	scopedEmpty := newIndexServer(t, map[string]string{
		"/s/alpha/.well-known/agent-skills/index.json": v2Index(),
		"/.well-known/agent-skills/index.json": v2Index(
			v2JSONEntry("root", "skill-md", "https://example.invalid/root.md", wellKnownTestDigest)),
	})
	_, err = ProbeWellKnown(context.Background(), scopedEmpty.URL+"/s/alpha", false)
	if !errors.Is(err, ErrWellKnownScopeNotFound) {
		t.Fatalf("err = %v, want the empty scoped index refused", err)
	}

	none := newIndexServer(t, map[string]string{})
	idx, err := ProbeWellKnown(context.Background(), none.URL+"/s/alpha", false)
	if err != nil || idx != nil {
		t.Errorf("index = %+v, err = %v, want a plain fallthrough", idx, err)
	}
}

// serveBody returns an artifact URL served by a test server.
func serveBody(t *testing.T, body []byte) string {
	t.Helper()
	srv := newIndexServer(t, map[string]string{"/artifact": string(body)})
	return srv.URL + "/artifact"
}

// zipArchive builds a zip with the given entries. link marks the last entry a symlink.
func zipArchive(t *testing.T, files map[string][]byte, link bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if link {
			header.SetMode(os.ModeSymlink | 0o777)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tarGzArchive builds a tar.gz holding SKILL.md and optionally a symlink entry.
func tarGzArchive(t *testing.T, doc []byte, link bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "SKILL.md", Mode: 0o644, Size: int64(len(doc))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(doc); err != nil {
		t.Fatal(err)
	}
	if link {
		if err := tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
