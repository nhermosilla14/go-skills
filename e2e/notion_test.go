package e2e

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// notionPageHex is the 32-hex page id the Notion end-to-end tests use.
const notionPageHex = "0123456789abcdef0123456789abcdef"

// notionPageUUID is the dashed form of notionPageHex.
const notionPageUUID = "01234567-89ab-cdef-0123-456789abcdef"

// notionPageLockURL is the URL a Notion page install records.
const notionPageLockURL = "https://www.notion.so/" + notionPageHex

// notionPageDoc is the page export body.
const notionPageDoc = "---\nname: alpha\ndescription: notion page skill\n---\nversion one\n"

// notionBetaDoc is the second skill of the pack export.
const notionBetaDoc = "---\nname: beta\ndescription: pack skill\n---\nbeta one\n"

// notionFixture is the script of the fake ntn, mirroring testdata/notion/ntn.
type notionFixture struct {
	Workspace string                     `json:"workspace,omitempty"`
	Packs     json.RawMessage            `json:"packs,omitempty"`
	Plugins   map[string]json.RawMessage `json:"plugins,omitempty"`
	Skills    map[string]json.RawMessage `json:"skills,omitempty"`
	Errors    map[string]notionFailure   `json:"errors,omitempty"`
}

// notionFailure is one scripted error answer.
type notionFailure struct {
	Exit   int    `json:"exit"`
	Stderr string `json:"stderr"`
}

// notionArchive serves one zip body and lets a test replace it.
type notionArchive struct {
	*httptest.Server
	body *[]byte
}

func newNotionArchive(t *testing.T, body []byte) *notionArchive {
	t.Helper()
	a := &notionArchive{body: &body}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(*a.body)
	}))
	t.Cleanup(a.Close)
	return a
}

func (a *notionArchive) set(body []byte) { *a.body = body }

// notionZip builds a zip with the given entries, in map order.
func notionZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing the zip: %v", err)
	}
	return buf.Bytes()
}

// pageExport is one page export: a single top-level folder holding SKILL.md.
func pageExport(t *testing.T, content string) []byte {
	t.Helper()
	return notionZip(t, map[string]string{"page/SKILL.md": content})
}

// packExport is a pack export with two skills.
func packExport(t *testing.T, beta string) []byte {
	t.Helper()
	return notionZip(t, map[string]string{
		"bundle/alpha/SKILL.md": notionPageDoc,
		"bundle/beta/SKILL.md":  beta,
	})
}

// onePackJSON is a one-pack workspace listing.
const onePackJSON = `{"results":[{"id":"pack-1","name":"Alpha Pack","version_id":"v1"}],` +
	`"has_more":false,"next_cursor":null}`

// buildFakeNTN builds the fake CLI and returns the directory holding it.
func buildFakeNTN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "ntn"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, "../testdata/notion/ntn")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the fake ntn: %v\n%s", err, out)
	}
	return dir
}

// runNotion runs the binary with the fake ntn configured.
func runNotion(t *testing.T, project, ntnDir string, fx notionFixture, args ...string) result {
	t.Helper()
	data, err := json.Marshal(fx)
	if err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	env := withoutHost(withoutCI(os.Environ()))
	kept := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if key, _, _ := splitEnv(kv); key == "PATH" || key == "FAKE_NTN_FIXTURE" {
			continue
		}
		kept = append(kept, kv)
	}
	fixture := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = project
	cmd.Env = append(kept,
		"PATH="+ntnDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_NTN_FIXTURE="+fixture,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exit := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), exit: exit}
}

// runWithoutNTN runs the binary with a PATH that holds no ntn.
func runWithoutNTN(t *testing.T, project string, args ...string) result {
	t.Helper()
	env := withoutHost(withoutCI(os.Environ()))
	kept := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if key, _, _ := splitEnv(kv); key == "PATH" {
			continue
		}
		kept = append(kept, kv)
	}
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = project
	cmd.Env = append(kept, "PATH="+t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), exit: exit}
}

func TestE2ENotionPage(t *testing.T) {
	archive := newNotionArchive(t, pageExport(t, notionPageDoc))
	ntn := buildFakeNTN(t)
	fx := notionFixture{Skills: map[string]json.RawMessage{
		notionPageUUID: json.RawMessage(`{"id":"` + notionPageUUID +
			`","version_id":"v1","url":"` + archive.URL + `/page.zip"}`),
	}}
	project := t.TempDir()

	res := runNotion(t, project, ntn, fx, "add", notionPageLockURL, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	installed := filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")
	raw, err := os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version one") {
		t.Fatalf("installed = %q, %v", raw, err)
	}

	entry, ok := readLock(t, project).Skills["alpha"]
	if !ok {
		t.Fatal("the lock has no alpha entry")
	}
	if entry.SourceType != "download" || entry.Source != notionPageLockURL ||
		entry.SourceURL != notionPageLockURL {
		t.Errorf("entry = %+v, want the Notion page recorded as a download", entry)
	}
	if entry.SkillPath != "SKILL.md" {
		t.Errorf("skillPath = %q, want SKILL.md", entry.SkillPath)
	}
	if want := oracleHash(t, filepath.Dir(installed)); entry.ComputedHash != want {
		t.Errorf("computedHash = %s, oracle says %s", entry.ComputedHash, want)
	}

	if res := runNotion(t, project, ntn, fx, "check"); res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
}

func TestE2ENotionUpdate(t *testing.T) {
	archive := newNotionArchive(t, pageExport(t, notionPageDoc))
	ntn := buildFakeNTN(t)
	fx := notionFixture{Skills: map[string]json.RawMessage{
		notionPageUUID: json.RawMessage(`{"id":"` + notionPageUUID +
			`","version_id":"v1","url":"` + archive.URL + `/page.zip"}`),
	}}
	project := t.TempDir()
	if res := runNotion(t, project, ntn, fx, "add", notionPageLockURL, "-a", "opencode", "-y"); res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	installed := filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")

	archive.set(pageExport(t, strings.ReplaceAll(notionPageDoc, "version one", "version two")))
	res := runNotion(t, project, ntn, fx, "check")
	if res.exit != 1 || !strings.Contains(res.stdout, "alpha has an update") {
		t.Fatalf("check exit = %d, stdout = %q, want the update", res.exit, res.stdout)
	}
	if res := runNotion(t, project, ntn, fx, "update", "-p"); res.exit != 0 {
		t.Fatalf("update exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err := os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version two") {
		t.Fatalf("installed after update = %q, %v", raw, err)
	}
	if res := runNotion(t, project, ntn, fx, "check"); res.exit != 0 {
		t.Fatalf("check after update exit = %d\nstdout:\n%s", res.exit, res.stdout)
	}

	// A deleted page fails the export and must leave the install alone.
	expired := notionFixture{Errors: map[string]notionFailure{
		"/v1/ai/skills/" + notionPageUUID: {Exit: 1, Stderr: "page not found"},
	}}
	res = runNotion(t, project, ntn, expired, "update", "-p")
	if res.exit != 1 {
		t.Fatalf("update of an expired page exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err = os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version two") {
		t.Fatalf("the install was damaged: %q, %v", raw, err)
	}
	// check names the ntn failure, so the user knows why the re-export cannot run.
	res = runNotion(t, project, ntn, expired, "check")
	if res.exit != 1 || !strings.Contains(res.stdout, "page not found") {
		t.Fatalf("check of an expired page exit = %d, stdout = %q, want the ntn error", res.exit, res.stdout)
	}
}

func TestE2ENotionPack(t *testing.T) {
	archive := newNotionArchive(t, packExport(t, notionBetaDoc))
	ntn := buildFakeNTN(t)
	fx := notionFixture{
		Packs: json.RawMessage(onePackJSON),
		Plugins: map[string]json.RawMessage{
			"pack-1": json.RawMessage(`{"id":"pack-1","version_id":"v1","url":"` + archive.URL + `/pack.zip"}`),
		},
	}
	project := t.TempDir()

	res := runNotion(t, project, ntn, fx, "add", "notion", "-a", "opencode", "-y", "-s", "beta")
	if res.exit != 0 {
		t.Fatalf("add exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	installed := filepath.Join(project, ".agents", "skills", "beta", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("beta was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha was installed despite -s beta: %v", err)
	}

	packURL := "notion://packs/pack-1"
	entry, ok := readLock(t, project).Skills["beta"]
	if !ok {
		t.Fatal("the lock has no beta entry")
	}
	if entry.SourceType != "download" || entry.Source != "notion" || entry.SourceURL != packURL {
		t.Errorf("entry = %+v, want source notion and the pack URL", entry)
	}
	if entry.SkillPath != "packs/pack-1/beta/SKILL.md" {
		t.Errorf("skillPath = %q, want the path inside the export", entry.SkillPath)
	}
	if res := runNotion(t, project, ntn, fx, "check"); res.exit != 0 {
		t.Fatalf("check exit = %d\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}

	archive.set(packExport(t, strings.ReplaceAll(notionBetaDoc, "beta one", "beta two")))
	if res := runNotion(t, project, ntn, fx, "update", "-p"); res.exit != 0 {
		t.Fatalf("update exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err := os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "beta two") {
		t.Fatalf("installed after update = %q, %v", raw, err)
	}
}

func TestE2ENotionMissingTool(t *testing.T) {
	project := t.TempDir()
	res := runWithoutNTN(t, project, "add", notionPageLockURL, "-a", "opencode", "-y")
	if res.exit != 1 {
		t.Fatalf("add exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "ntn") || !strings.Contains(res.stderr, "developers.notion.com/cli") {
		t.Errorf("stderr = %q, want the install guidance", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("a run without ntn wrote a lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills")); !os.IsNotExist(err) {
		t.Errorf("a run without ntn installed something: %v", err)
	}
}
