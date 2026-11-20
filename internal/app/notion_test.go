package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/hash"
)

// notionSkillFile is the skill document name.
const notionSkillFile = "SKILL.md"

// notionPageHex is the 32-hex page id the Notion tests use.
const notionPageHex = "0123456789abcdef0123456789abcdef"

// notionPageUUID is the dashed form of notionPageHex.
const notionPageUUID = "01234567-89ab-cdef-0123-456789abcdef"

// notionPageLockURL is the URL a Notion page install records.
const notionPageLockURL = "https://www.notion.so/" + notionPageHex

// notionDoc is the SKILL.md body served by the page export.
const notionDoc = "---\nname: alpha\ndescription: notion skill\n---\nversion one\n"

// notionPackDoc is the second skill of a pack export.
const notionPackDoc = "---\nname: beta\ndescription: second notion skill\n---\nbeta one\n"

// notionTestFixture is the script of the fake ntn, mirroring testdata/notion/ntn.
type notionTestFixture struct {
	Workspace string                     `json:"workspace,omitempty"`
	Packs     json.RawMessage            `json:"packs,omitempty"`
	Plugins   map[string]json.RawMessage `json:"plugins,omitempty"`
	Skills    map[string]json.RawMessage `json:"skills,omitempty"`
	Errors    map[string]notionFailure   `json:"errors,omitempty"`
	Calls     string                     `json:"calls,omitempty"`
}

// notionFailure is one scripted error answer.
type notionFailure struct {
	Exit   int    `json:"exit"`
	Stderr string `json:"stderr"`
}

// notionServer serves one zip body and lets a test replace it.
type notionServer struct {
	*httptest.Server
	body *[]byte
}

func newNotionServer(t *testing.T, body []byte) *notionServer {
	t.Helper()
	s := &notionServer{body: &body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(*s.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *notionServer) set(body []byte) { *s.body = body }

// installNotionFake builds the fake ntn and puts it first on PATH, with fx as its fixture.
func installNotionFake(t *testing.T, fx notionTestFixture) {
	t.Helper()
	dir := t.TempDir()
	name := "ntn"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, "../../testdata/notion/ntn")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the fake ntn: %v\n%s", err, out)
	}
	data, err := json.Marshal(fx)
	if err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	fixturePath := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(fixturePath, data, 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_NTN_FIXTURE", fixturePath)
}

// notionPageFixture points the page id at one served export.
func notionPageFixture(url string) notionTestFixture {
	return notionTestFixture{Skills: map[string]json.RawMessage{
		notionPageUUID: json.RawMessage(`{"id":"` + notionPageUUID + `","version_id":"v1","url":"` +
			url + `"}`),
	}}
}

// notionZip builds a zip with the given entries.
func notionZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedKeys(files) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatalf("zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("closing the zip: %v", err)
	}
	return buf.Bytes()
}

// sortedKeys orders the entries of a fixture map for a deterministic archive.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// zipPageExport is one page export: a single top-level folder holding SKILL.md.
func zipPageExport(t *testing.T, content string) []byte {
	t.Helper()
	return notionZip(t, map[string]string{"page/" + notionSkillFile: content})
}

// zipPackExport is a pack export with two skills.
func zipPackExport(t *testing.T, alpha, beta string) []byte {
	t.Helper()
	return notionZip(t, map[string]string{
		"bundle/alpha/" + notionSkillFile: alpha,
		"bundle/beta/" + notionSkillFile:  beta,
	})
}

// notionPackJSON is a one-pack listing.
const notionPackJSON = `{"results":[{"id":"pack-1","name":"Alpha Pack","description":"first",` +
	`"version_id":"v1"}],"has_more":false,"next_cursor":null}`

func TestNotionPageAddCheckUpdateRestore(t *testing.T) {
	srv := newNotionServer(t, zipPageExport(t, notionDoc))
	installNotionFake(t, notionPageFixture(srv.URL+"/page.zip"))
	project := t.TempDir()
	env := downloadEnv(t, project)

	rep, err := Add(AddOptions{Source: notionPageLockURL, Agents: []string{"opencode"}, Yes: true}, env)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	if rep.Source != notionPageLockURL {
		t.Errorf("Source = %q, want the page URL", rep.Source)
	}
	installed := filepath.Join(project, ".agents", "skills", "alpha", notionSkillFile)
	raw, err := os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version one") {
		t.Fatalf("installed = %q, %v", raw, err)
	}

	entry := lockEntryOf(t, project, "alpha")
	if entry.SourceType != "download" || entry.Source != notionPageLockURL ||
		entry.SourceURL != notionPageLockURL {
		t.Errorf("entry = %+v, want the Notion page as a download source", entry)
	}
	if entry.SkillPath != notionSkillFile {
		t.Errorf("skillPath = %q, want %q", entry.SkillPath, notionSkillFile)
	}
	if want, err := hash.Dir(filepath.Dir(installed)); err != nil || entry.ComputedHash != want {
		t.Errorf("computedHash = %q, want %q (%v)", entry.ComputedHash, want, err)
	}

	if report, err := Check(context.Background(), CheckOptions{}, env); err != nil || report.Failed() {
		t.Fatalf("check = %+v, %v; want a clean run", report, err)
	}

	srv.set(zipPageExport(t, strings.ReplaceAll(notionDoc, "version one", "version two")))
	report, err := Check(context.Background(), CheckOptions{}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(report.Updates) != 1 || report.Updates[0] != "alpha" {
		t.Fatalf("check updates = %v, want alpha", report.Updates)
	}
	updated, err := Update(UpdateOptions{Project: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.Changed() {
		t.Fatalf("update did not reinstall: %+v", updated.Results)
	}
	raw, err = os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version two") {
		t.Fatalf("installed after update = %q, %v", raw, err)
	}
	if report, err := Check(context.Background(), CheckOptions{}, env); err != nil || report.Failed() {
		t.Fatalf("check after update = %+v, %v", report, err)
	}

	// The lock is enough to rebuild the project: ntn is asked for a fresh export.
	if err := os.RemoveAll(filepath.Join(project, ".agents", "skills", "alpha")); err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(RestoreOptions{}, env)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.Failed() {
		t.Fatalf("restore = %+v, want the skill rebuilt", restored)
	}
	raw, err = os.ReadFile(installed)
	if err != nil || !strings.Contains(string(raw), "version two") {
		t.Fatalf("restored = %q, %v", raw, err)
	}
}

func TestNotionPackSelectionAndLock(t *testing.T) {
	srv := newNotionServer(t, zipPackExport(t, notionDoc, notionPackDoc))
	installNotionFake(t, notionTestFixture{
		Packs: json.RawMessage(notionPackJSON),
		Plugins: map[string]json.RawMessage{
			"pack-1": json.RawMessage(`{"id":"pack-1","version_id":"v1","url":"` + srv.URL + `/pack.zip"}`),
		},
	})
	project := t.TempDir()
	env := downloadEnv(t, project)

	rep, err := Add(AddOptions{
		Source: "notion", Agents: []string{"opencode"}, Skills: []string{"beta"}, Yes: true,
	}, env)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "beta", notionSkillFile)); err != nil {
		t.Fatalf("beta was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("alpha was installed despite the --skill selection: %v", err)
	}

	packURL := "notion://packs/pack-1"
	entry := lockEntryOf(t, project, "beta")
	if entry.SourceType != "download" || entry.Source != "notion" || entry.SourceURL != packURL {
		t.Errorf("entry = %+v, want source notion and the pack URL", entry)
	}
	if entry.SkillPath != "packs/pack-1/beta/"+notionSkillFile {
		t.Errorf("skillPath = %q, want the path inside the export", entry.SkillPath)
	}

	if report, err := Check(context.Background(), CheckOptions{}, env); err != nil || report.Failed() {
		t.Fatalf("check = %+v, %v; want a clean run", report, err)
	}

	srv.set(zipPackExport(t, notionDoc, strings.ReplaceAll(notionPackDoc, "beta one", "beta two")))
	updated, err := Update(UpdateOptions{Project: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.Changed() {
		t.Fatalf("update did not reinstall: %+v", updated.Results)
	}
	raw, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "beta", notionSkillFile))
	if err != nil || !strings.Contains(string(raw), "beta two") {
		t.Fatalf("installed after update = %q, %v", raw, err)
	}
}

// packPrompter is a Prompter that also presents the Notion pack picker.
type packPrompter struct {
	*Scripted
	answer []string
	asked  []NotionPackChoice
	seenWS string
	used   bool
}

func (p *packPrompter) SelectNotionPacks(workspace string, packs []NotionPackChoice) ([]string, error) {
	p.used = true
	p.seenWS = workspace
	p.asked = packs
	return p.answer, nil
}

func TestNotionPackSelectionPrompt(t *testing.T) {
	srv := newNotionServer(t, zipPackExport(t, notionDoc, notionPackDoc))
	second := `{"id":"pack-2","version_id":"v1","url":"` + srv.URL + `/pack2.zip"}`
	installNotionFake(t, notionTestFixture{
		Workspace: "Mining Team",
		Packs: json.RawMessage(`{"results":[` +
			`{"id":"pack-1","name":"Alpha Pack","version_id":"v1"},` +
			`{"id":"pack-2","name":"Beta Pack","version_id":"v1"}` +
			`],"has_more":false,"next_cursor":null}`),
		Plugins: map[string]json.RawMessage{
			"pack-1": json.RawMessage(`{"id":"pack-1","version_id":"v1","url":"` + srv.URL + `/pack.zip"}`),
			"pack-2": json.RawMessage(second),
		},
	})
	project := t.TempDir()
	env := downloadEnv(t, project)
	prompter := &packPrompter{
		Scripted: NewScripted(t, Answers{
			Skills: []string{"alpha", "beta"}, Scope: ScopeProject, Proceed: true,
		}),
		answer: []string{"pack-2"},
	}

	rep, err := Add(AddOptions{Source: "notion", Agents: []string{"opencode"}}, env, prompter)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	if !prompter.used || len(prompter.asked) != 2 {
		t.Fatalf("the pack picker was not asked: %+v", prompter.asked)
	}
	if prompter.seenWS != "Mining Team" {
		t.Errorf("workspace = %q, want the workspace ntn reported", prompter.seenWS)
	}
	if entry := lockEntryOf(t, project, "beta"); entry.SourceURL != "notion://packs/pack-2" || entry.Source != "notion" {
		t.Errorf("entry = %+v, want the selected pack", entry)
	}
}

func TestNotionPacksEmpty(t *testing.T) {
	installNotionFake(t, notionTestFixture{Packs: json.RawMessage(`{"results":[],"has_more":false}`)})
	project := t.TempDir()
	_, err := Add(AddOptions{Source: "notion", Agents: []string{"opencode"}, Yes: true},
		downloadEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "no packs") {
		t.Fatalf("err = %v, want the empty-workspace error", err)
	}
}

func TestNotionMissingTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	project := t.TempDir()
	_, err := Add(AddOptions{Source: notionPageLockURL, Agents: []string{"opencode"}, Yes: true},
		downloadEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "ntn") ||
		!strings.Contains(err.Error(), "developers.notion.com/cli") {
		t.Fatalf("err = %v, want the missing-tool guidance", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills")); !os.IsNotExist(err) {
		t.Errorf("a run without ntn installed something: %v", err)
	}
}

func TestNotionExpiredPageKeepsInstall(t *testing.T) {
	srv := newNotionServer(t, zipPageExport(t, notionDoc))
	fx := notionPageFixture(srv.URL + "/page.zip")
	installNotionFake(t, fx)
	project := t.TempDir()
	env := downloadEnv(t, project)
	if _, err := Add(AddOptions{Source: notionPageLockURL, Agents: []string{"opencode"}, Yes: true},
		env); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// The page is deleted: ntn answers with its error on every call.
	expired := fx
	expired.Skills = nil
	expired.Errors = map[string]notionFailure{
		"/v1/ai/skills/" + notionPageUUID: {Exit: 1, Stderr: "page not found"},
	}
	data, err := json.Marshal(expired)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("FAKE_NTN_FIXTURE"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Update(UpdateOptions{Project: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("update = %+v, want the ntn failure", report)
	}
	joined := ""
	for _, check := range report.Checks {
		joined += check.Err
	}
	if !strings.Contains(joined, "page not found") {
		t.Errorf("checks = %+v, want the ntn error", report.Checks)
	}
	raw, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "alpha", notionSkillFile))
	if err != nil || !strings.Contains(string(raw), "version one") {
		t.Fatalf("the install was damaged: %q, %v", raw, err)
	}

	check, err := Check(context.Background(), CheckOptions{}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !check.Failed() {
		t.Errorf("check = %+v, want the expiry reported", check)
	}
}
