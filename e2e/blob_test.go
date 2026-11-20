package e2e

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// blobFallbackNotice is the one line a failed GitHub API read logs before cloning.
const blobFallbackNotice = "GitHub API unavailable; falling back to git clone"

// blobEntry is one entry of the recursive tree the stub serves.
type blobEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size"`
}

// blobRepo is a local git repository plus the API documents of one commit.
type blobRepo struct {
	work    string
	commit  string
	treeSHA string
	files   map[string]string
	entries []blobEntry
	blobs   map[string][]byte
}

// newBlobRepo commits files into a throwaway repository and reads back its commit, tree and blobs.
func newBlobRepo(t *testing.T, files map[string]string) *blobRepo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture uses POSIX paths")
	}
	work := t.TempDir()
	runGit(t, work, "init", "-q", "-b", "main")
	for path, content := range files {
		writeFixtureFile(t, work, path, content)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "one")

	repo := &blobRepo{
		work:    work,
		commit:  runGit(t, work, "rev-parse", "HEAD"),
		treeSHA: runGit(t, work, "rev-parse", "HEAD^{tree}"),
		files:   files,
		blobs:   map[string][]byte{},
	}
	out := gitBytes(t, work, "ls-tree", "-r", "-z", "-l", "HEAD")
	for _, line := range strings.Split(string(out), "\x00") {
		if line == "" {
			continue
		}
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("unexpected ls-tree entry %q", line)
		}
		fields := strings.Fields(meta)
		if len(fields) != 4 {
			t.Fatalf("unexpected ls-tree metadata %q", meta)
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil {
			t.Fatalf("ls-tree size %q: %v", fields[3], err)
		}
		repo.entries = append(repo.entries, blobEntry{
			Path: path, Mode: fields[0], Type: fields[1], SHA: fields[2], Size: &size,
		})
		repo.blobs[fields[2]] = []byte(files[path])
	}
	return repo
}

// gitBytes runs git and returns its raw output, without trimming.
func gitBytes(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// blobServer serves the commits, trees and blobs endpoints for a fixture repository.
type blobServer struct {
	*httptest.Server
	repo   *blobRepo
	omit   string
	reject func(*http.Request) (int, http.Header)
}

// newBlobServer starts the API stub. configure runs before the server accepts requests, so the
// handler reads its settings without a lock.
func newBlobServer(t *testing.T, repo *blobRepo, configure ...func(*blobServer)) *blobServer {
	t.Helper()
	s := &blobServer{repo: repo}
	for _, apply := range configure {
		apply(s)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

// blobSHA returns the Git blob SHA of a fixture path.
func (r *blobRepo) blobSHA(path string) string {
	for _, entry := range r.entries {
		if entry.Path == path {
			return entry.SHA
		}
	}
	return ""
}

func (s *blobServer) serve(w http.ResponseWriter, r *http.Request) {
	if s.reject != nil {
		if status, header := s.reject(r); status != 0 {
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
		writeBlobJSON(w, []map[string]any{{"sha": s.repo.commit}})
	case strings.HasPrefix(r.URL.Path, prefix+"git/trees/"):
		writeBlobJSON(w, map[string]any{
			"sha": s.repo.treeSHA, "truncated": false, "tree": s.repo.entries,
		})
	case strings.HasPrefix(r.URL.Path, prefix+"git/blobs/"):
		sha := strings.TrimPrefix(r.URL.Path, prefix+"git/blobs/")
		content, ok := s.repo.blobs[sha]
		if !ok || sha == s.omit {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeBlobJSON(w, map[string]any{
			"sha": sha, "size": len(content), "encoding": "base64",
			"content": blobBase64(content),
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// writeBlobJSON writes one JSON document.
func writeBlobJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

// blobBase64 encodes content with the line breaks GitHub uses.
func blobBase64(content []byte) string {
	encoded := base64.StdEncoding.EncodeToString(content)
	var b strings.Builder
	for len(encoded) > 40 {
		b.WriteString(encoded[:40])
		b.WriteString("\n")
		encoded = encoded[40:]
	}
	b.WriteString(encoded)
	return b.String()
}

// blobEnv is the environment of a blob-path run: the stub API, an optional PATH and no ambient
// GitHub credentials.
func blobEnv(apiURL, path string) []string {
	return []string{
		"SKILLS_GITHUB_API_URL=" + apiURL,
		"PATH=" + path,
		"GITHUB_TOKEN=",
		"GH_TOKEN=",
	}
}

// assertSameInstalledTree compares two directories file by file.
func assertSameInstalledTree(t *testing.T, left, right string) {
	t.Helper()
	walk := func(root string) map[string]string {
		files := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[filepath.ToSlash(rel)] = string(data)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
		return files
	}
	a, b := walk(left), walk(right)
	if len(a) != len(b) {
		t.Fatalf("installed files differ: %v vs %v", a, b)
	}
	for path, content := range a {
		if b[path] != content {
			t.Errorf("%s differs between the blob and clone installs", path)
		}
	}
}

func TestE2EBlobInstallMatchesClone(t *testing.T) {
	repo := newBlobRepo(t, map[string]string{
		"README.md":             "readme\n",
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
		"skills/alpha/ref.md":   "reference\n",
		"skills/beta/SKILL.md":  "---\nname: beta\ndescription: b\n---\n",
	})
	stub := newBlobServer(t, repo)
	before := cloneTempDirs(t)

	projectBlob := t.TempDir()
	res := runEnv(t, projectBlob, blobEnv(stub.URL, t.TempDir()),
		"add", "acme/skills", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("blob exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, blobFallbackNotice) {
		t.Errorf("the blob path fell back:\n%s", res.stderr)
	}

	projectClone := t.TempDir()
	res = run(t, projectClone, "add", repo.work, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("clone exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}

	for _, name := range []string{"alpha", "beta"} {
		blobDir := filepath.Join(projectBlob, ".agents", "skills", name)
		cloneDir := filepath.Join(projectClone, ".agents", "skills", name)
		assertSameInstalledTree(t, blobDir, cloneDir)
		if oracleHash(t, blobDir) != oracleHash(t, cloneDir) {
			t.Errorf("%s: the blob and clone content hashes differ", name)
		}
	}

	blobLock := readLock(t, projectBlob)
	cloneLock := readLock(t, projectClone)
	for _, name := range []string{"alpha", "beta"} {
		blobEntry, ok := blobLock.Skills[name]
		if !ok {
			t.Fatalf("the blob lock has no %s entry: %+v", name, blobLock.Skills)
		}
		cloneEntry := cloneLock.Skills[name]
		if blobEntry.ComputedHash != cloneEntry.ComputedHash {
			t.Errorf("%s: computedHash differs between blob and clone", name)
		}
		if blobEntry.Source != "acme/skills" || blobEntry.SourceType != "github" {
			t.Errorf("%s: source = %q, sourceType = %q", name, blobEntry.Source, blobEntry.SourceType)
		}
		if blobEntry.Commit != repo.commit {
			t.Errorf("%s: commit = %q, want %q", name, blobEntry.Commit, repo.commit)
		}
		if blobEntry.SkillPath != "skills/"+name+"/SKILL.md" {
			t.Errorf("%s: skillPath = %q", name, blobEntry.SkillPath)
		}
	}
	assertNoNewTempDirs(t, before)
}

func TestE2EBlobFallsBackOnIncompleteTree(t *testing.T) {
	repo := newBlobRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
		"skills/alpha/ref.md":   "reference\n",
	})
	stub := newBlobServer(t, repo, func(s *blobServer) {
		s.omit = repo.blobSHA("skills/alpha/ref.md")
	})

	fake := t.TempDir()
	script := `#!/bin/sh
while [ "$1" = "-c" ]; do shift 2; done
case "$1" in
  clone)
    for last in "$@"; do :; done
    /bin/mkdir -p "$last"
    /bin/cp -R "$FAKE_REPO"/. "$last"
    ;;
  rev-parse) printf '%s\n' "$FAKE_COMMIT" ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(fake, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(blobEnv(stub.URL, fake), "FAKE_REPO="+repo.work, "FAKE_COMMIT="+repo.commit)

	project := t.TempDir()
	res := runEnv(t, project, env, "add", "acme/skills", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want the clone fallback to succeed\nstderr:\n%s", res.exit, res.stderr)
	}
	if got := strings.Count(res.stderr, blobFallbackNotice); got != 1 {
		t.Errorf("fallback lines = %d, want 1:\n%s", got, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "ref.md")); err != nil {
		t.Errorf("the clone fallback did not install the whole skill: %v", err)
	}
	lock := readLock(t, project)
	if lock.Skills["alpha"].Commit != repo.commit {
		t.Errorf("commit = %q, want the cloned commit", lock.Skills["alpha"].Commit)
	}
}

func TestE2EBlobTokenLadder(t *testing.T) {
	repo := newBlobRepo(t, map[string]string{
		"skills/alpha/SKILL.md": "---\nname: alpha\ndescription: a\n---\n",
	})
	const goodToken = "s3cret-gh-token"
	const otherToken = "other-gh-token"
	stub := newBlobServer(t, repo, func(s *blobServer) {
		s.reject = func(r *http.Request) (int, http.Header) {
			if r.Header.Get("Authorization") == "Bearer "+goodToken {
				return 0, nil
			}
			return http.StatusForbidden, http.Header{"X-RateLimit-Remaining": {"0"}}
		}
	})

	env := append(blobEnv(stub.URL, t.TempDir()),
		"GITHUB_TOKEN="+goodToken, "GH_TOKEN="+otherToken)
	project := t.TempDir()
	res := runEnv(t, project, env, "add", "acme/skills", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want the token retry to succeed\nstderr:\n%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stdout+res.stderr, goodToken) ||
		strings.Contains(res.stdout+res.stderr, otherToken) {
		t.Errorf("a token was printed:\nstdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, blobFallbackNotice) {
		t.Errorf("the token ladder fell back:\n%s", res.stderr)
	}
}

func TestE2EBlobRootSkill(t *testing.T) {
	repo := newBlobRepo(t, map[string]string{
		"SKILL.md": "---\nname: root-demo\ndescription: r\n---\n",
		"notes.md": "supporting notes\n",
	})
	stub := newBlobServer(t, repo)

	projectBlob := t.TempDir()
	res := runEnv(t, projectBlob, blobEnv(stub.URL, t.TempDir()),
		"add", "acme/skills", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("blob exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	blobDir := filepath.Join(projectBlob, ".agents", "skills", "root-demo")
	entries, err := os.ReadDir(blobDir)
	if err != nil {
		t.Fatalf("the root skill was not installed: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Errorf("blob root skill payload = %v, want only SKILL.md", entries)
	}

	projectClone := t.TempDir()
	res = run(t, projectClone, "add", repo.work, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("clone exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(projectClone, ".agents", "skills", "root-demo", "notes.md")); err != nil {
		t.Errorf("the clone path no longer installs the whole root skill: %v", err)
	}
}
