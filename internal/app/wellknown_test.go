package app

import (
	"context"
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

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// wellKnownFixture serves an index and its artifacts, both replaceable between runs.
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

// publishV2 serves content at /alpha.md and publishes an index whose digest matches it.
func (f *wellKnownFixture) publishV2(content string) {
	digest := digestOf([]byte(content))
	f.setFile("/alpha.md", content)
	f.setIndex("/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"alpha","type":"skill-md","description":"test skill",`+
			`"url":"/alpha.md","digest":"%s"}]}`, wellKnownSchemaURL, digest))
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// wellKnownSchemaURL is the only index schema the tool accepts.
const wellKnownSchemaURL = "https://schemas.agentskills.io/discovery/0.2.0/schema.json"

const wellKnownDoc = "---\nname: alpha\ndescription: test skill\n---\nversion one\n"

// wellKnownEnv isolates temp dirs and home like the download tests do.
func wellKnownEnv(t *testing.T, project string) Env {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	return addEnv(t, project)
}

func addWellKnown(t *testing.T, project, source string, extra AddOptions) Report {
	t.Helper()
	opts := extra
	opts.Source = source
	if len(opts.Agents) == 0 {
		opts.Agents = []string{"opencode"}
	}
	opts.Yes = true
	rep, err := Add(opts, wellKnownEnv(t, project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	return rep
}

func TestAddWellKnownV2RecordsTheProvider(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(wellKnownDoc)

	rep := addWellKnown(t, project, fx.URL, AddOptions{})
	if rep.Source != fx.URL {
		t.Errorf("report source = %q, want the provider base", rep.Source)
	}

	installed := installedSkillDir(project, "alpha")
	raw, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
	if err != nil || !strings.Contains(string(raw), "version one") {
		t.Fatalf("installed = %q, %v", raw, err)
	}
	entry := lockEntryOf(t, project, "alpha")
	if entry.SourceType != "well-known" || entry.Source != fx.URL || entry.SourceBaseURL != fx.URL {
		t.Errorf("entry = %+v, want the well-known provider", entry)
	}
	if entry.WellKnownDigest != digestOf([]byte(wellKnownDoc)) {
		t.Errorf("wellKnownDigest = %q, want the index digest", entry.WellKnownDigest)
	}
	if entry.SkillPath != "alpha/SKILL.md" || entry.SourceURL != "" || entry.Ref != "" || entry.Commit != "" {
		t.Errorf("entry = %+v, want only skillPath", entry)
	}
	want, err := hash.Dir(installed)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ComputedHash != want {
		t.Errorf("computedHash = %q, want %q", entry.ComputedHash, want)
	}
}

func TestAddWellKnownGlobalFolderHashIsEmpty(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(wellKnownDoc)

	env := wellKnownEnv(t, project)
	if _, err := Add(AddOptions{
		Source: fx.URL, Agents: []string{"opencode"}, Yes: true, Global: true,
	}, env); err != nil {
		t.Fatalf("Add: %v", err)
	}
	lock, err := lockfile.LoadGlobal(agents.GlobalLockPath(env.AgentEnv))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := lock.Get("alpha")
	if !ok {
		t.Fatalf("global lock has no alpha: %+v", lock.Skills)
	}
	if entry.SourceType != "well-known" || entry.SourceBaseURL != fx.URL ||
		entry.WellKnownDigest != digestOf([]byte(wellKnownDoc)) {
		t.Errorf("entry = %+v, want the provider fields", entry)
	}
	if entry.SkillFolderHash != "" {
		t.Errorf("skillFolderHash = %q, want empty for a well-known entry", entry.SkillFolderHash)
	}

	rep, err := Check(context.Background(), CheckOptions{Global: true}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("report = %+v, want a current global skill", rep)
	}
	fx.publishV2(strings.ReplaceAll(wellKnownDoc, "version one", "version two"))
	rep, err = Check(context.Background(), CheckOptions{Global: true}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Updates) != 1 || rep.Updates[0] != "alpha" {
		t.Errorf("updates = %v, want alpha", rep.Updates)
	}

	urep, err := Update(UpdateOptions{Global: true, Yes: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !urep.Changed() || urep.Failed() {
		t.Fatalf("report = %+v, want the global skill updated", urep)
	}
	lock, err = lockfile.LoadGlobal(agents.GlobalLockPath(env.AgentEnv))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ = lock.Get("alpha")
	if entry.WellKnownDigest != digestOf([]byte(strings.ReplaceAll(wellKnownDoc, "version one", "version two"))) {
		t.Errorf("wellKnownDigest = %q, want the new digest", entry.WellKnownDigest)
	}
	if entry.SkillFolderHash != "" {
		t.Errorf("skillFolderHash = %q, want it still empty after an update", entry.SkillFolderHash)
	}
}

func TestUpdateWellKnownReinstallsChangedContent(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(wellKnownDoc)
	addWellKnown(t, project, fx.URL, AddOptions{})
	env := wellKnownEnv(t, project)

	rep, err := Update(UpdateOptions{Project: true, Yes: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateCurrent {
		t.Fatalf("results = %+v, want the skill current", rep.Results)
	}

	changed := strings.ReplaceAll(wellKnownDoc, "version one", "version two")
	fx.publishV2(changed)
	rep, err = Update(UpdateOptions{Project: true, Yes: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated || !rep.Changed() || rep.Failed() {
		t.Fatalf("report = %+v, want the changed skill updated", rep)
	}
	raw, err := os.ReadFile(filepath.Join(installedSkillDir(project, "alpha"), "SKILL.md"))
	if err != nil || !strings.Contains(string(raw), "version two") {
		t.Fatalf("installed = %q, %v", raw, err)
	}
	entry := lockEntryOf(t, project, "alpha")
	if entry.WellKnownDigest != digestOf([]byte(changed)) {
		t.Errorf("wellKnownDigest = %q, want the new digest", entry.WellKnownDigest)
	}
}

func TestCheckWellKnownReportsChangedContent(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(wellKnownDoc)
	addWellKnown(t, project, fx.URL, AddOptions{})
	env := wellKnownEnv(t, project)

	rep, err := Check(context.Background(), CheckOptions{}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("report = %+v, want a current skill", rep)
	}

	fx.publishV2(strings.ReplaceAll(wellKnownDoc, "version one", "version two"))
	rep, err = Check(context.Background(), CheckOptions{}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Updates) != 1 || rep.Updates[0] != "alpha" || !rep.Failed() {
		t.Fatalf("report = %+v, want alpha reported as an update", rep)
	}
}

func TestRestoreWellKnownRefetches(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(wellKnownDoc)
	addWellKnown(t, project, fx.URL, AddOptions{})
	if err := os.RemoveAll(installedSkillDir(project, "alpha")); err != nil {
		t.Fatal(err)
	}

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}}, wellKnownEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Unrestorable) != 0 {
		t.Fatalf("unrestorable = %+v, want the provider restored", rep.Unrestorable)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the skill restored", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(installedSkillDir(project, "alpha"), "SKILL.md")); err != nil {
		t.Errorf("the skill was not restored: %v", err)
	}
}

func TestAddWellKnownDigestMismatchIsFatal(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	// The artifact is served but the index declares a different digest. A valid download body at
	// the source URL proves a fallback did not happen.
	fx.setFile("/alpha.md", "tampered\n")
	fx.setFile("/", wellKnownDoc)
	fx.setIndex("/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"alpha","type":"skill-md","description":"d",`+
			`"url":"/alpha.md","digest":"%s"}]}`, wellKnownSchemaURL, "sha256:"+strings.Repeat("0", 64)))

	_, err := Add(AddOptions{Source: fx.URL, Agents: []string{"opencode"}, Yes: true},
		wellKnownEnv(t, project))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "digest mismatch") {
		t.Fatalf("err = %v, want the digest mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(project, lockfile.FileName)); !os.IsNotExist(err) {
		t.Errorf("a rejected provider wrote a lock: %v", err)
	}
	if _, err := os.Stat(installedSkillDir(project, "alpha")); !os.IsNotExist(err) {
		t.Errorf("a rejected provider installed something: %v", err)
	}
}

func TestAddWellKnownLegacyRecordsNoDigestAndFetchesSelectedOnly(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	doc := "---\nname: alpha\ndescription: legacy skill\n---\nlegacy one\n"
	fx.setIndex("/.well-known/skills/index.json",
		`{"skills":[`+
			`{"name":"alpha","description":"a","files":["SKILL.md"]},`+
			`{"name":"beta","description":"b","files":["SKILL.md","missing.md"]}]}`)
	fx.setFile("/.well-known/skills/SKILL.md", doc)

	addWellKnown(t, project, fx.URL, AddOptions{Skills: []string{"alpha"}})

	entry := lockEntryOf(t, project, "alpha")
	if entry.SourceType != "well-known" || entry.WellKnownDigest != "" ||
		entry.SourceBaseURL != fx.URL || entry.SkillPath != "alpha/SKILL.md" {
		t.Errorf("entry = %+v, want a legacy well-known record", entry)
	}

	// The unselected broken skill did not block the install; selecting it does.
	_, err := Add(AddOptions{
		Source: fx.URL, Agents: []string{"opencode"}, Skills: []string{"beta"}, Yes: true,
	}, wellKnownEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "without all listed files") {
		t.Fatalf("err = %v, want the missing-file failure", err)
	}
}

func TestAddWellKnownScopedIndexDoesNotWiden(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.setIndex("/.well-known/agent-skills/index.json", fmt.Sprintf(
		`{"$schema":"%s","skills":[{"name":"root","type":"skill-md","description":"d",`+
			`"url":"/root.md","digest":"%s"}]}`, wellKnownSchemaURL, digestOf([]byte(wellKnownDoc))))
	fx.setFile("/root.md", wellKnownDoc)
	// A valid download at the scoped URL proves no download fallback happened.
	fx.setFile("/s/alpha", wellKnownDoc)

	_, err := Add(AddOptions{Source: fx.URL + "/s/alpha", Agents: []string{"opencode"}, Yes: true},
		wellKnownEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "scoped path") {
		t.Fatalf("err = %v, want the scope refusal", err)
	}
	if _, err := os.Stat(installedSkillDir(project, "root")); !os.IsNotExist(err) {
		t.Errorf("the scoped source widened to the root catalog: %v", err)
	}
}

func TestAddFallsThroughToDownloadWithoutAnIndex(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.setFile("/skill.md", downloadSkillDoc)

	addWellKnown(t, project, fx.URL+"/skill.md", AddOptions{})

	entry := lockEntryOf(t, project, "alpha")
	if entry.SourceType != "download" {
		t.Errorf("sourceType = %q, want the unchanged download path", entry.SourceType)
	}
}

func TestUpdateWellKnownUnreachableProviderFails(t *testing.T) {
	project := t.TempDir()
	fx := newWellKnownFixture(t)
	fx.publishV2(wellKnownDoc)
	addWellKnown(t, project, fx.URL, AddOptions{})
	fx.Close()

	rep, err := Update(UpdateOptions{Project: true, Yes: true}, wellKnownEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !rep.Failed() {
		t.Errorf("report = %+v, want the unreachable provider to fail", rep)
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Err == "" {
		t.Errorf("checks = %+v, want the fetch failure", rep.Checks)
	}
}
