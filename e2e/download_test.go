package e2e

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// downloadDoc is a valid SKILL.md served by the download tests.
const downloadDoc = "---\nname: alpha\ndescription: test skill\n---\nversion one\n"

// downloadFixture serves the body it holds at every path and lets a test replace it.
type downloadFixture struct {
	*httptest.Server
	mu          sync.Mutex
	body        []byte
	contentType string
}

func newDownloadFixture(t *testing.T, contentType string, body []byte) *downloadFixture {
	t.Helper()
	f := &downloadFixture{body: body, contentType: contentType}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		body, contentType := f.body, f.contentType
		f.mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *downloadFixture) set(body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body = body
}

func TestE2EDownloadSingleFile(t *testing.T) {
	project := t.TempDir()
	fx := newDownloadFixture(t, "text/markdown", []byte(downloadDoc))
	source := fx.URL + "/skill.md"

	res := run(t, project, "add", source, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	installedDir := filepath.Join(project, ".agents", "skills", "alpha")
	raw, err := os.ReadFile(filepath.Join(installedDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(raw), "version one") {
		t.Errorf("installed = %q, want the served body", raw)
	}

	lock := readLock(t, project)
	entry, ok := lock.Skills["alpha"]
	if !ok {
		t.Fatalf("lock has no alpha entry: %+v", lock.Skills)
	}
	if entry.SourceType != "download" {
		t.Errorf("sourceType = %q, want download", entry.SourceType)
	}
	if entry.Source != source || entry.SourceURL != source {
		t.Errorf("source = %q, sourceUrl = %q, want %q", entry.Source, entry.SourceURL, source)
	}
	if entry.SkillPath != "SKILL.md" {
		t.Errorf("skillPath = %q, want SKILL.md", entry.SkillPath)
	}
	if entry.Ref != "" || entry.Commit != "" {
		t.Errorf("ref = %q, commit = %q, want none", entry.Ref, entry.Commit)
	}
	if want := oracleHash(t, installedDir); entry.ComputedHash != want {
		t.Errorf("computedHash = %s, oracle says %s", entry.ComputedHash, want)
	}

	if res := run(t, project, "check"); res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout:\n%s", res.exit, res.stdout)
	}

	fx.set([]byte(strings.ReplaceAll(downloadDoc, "version one", "version two")))
	res = run(t, project, "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1 after the content changed\nstdout:\n%s", res.exit, res.stdout)
	}
	if !strings.Contains(res.stdout, "alpha has an update") {
		t.Errorf("check output = %q, want the update", res.stdout)
	}

	res = run(t, project, "update", "-p")
	if res.exit != 0 {
		t.Fatalf("update exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err = os.ReadFile(filepath.Join(installedDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "version two") {
		t.Errorf("installed = %q, want the new content", raw)
	}
	if entry := readLock(t, project).Skills["alpha"]; entry.Commit != "" {
		t.Errorf("commit = %q, want none for a download", entry.Commit)
	}

	if res := run(t, project, "check"); res.exit != 0 {
		t.Fatalf("check after update exit = %d, want 0\nstdout:\n%s", res.exit, res.stdout)
	}
}

func TestE2EDownloadArchive(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"pkg/alpha/SKILL.md":     downloadDoc,
		"pkg/alpha/reference.md": "supporting file\n",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	fx := newDownloadFixture(t, "application/zip", buf.Bytes())
	res := run(t, project, "add", fx.URL+"/skills", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	installedDir := filepath.Join(project, ".agents", "skills", "alpha")
	if _, err := os.Stat(filepath.Join(installedDir, "reference.md")); err != nil {
		t.Errorf("supporting file missing: %v", err)
	}
	entry := readLock(t, project).Skills["alpha"]
	if entry.SourceType != "download" || entry.SkillPath != "alpha/SKILL.md" {
		t.Errorf("entry = %+v, want the collapsed skill path", entry)
	}

	// install rebuilds the project from the recorded URL.
	if err := os.RemoveAll(installedDir); err != nil {
		t.Fatal(err)
	}
	res = run(t, project, "install")
	if res.exit != 0 {
		t.Fatalf("install exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(installedDir, "SKILL.md")); err != nil {
		t.Errorf("the skill was not restored: %v", err)
	}
}

func TestE2EDownloadLimits(t *testing.T) {
	// One byte past the 10 MiB default, with valid frontmatter so only the limit can reject it.
	body := append([]byte(downloadDoc), bytes.Repeat([]byte(" "), 10<<20)...)
	fx := newDownloadFixture(t, "text/markdown", body)
	source := fx.URL + "/skill.md"

	project := t.TempDir()
	res := run(t, project, "add", source, "-a", "opencode", "-y")
	if res.exit != 1 {
		t.Fatalf("add exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "larger than 10485760 bytes") {
		t.Errorf("stderr = %q, want the body limit", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("a rejected download wrote a lock: %v", err)
	}

	// The documented variable may raise the limit, and then the same body installs.
	res = runEnv(t, project, []string{"SKILLS_DOWNLOAD_MAX_BYTES=11000000"},
		"add", source, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add with a raised limit exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
}

func TestE2EDownloadTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"SKILL.md":     downloadDoc,
		"../evil.txt":  "escaped\n",
		"pkg/../../up": "escaped\n",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	fx := newDownloadFixture(t, "application/zip", buf.Bytes())
	res := run(t, project, "add", fx.URL+"/evil.zip", "-a", "opencode", "-y")
	if res.exit != 1 {
		t.Fatalf("add exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "unsafe path") {
		t.Errorf("stderr = %q, want the unsafe-path rejection", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("a rejected archive wrote a lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "evil.txt")); !os.IsNotExist(err) {
		t.Errorf("an archive entry escaped into the project: %v", err)
	}
}
