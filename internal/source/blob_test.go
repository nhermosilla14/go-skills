package source

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestMain keeps the suite offline: any GitHub source that reaches the fast path without its own
// stub gets one immediate server error and falls back to the clone path.
func TestMain(m *testing.M) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	githubAPIBase = server.URL
	noticeWriter = io.Discard
	code := m.Run()
	server.Close()
	os.Exit(code)
}

// apiCall is one request the stub served.
type apiCall struct {
	Path  string
	Query string
	Auth  string
}

// blobStub serves the commits, trees and blobs endpoints for a small fixture repository.
type blobStub struct {
	*httptest.Server
	mu       sync.Mutex
	calls    []apiCall
	commit   string
	treeSHA  string
	entries  []blobTreeEntry
	blobs    map[string][]byte
	omitted  map[string]bool
	corrupt  map[string]bool
	badSize  map[string]bool
	truncate bool
	reject   func(*http.Request) (int, http.Header)
}

// newBlobStub builds the API documents for acme/skills.
func newBlobStub(t *testing.T, files map[string]string) *blobStub {
	t.Helper()
	stub := &blobStub{
		commit:  strings.Repeat("c", 40),
		treeSHA: strings.Repeat("d", 40),
		blobs:   map[string][]byte{},
		omitted: map[string]bool{},
		corrupt: map[string]bool{},
		badSize: map[string]bool{},
	}
	dirs := map[string]bool{}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		content := []byte(files[path])
		sha := gitBlobSHA(content)
		size := int64(len(content))
		stub.entries = append(stub.entries, blobTreeEntry{
			Path: path, Mode: "100644", Type: "blob", SHA: sha, Size: &size,
		})
		stub.blobs[sha] = content
		for parent := filepath.ToSlash(filepath.Dir(path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			dirs[parent] = true
		}
	}
	for dir := range dirs {
		stub.entries = append(stub.entries, blobTreeEntry{
			Path: dir, Mode: "040000", Type: "tree", SHA: strings.Repeat("e", 40),
		})
	}
	sort.Slice(stub.entries, func(i, j int) bool { return stub.entries[i].Path < stub.entries[j].Path })
	stub.Server = httptest.NewServer(http.HandlerFunc(stub.serve))
	t.Cleanup(stub.Close)
	return stub
}

func (s *blobStub) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, apiCall{
		Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"),
	})
	reject := s.reject
	s.mu.Unlock()
	if reject != nil {
		if status, header := reject(r); status != 0 {
			for key, values := range header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(status)
			return
		}
	}

	const prefix = "/repos/acme/skills/"
	switch {
	case r.URL.Path == prefix+"commits":
		if r.URL.RawQuery != "per_page=1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, []map[string]any{{"sha": s.commit}})
	case strings.HasPrefix(r.URL.Path, prefix+"git/trees/"):
		if strings.TrimPrefix(r.URL.Path, prefix+"git/trees/") != s.commit {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.RawQuery != "recursive=1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{
			"sha": s.treeSHA, "truncated": s.truncate, "tree": s.entries,
		})
	case strings.HasPrefix(r.URL.Path, prefix+"git/blobs/"):
		sha := strings.TrimPrefix(r.URL.Path, prefix+"git/blobs/")
		content, ok := s.blobs[sha]
		if !ok || s.omitted[sha] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if s.corrupt[sha] {
			content = append(append([]byte{}, content...), "tampered"...)
		}
		size := int64(len(content))
		if s.badSize[sha] {
			size++
		}
		writeJSON(w, map[string]any{
			"sha": sha, "size": size, "encoding": "base64",
			"content": wrapBase64(content),
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// writeJSON writes one JSON document.
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// wrapBase64 encodes content the way GitHub does, with line breaks inside the value.
func wrapBase64(content []byte) string {
	encoded := base64.StdEncoding.EncodeToString(content)
	var b strings.Builder
	for len(encoded) > 20 {
		b.WriteString(encoded[:20])
		b.WriteString("\n")
		encoded = encoded[20:]
	}
	b.WriteString(encoded)
	return b.String()
}

// paths returns the request paths the stub served.
func (s *blobStub) paths() []string {
	return s.snapshotPaths()
}

// snapshotPaths copies the recorded request paths.
func (s *blobStub) snapshotPaths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.calls))
	for i, call := range s.calls {
		out[i] = call.Path
	}
	return out
}

// snapshotCalls copies the recorded requests.
func (s *blobStub) snapshotCalls() []apiCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]apiCall(nil), s.calls...)
}

// useBlobStub points the fast path at a stub for the test, with no gh and no tokens in reach.
func useBlobStub(t *testing.T, stub *blobStub) {
	t.Helper()
	old := githubAPIBase
	githubAPIBase = stub.URL
	t.Cleanup(func() { githubAPIBase = old })
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("PATH", t.TempDir())
}

// captureNotice records the fallback lines of a test.
func captureNotice(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := noticeWriter
	noticeWriter = buf
	t.Cleanup(func() { noticeWriter = old })
	return buf
}

// acquireBlobStub acquires acme/skills from the stub.
func acquireBlobStub(t *testing.T, src Source) (Tree, string, error) {
	t.Helper()
	dir := t.TempDir()
	tree, err := Acquire(context.Background(), src, dir)
	return tree, dir, err
}

func TestBlobAcquireSequenceAndFiles(t *testing.T) {
	files := map[string]string{
		"README.md":             "readme",
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
		"skills/alpha/ref.md":   "reference",
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: b\n---\n",
	}
	stub := newBlobStub(t, files)
	useBlobStub(t, stub)
	tree, dir, err := acquireBlobStub(t, Source{Kind: KindGitHub, Source: "acme/skills"})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Dir != dir {
		t.Errorf("Dir = %q, want the repository root %q", tree.Dir, dir)
	}
	if tree.Commit != stub.commit {
		t.Errorf("Commit = %q, want %q", tree.Commit, stub.commit)
	}
	for path, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("reading %s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	calls := stub.paths()
	head := []string{
		"/repos/acme/skills/commits",
		"/repos/acme/skills/git/trees/" + stub.commit,
	}
	if strings.Join(calls[:len(head)], "\n") != strings.Join(head, "\n") {
		t.Errorf("first requests = %v, want %v", calls[:len(head)], head)
	}
	want := map[string]bool{}
	for path := range files {
		want["/repos/acme/skills/git/blobs/"+gitBlobSHA([]byte(files[path]))] = true
	}
	if len(calls) != len(head)+len(want) {
		t.Errorf("requests = %v, want %d", calls, len(head)+len(want))
	}
	for _, call := range calls[len(head):] {
		if !want[call] {
			t.Errorf("unexpected blob request %q", call)
		}
		delete(want, call)
	}
	if len(want) != 0 {
		t.Errorf("blobs never fetched: %v", want)
	}
}

func TestBlobAcquireSubpath(t *testing.T) {
	stub := newBlobStub(t, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
		"skills/alpha/ref.md":   "reference",
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: b\n---\n",
	})
	useBlobStub(t, stub)
	tree, dir, err := acquireBlobStub(t, Source{
		Kind: KindGitHub, Source: "acme/skills", Subpath: "skills/alpha",
	})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Dir != filepath.Join(dir, "skills", "alpha") {
		t.Errorf("Dir = %q, want the subpath", tree.Dir)
	}
	if _, err := os.Stat(filepath.Join(tree.Dir, "ref.md")); err != nil {
		t.Errorf("the subpath files were not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "beta")); !os.IsNotExist(err) {
		t.Errorf("a sibling subtree was fetched: %v", err)
	}
	for _, path := range stub.paths() {
		if strings.Contains(path, gitBlobSHA([]byte("---\nname: beta\ndescription: b\n---\n"))) {
			t.Errorf("the beta blob was fetched: %v", stub.paths())
		}
	}
}

func TestBlobAcquireRootSkillKeepsOnlySkillMD(t *testing.T) {
	stub := newBlobStub(t, map[string]string{
		"SKILL.md":     "---\nname: root\ndescription: r\n---\n",
		"notes.md":     "supporting",
		"scripts/x.sh": "#!/bin/sh\n",
	})
	useBlobStub(t, stub)
	tree, dir, err := acquireBlobStub(t, Source{Kind: KindGitHub, Source: "acme/skills"})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	entries, err := os.ReadDir(tree.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Errorf("root skill payload = %v, want only SKILL.md", entries)
	}
	if len(stub.paths()) != 3 {
		t.Errorf("requests = %v, want commits, tree and one blob", stub.paths())
	}
	if tree.Dir != dir {
		t.Errorf("Dir = %q, want %q", tree.Dir, dir)
	}
}

func TestBlobAcquireUsesARecordedCommit(t *testing.T) {
	stub := newBlobStub(t, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
	})
	useBlobStub(t, stub)
	commit := strings.Repeat("a", 40)
	stub.commit = commit
	tree, _, err := acquireBlobStub(t, Source{Kind: KindGitHub, Source: "acme/skills", Ref: commit})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit != commit {
		t.Errorf("Commit = %q, want the recorded commit", tree.Commit)
	}
	for _, path := range stub.paths() {
		if strings.HasSuffix(path, "/commits") {
			t.Errorf("a recorded commit resolved the default branch: %v", stub.paths())
		}
	}
}

func TestBlobAcquireSkipsExplicitRefs(t *testing.T) {
	stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": "x"})
	captureNotice(t)
	useBlobStub(t, stub)
	fakePath(t, false)

	tree, err := Acquire(context.Background(),
		Source{Kind: KindGitHub, Source: "acme/skills", Ref: "main"}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit == "" {
		t.Error("no commit recorded by the clone")
	}
	if calls := stub.paths(); len(calls) != 0 {
		t.Errorf("an explicit ref called the API: %v", calls)
	}
}

func TestBlobAcquireSkipsOtherHosts(t *testing.T) {
	stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": "x"})
	captureNotice(t)
	useBlobStub(t, stub)
	fakePath(t, false)

	src := Source{Kind: KindGit, Source: "file:///repo", SourceURL: "file:///repo"}
	if _, err := Acquire(context.Background(), src, t.TempDir()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if calls := stub.paths(); len(calls) != 0 {
		t.Errorf("a non-GitHub source called the API: %v", calls)
	}
}

func TestBlobAcquireFallsBackOnIncompleteTree(t *testing.T) {
	content := []byte("---\nname: alpha\ndescription: a\n---\n")
	sha := gitBlobSHA(content)
	cases := map[string]func(*blobStub){
		"missing blob":   func(s *blobStub) { s.omitted[sha] = true },
		"tampered blob":  func(s *blobStub) { s.corrupt[sha] = true },
		"wrong size":     func(s *blobStub) { s.badSize[sha] = true },
		"truncated tree": func(s *blobStub) { s.truncate = true },
	}
	for name, breakStub := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": string(content)})
			breakStub(stub)
			notice := captureNotice(t)
			useBlobStub(t, stub)
			fakePath(t, false)

			tree, err := Acquire(context.Background(),
				Source{Kind: KindGitHub, Source: "acme/skills"}, t.TempDir())
			if err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			if tree.Commit != "1111111111111111111111111111111111111111" {
				t.Errorf("Commit = %q, want the clone commit", tree.Commit)
			}
			if got := strings.Count(notice.String(), blobFallbackNotice); got != 1 {
				t.Errorf("notice lines = %d, want 1:\n%s", got, notice)
			}
		})
	}
}

func TestBlobAcquireFallsBackOnOversizedResponse(t *testing.T) {
	stub := newBlobStub(t, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
	})
	old := maxBlobResponse
	maxBlobResponse = 8
	t.Cleanup(func() { maxBlobResponse = old })
	notice := captureNotice(t)
	useBlobStub(t, stub)
	fakePath(t, false)

	if _, err := Acquire(context.Background(),
		Source{Kind: KindGitHub, Source: "acme/skills"}, t.TempDir()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !strings.Contains(notice.String(), blobFallbackNotice) {
		t.Errorf("no fallback notice: %q", notice)
	}
}

func TestBlobAcquireFallsBackOnMissingSubpath(t *testing.T) {
	stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": "x"})
	notice := captureNotice(t)
	useBlobStub(t, stub)
	fakePath(t, false)

	if _, err := Acquire(context.Background(), Source{
		Kind: KindGitHub, Source: "acme/skills", Subpath: "missing",
	}, t.TempDir()); err == nil {
		t.Error("Acquire succeeded, want the clone to report the missing subpath")
	}
	if !strings.Contains(notice.String(), blobFallbackNotice) {
		t.Errorf("no fallback notice: %q", notice)
	}
}

func TestBlobAcquireRejectsLinksAndSubmodules(t *testing.T) {
	cases := map[string]blobTreeEntry{
		"symlink":   {Path: "skills/alpha/link", Mode: "120000", Type: "blob", SHA: strings.Repeat("a", 40)},
		"submodule": {Path: "skills/alpha/mod", Mode: "160000", Type: "commit", SHA: strings.Repeat("b", 40)},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newBlobStub(t, map[string]string{
				"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
			})
			stub.entries = append(stub.entries, entry)
			notice := captureNotice(t)
			useBlobStub(t, stub)
			fakePath(t, false)
			if _, err := Acquire(context.Background(), Source{
				Kind: KindGitHub, Source: "acme/skills",
			}, t.TempDir()); err != nil {
				t.Fatalf("Acquire: %v", err)
			}
			if !strings.Contains(notice.String(), blobFallbackNotice) {
				t.Errorf("no fallback notice: %q", notice)
			}
		})
	}
}

func TestBlobAcquirePrivateRepoFallsBackWithoutToken(t *testing.T) {
	// A private repository answers 404 to anonymous requests; the clone path keeps its own
	// authentication, so the token is not sent to the API.
	stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": "x"})
	stub.reject = func(r *http.Request) (int, http.Header) {
		if r.Header.Get("Authorization") == "" {
			return http.StatusNotFound, nil
		}
		return 0, nil
	}
	notice := captureNotice(t)
	useBlobStub(t, stub)
	t.Setenv("GITHUB_TOKEN", "secret-token")
	fakePath(t, false)

	if _, err := Acquire(context.Background(),
		Source{Kind: KindGitHub, Source: "acme/skills"}, t.TempDir()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	for _, call := range stub.snapshotCalls() {
		if call.Auth != "" {
			t.Errorf("a token was sent after a 404: %v", stub.snapshotCalls())
		}
	}
	if !strings.Contains(notice.String(), blobFallbackNotice) {
		t.Errorf("no fallback notice: %q", notice)
	}
}

func TestBlobAcquireTokenLadder(t *testing.T) {
	stub := newBlobStub(t, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
	})
	stub.reject = func(r *http.Request) (int, http.Header) {
		if r.Header.Get("Authorization") == "" {
			return http.StatusForbidden, http.Header{"X-RateLimit-Remaining": {"0"}}
		}
		if r.Header.Get("Authorization") != "Bearer good-token" {
			return http.StatusUnauthorized, nil
		}
		return 0, nil
	}
	useBlobStub(t, stub)
	t.Setenv("GITHUB_TOKEN", "good-token")
	t.Setenv("GH_TOKEN", "other-token")

	if _, _, err := acquireBlobStub(t, Source{Kind: KindGitHub, Source: "acme/skills"}); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	var sawAnonymous, sawToken bool
	for _, call := range stub.snapshotCalls() {
		switch call.Auth {
		case "":
			sawAnonymous = true
		case "Bearer good-token":
			sawToken = true
		default:
			t.Errorf("unexpected credential %q", call.Auth)
		}
	}
	if !sawAnonymous || !sawToken {
		t.Errorf("calls = %v, want an anonymous attempt and a token retry", stub.snapshotCalls())
	}
}

func TestBlobAcquireGHTOKENIsTheFallback(t *testing.T) {
	stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": "---\nname: a\ndescription: d\n---\n"})
	stub.reject = func(r *http.Request) (int, http.Header) {
		if r.Header.Get("Authorization") != "Bearer gh-token" {
			return http.StatusForbidden, http.Header{"Retry-After": {"1"}}
		}
		return 0, nil
	}
	useBlobStub(t, stub)
	t.Setenv("GH_TOKEN", "gh-token")

	if _, _, err := acquireBlobStub(t, Source{Kind: KindGitHub, Source: "acme/skills"}); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	for _, call := range stub.snapshotCalls() {
		if call.Auth != "" && call.Auth != "Bearer gh-token" {
			t.Errorf("a token other than GH_TOKEN was used: %q", call.Auth)
		}
	}
}

func TestBlobAcquireGHApiFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake gh executable is a POSIX shell script")
	}
	files := map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
		"skills/alpha/ref.md":   "reference",
	}
	stub := newBlobStub(t, files)
	stub.reject = func(*http.Request) (int, http.Header) {
		return http.StatusForbidden, http.Header{"X-RateLimit-Remaining": {"0"}}
	}
	useBlobStub(t, stub)

	dir := t.TempDir()
	docs := map[string]string{
		"commits.json": mustJSON(t, []map[string]any{{"sha": stub.commit}}),
		"trees.json": mustJSON(t, map[string]any{
			"sha": stub.treeSHA, "truncated": false, "tree": stub.entries,
		}),
	}
	for sha, content := range stub.blobs {
		docs["blob-"+sha+".json"] = mustJSON(t, map[string]any{
			"sha": sha, "size": len(content), "encoding": "base64",
			"content": base64.StdEncoding.EncodeToString(content),
		})
	}
	for name, body := range docs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(t.TempDir(), "gh.log")
	script := `#!/bin/sh
printf 'gh %s\n' "$*" >> "$GH_LOG"
case "$2" in
  *commits*) /bin/cat "$GH_FIXTURE/commits.json" ;;
  *trees*) /bin/cat "$GH_FIXTURE/trees.json" ;;
  *blobs/*) /bin/cat "$GH_FIXTURE/blob-${2##*/}.json" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("GH_LOG", logPath)
	t.Setenv("GH_FIXTURE", dir)

	tree, err := Acquire(context.Background(), Source{Kind: KindGitHub, Source: "acme/skills"}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit != stub.commit {
		t.Errorf("Commit = %q", tree.Commit)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("gh was not called: %v", err)
	}
	if !strings.Contains(string(log), "api repos/acme/skills/commits?per_page=1 --method GET") {
		t.Errorf("gh call = %q", log)
	}
}

// mustJSON renders a value as JSON, for the fake gh fixture.
func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBlobAcquireNeverPrintsTheToken(t *testing.T) {
	stub := newBlobStub(t, map[string]string{"skills/alpha/SKILL.md": "x"})
	stub.reject = func(*http.Request) (int, http.Header) {
		return http.StatusUnauthorized, nil
	}
	notice := captureNotice(t)
	useBlobStub(t, stub)
	t.Setenv("GITHUB_TOKEN", "super-secret-token")
	fakePath(t, false)

	tree, err := Acquire(context.Background(),
		Source{Kind: KindGitHub, Source: "acme/skills"}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if strings.Contains(notice.String(), "super-secret-token") {
		t.Errorf("the notice leaks the token: %q", notice)
	}
	if strings.Contains(tree.Commit, "super-secret-token") {
		t.Error("the commit leaks the token")
	}
}
