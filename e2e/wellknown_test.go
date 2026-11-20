package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// wellKnownSchema is the only index schema the binary accepts.
const wellKnownSchema = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"

// wellKnownFixture serves an index and its artifacts, replaceable between runs.
type wellKnownFixture struct {
	*httptest.Server
	mu      sync.Mutex
	indexes map[string]string
	files   map[string]string
}

func newWellKnownFixture(t *testing.T) *wellKnownFixture {
	t.Helper()
	f := &wellKnownFixture{indexes: map[string]string{}, files: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		body, ok := f.indexes[r.URL.Path]
		if !ok {
			body, ok = f.files[r.URL.Path]
		}
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *wellKnownFixture) setIndex(path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indexes[path] = body
}

func (f *wellKnownFixture) setFile(path, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = body
}

// publishV2 serves content at /alpha.md and an index whose digest matches it.
func (f *wellKnownFixture) publishV2(content string) {
	f.setFile("/alpha.md", content)
	f.setIndex("/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"alpha","type":"skill-md","description":"test skill",`+
			`"url":"/alpha.md","digest":"%s"}]}`, wellKnownSchema, sha256Digest(content)))
}

func sha256Digest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestE2EWellKnownInstall(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(downloadDoc)
	source := fx.URL

	res := run(t, project, "add", source, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	installed := filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")
	raw, err := os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version one") {
		t.Fatalf("installed = %q, %v", raw, err)
	}

	entry := readLock(t, project).Skills["alpha"]
	if entry.SourceType != "well-known" || entry.Source != source || entry.SourceBaseURL != source {
		t.Errorf("entry = %+v, want the provider base", entry)
	}
	if entry.WellKnownDigest != sha256Digest(downloadDoc) {
		t.Errorf("wellKnownDigest = %q, want the index digest", entry.WellKnownDigest)
	}
	if entry.SkillPath != "alpha/SKILL.md" || entry.ComputedHash == "" {
		t.Errorf("entry = %+v, want skillPath and computedHash", entry)
	}

	if res := run(t, project, "check"); res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout:\n%s", res.exit, res.stdout)
	}

	fx.publishV2(strings.ReplaceAll(downloadDoc, "version one", "version two"))
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
	raw, err = os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version two") {
		t.Fatalf("installed = %q, %v", raw, err)
	}
	entry = readLock(t, project).Skills["alpha"]
	if entry.WellKnownDigest != sha256Digest(strings.ReplaceAll(downloadDoc, "version one", "version two")) {
		t.Errorf("wellKnownDigest = %q, want the new digest", entry.WellKnownDigest)
	}
	if res := run(t, project, "check"); res.exit != 0 {
		t.Fatalf("check after update exit = %d, want 0\nstdout:\n%s", res.exit, res.stdout)
	}
}

func TestE2EWellKnownDigestMismatch(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.setFile("/alpha.md", "tampered\n")
	fx.setIndex("/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"alpha","type":"skill-md","description":"d",`+
			`"url":"/alpha.md","digest":"%s"}]}`, wellKnownSchema, sha256Digest("expected\n")))

	res := run(t, project, "add", fx.URL, "-a", "opencode", "-y")
	if res.exit != 1 {
		t.Fatalf("add exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(strings.ToLower(res.stderr), "digest mismatch") {
		t.Errorf("stderr = %q, want the digest mismatch", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("a mismatched provider wrote a lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("a mismatched provider installed something: %v", err)
	}
}

func TestE2EWellKnownScopedIndex(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.setIndex("/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"root-skill","type":"skill-md","description":"d",`+
			`"url":"/root.md","digest":"%s"}]}`, wellKnownSchema, sha256Digest(downloadDoc)))
	fx.setFile("/root.md", downloadDoc)
	fx.setIndex("/s/alpha/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"alpha","type":"skill-md","description":"d",`+
			`"url":"/alpha.md","digest":"%s"}]}`, wellKnownSchema, sha256Digest(downloadDoc)))
	fx.setFile("/alpha.md", downloadDoc)

	res := run(t, project, "add", fx.URL+"/s/alpha", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("the scoped skill was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "root-skill")); !os.IsNotExist(err) {
		t.Errorf("the scoped source widened to the root catalog: %v", err)
	}
	entry := readLock(t, project).Skills["alpha"]
	if entry.SourceBaseURL != fx.URL+"/s/alpha" {
		t.Errorf("sourceBaseUrl = %q, want the scoped base", entry.SourceBaseURL)
	}
}

func TestE2EWellKnownFallbackToDownload(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.setFile("/skill.md", downloadDoc)

	res := run(t, project, "add", fx.URL+"/skill.md", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	entry := readLock(t, project).Skills["alpha"]
	if entry.SourceType != "download" {
		t.Errorf("sourceType = %q, want the unchanged download path", entry.SourceType)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("the download was not installed: %v", err)
	}
}
