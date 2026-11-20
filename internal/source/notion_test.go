package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// notionPageHex is the 32-hex page id the tests use.
const notionPageHex = "0123456789abcdef0123456789abcdef"

// notionPageUUID is the dashed form of notionPageHex.
const notionPageUUID = "01234567-89ab-cdef-0123-456789abcdef"

// notionPackFixture is a packs listing with one pack.
const notionPackFixture = `{"results":[` +
	`{"id":"pack-1","name":"Alpha Pack","description":"first pack","version_id":"v1"}` +
	`],"has_more":false,"next_cursor":null}`

// ntnFixture is the script of the fake ntn, mirroring testdata/notion/ntn.
type ntnFixture struct {
	Workspace     string                     `json:"workspace,omitempty"`
	Packs         json.RawMessage            `json:"packs,omitempty"`
	PacksByCursor map[string]json.RawMessage `json:"packs_by_cursor,omitempty"`
	Plugins       map[string]json.RawMessage `json:"plugins,omitempty"`
	Skills        map[string]json.RawMessage `json:"skills,omitempty"`
	Errors        map[string]ntnFailure      `json:"errors,omitempty"`
	Calls         string                     `json:"calls,omitempty"`
	SleepMS       int                        `json:"sleep_ms,omitempty"`
	Stdout        string                     `json:"stdout,omitempty"`
}

// ntnFailure is one scripted error answer.
type ntnFailure struct {
	Exit   int    `json:"exit"`
	Stderr string `json:"stderr"`
}

// installFakeNTN builds the fake CLI and puts it first on PATH.
func installFakeNTN(t *testing.T, fx ntnFixture) string {
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
	return fixturePath
}

// readCalls returns the argument lists the fake recorded.
func readCalls(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the calls log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// notionArchiveServer serves one zip body at every path and lets a test replace it.
func notionArchiveServer(t *testing.T, body []byte) (*httptest.Server, func([]byte)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, func(next []byte) { body = next }
}

// pageZip is one skill page export: a single top-level directory holding SKILL.md.
func pageZip(t *testing.T, content string) []byte {
	t.Helper()
	return zipBytes(t, []archiveEntry{
		{name: "alpha/" + skillFile, body: []byte(content)},
	})
}

// packZip is a pack export holding two skills under one top-level directory.
func packZip(t *testing.T) []byte {
	t.Helper()
	return zipBytes(t, []archiveEntry{
		{name: "bundle/alpha/" + skillFile, body: []byte(skillDoc)},
		{name: "bundle/beta/" + skillFile, body: []byte(
			"---\nname: beta\ndescription: second skill\n---\nbeta body\n")},
	})
}

func TestParseNotionPageURL(t *testing.T) {
	tests := []struct {
		name   string
		arg    string
		pageID string
		ok     bool
	}{
		{"bare hex", "https://www.notion.so/" + notionPageHex, notionPageUUID, true},
		{"dashed uuid", "https://www.notion.so/" + notionPageUUID, notionPageUUID, true},
		{"title slug", "https://www.notion.so/My-Page-" + notionPageHex, notionPageUUID, true},
		{"escaped slug", "https://www.notion.so/My%20Page-" + notionPageHex, notionPageUUID, true},
		{"query and fragment", "https://notion.so/Page-" + notionPageHex + "?pvs=4#block", notionPageUUID, true},
		{"notion.com", "https://www.notion.com/" + notionPageHex, notionPageUUID, true},
		{"uppercase hex", "https://www.notion.so/" + strings.ToUpper(notionPageHex), notionPageUUID, true},
		{"no page id", "https://www.notion.so/workspace", "", false},
		{"notion.site", "https://example.notion.site/" + notionPageHex, "", false},
		{"other host", "https://example.com/" + notionPageHex, "", false},
		{"deceptive host", "https://notion.so.example.com/" + notionPageHex, "", false},
		{"not a url", notionPageHex, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pageID, ok := parseNotionPageURL(tt.arg)
			if ok != tt.ok || pageID != tt.pageID {
				t.Errorf("parseNotionPageURL(%q) = %q, %v; want %q, %v",
					tt.arg, pageID, ok, tt.pageID, tt.ok)
			}
		})
	}
}

func TestParseNotionArguments(t *testing.T) {
	src, err := Parse("NoTiOn")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if src.Kind != KindNotion || src.Source != "notion" {
		t.Errorf("Parse(notion) = %+v, want the pack browser", src)
	}

	src, err = Parse("https://www.notion.so/Title-" + notionPageHex + "?pvs=4")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "https://www.notion.so/" + notionPageHex
	if src.Kind != KindDownload || src.Source != want || src.SourceURL != want {
		t.Errorf("page = %+v, want the canonical lock URL %q", src, want)
	}
	if src.NotionPageID != notionPageUUID || !src.IsNotion() {
		t.Errorf("page id = %q, want %q", src.NotionPageID, notionPageUUID)
	}
	if !src.Recordable() {
		t.Errorf("a Notion page must be recordable, got %+v", src)
	}

	packURL := NotionPackURL("pack-1")
	src, err = Parse(packURL)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if src.Kind != KindDownload || src.Source != packURL || src.SourceURL != packURL {
		t.Errorf("pack = %+v, want the pack lock URL", src)
	}
	if src.NotionPackID != "pack-1" {
		t.Errorf("pack id = %q, want pack-1", src.NotionPackID)
	}

	// Everything else on a Notion host keeps the ordinary download handling.
	src, err = Parse("https://www.notion.so/workspace")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if src.Kind != KindDownload || src.IsNotion() {
		t.Errorf("unmatched Notion URL = %+v, want a plain download", src)
	}
}

func TestNotionPackLockURL(t *testing.T) {
	got := NotionPackURL("pack/with space")
	if got != "notion://packs/pack%2Fwith%20space" {
		t.Errorf("NotionPackURL = %q", got)
	}
	src, err := Parse(got)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if src.NotionPackID != "pack/with space" {
		t.Errorf("round trip id = %q, want the original", src.NotionPackID)
	}
	if NotionPackDir("Pack One") != "packs/pack-one" {
		t.Errorf("NotionPackDir = %q, want the sanitized id", NotionPackDir("Pack One"))
	}
}

func TestNotionMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := RequireNTN(); err == nil || !strings.Contains(err.Error(), "ntn") {
		t.Fatalf("err = %v, want the missing-tool error", err)
	}
	src, err := Parse("https://www.notion.so/" + notionPageHex)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	_, err = Acquire(context.Background(), src, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "developers.notion.com/cli") {
		t.Fatalf("Acquire err = %v, want the install guidance", err)
	}
}

func TestRunNTNTimeout(t *testing.T) {
	installFakeNTN(t, ntnFixture{SleepMS: 5000})
	previous := ntnTimeout
	ntnTimeout = 100 * time.Millisecond
	t.Cleanup(func() { ntnTimeout = previous })

	_, err := runNTN(context.Background(), notionAPIArgs("/v1/users/me"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want the timeout", err)
	}
}

func TestRunNTNStderrMapping(t *testing.T) {
	const secret = "ntn_abcdef123456"
	installFakeNTN(t, ntnFixture{Errors: map[string]ntnFailure{
		"/v1/ai/skills/" + notionPageUUID: {
			Exit:   1,
			Stderr: "\\u001b[31mpage not found\\u001b[0m token=abc123 " + secret + " ",
		},
	}})

	_, err := notionSkillDirectory(context.Background(), notionPageUUID)
	if err == nil {
		t.Fatal("a failing ntn call was accepted")
	}
	text := err.Error()
	if !strings.Contains(text, "page not found") || !strings.Contains(text, "ntn api failed") {
		t.Errorf("err = %q, want the stderr mapped into the message", text)
	}
	for _, leaked := range []string{secret, "abc123", "\x1b"} {
		if strings.Contains(text, leaked) {
			t.Errorf("err = %q leaks %q", text, leaked)
		}
	}
}

func TestRunNTNOutputLimit(t *testing.T) {
	installFakeNTN(t, ntnFixture{Stdout: strings.Repeat("x", 512)})
	previous := ntnMaxOutput
	ntnMaxOutput = 128
	t.Cleanup(func() { ntnMaxOutput = previous })

	_, err := runNTN(context.Background(), notionAPIArgs("/v1/users/me"))
	if err == nil || !strings.Contains(err.Error(), "output exceeded 10 MiB") {
		t.Fatalf("err = %v, want the output limit", err)
	}
}

func TestFetchNotionPacksCommandsAndPagination(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls.log")
	installFakeNTN(t, ntnFixture{
		Calls: calls,
		PacksByCursor: map[string]json.RawMessage{
			"": json.RawMessage(`{"results":[{"id":"pack-1","name":"Alpha Pack","version_id":"v1"}],` +
				`"has_more":true,"next_cursor":"c1"}`),
			"c1": json.RawMessage(`{"results":[{"id":"pack-2","name":"Beta Pack","version_id":"v2"}],` +
				`"has_more":false,"next_cursor":null}`),
		},
	})

	packs, err := FetchNotionPacks(context.Background())
	if err != nil {
		t.Fatalf("FetchNotionPacks: %v", err)
	}
	if len(packs) != 2 || packs[0].ID != "pack-1" || packs[1].ID != "pack-2" {
		t.Errorf("packs = %+v, want both pages sorted by name", packs)
	}
	got := readCalls(t, calls)
	want := []string{
		"api /v1/ai/plugins page_size==100 --notion-version 2026-03-11",
		"api /v1/ai/plugins page_size==100 start_cursor==c1 --notion-version 2026-03-11",
	}
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFetchNotionPacksRejectsAnInvalidCursor(t *testing.T) {
	installFakeNTN(t, ntnFixture{Packs: json.RawMessage(
		`{"results":[],"has_more":true,"next_cursor":null}`)})
	_, err := FetchNotionPacks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid cursor") {
		t.Fatalf("err = %v, want the invalid-cursor rejection", err)
	}
}

func TestParseNotionPackPage(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		packs   int
		wantErr string
	}{
		{name: "one pack", body: notionPackFixture, packs: 1},
		{
			name:  "nameless pack is skipped",
			body:  `{"results":[{"id":"p","name":"  ","version_id":"v"}],"has_more":false}`,
			packs: 0,
		},
		{
			name:  "description is sanitized",
			body:  `{"results":[{"id":"p","name":"Pack","description":"a\u001b[31mb","version_id":"v"}],"has_more":false}`,
			packs: 1,
		},
		{name: "missing has_more", body: `{"results":[]}`, wantErr: "missing has_more"},
		{name: "results is not an array", body: `{"results":{},"has_more":false}`, wantErr: "invalid"},
		{name: "missing id", body: `{"results":[{"name":"Pack","version_id":"v"}],"has_more":false}`,
			wantErr: "missing pack.id"},
		{name: "not json", body: `not json`, wantErr: "invalid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := parseNotionPackPage(tt.body)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if len(page.packs) != tt.packs {
				t.Fatalf("packs = %+v, want %d", page.packs, tt.packs)
			}
		})
	}

	page, err := parseNotionPackPage(notionPackFixture)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if page.packs[0].Name != "Alpha Pack" || page.packs[0].Description != "first pack" {
		t.Errorf("pack = %+v, want the sanitized fields", page.packs[0])
	}
}

func TestFetchNotionPacksNoPacks(t *testing.T) {
	installFakeNTN(t, ntnFixture{Packs: json.RawMessage(`{"results":[],"has_more":false}`)})
	packs, err := FetchNotionPacks(context.Background())
	if err != nil {
		t.Fatalf("FetchNotionPacks: %v", err)
	}
	if len(packs) != 0 {
		t.Errorf("packs = %+v, want none", packs)
	}
}

func TestNotionDirectoryCommands(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls.log")
	installFakeNTN(t, ntnFixture{
		Calls: calls,
		Skills: map[string]json.RawMessage{
			notionPageUUID: json.RawMessage(`{"id":"` + notionPageUUID +
				`","version_id":"v1","url":"https://example.com/page.zip"}`),
		},
		Plugins: map[string]json.RawMessage{
			"pack-1": json.RawMessage(`{"id":"pack-1","version_id":"v1","url":"https://example.com/pack.zip"}`),
		},
	})

	page, err := notionSkillDirectory(context.Background(), notionPageUUID)
	if err != nil || page.URL != "https://example.com/page.zip" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	pack := NotionPack{ID: "pack-1", Name: "Alpha Pack", VersionID: "v1"}
	if _, err := notionPackDirectory(context.Background(), pack); err != nil {
		t.Fatalf("pack directory: %v", err)
	}
	changed := pack
	changed.VersionID = "v2"
	if _, err := notionPackDirectory(context.Background(), changed); err == nil ||
		!strings.Contains(err.Error(), "changed") {
		t.Fatalf("err = %v, want the pack-changed rejection", err)
	}

	got := readCalls(t, calls)
	want := []string{
		"api /v1/ai/skills/" + notionPageUUID + " --notion-version 2026-03-11",
		"api /v1/ai/plugins/pack-1 --notion-version 2026-03-11",
		"api /v1/ai/plugins/pack-1 --notion-version 2026-03-11",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNotionWorkspaceName(t *testing.T) {
	installFakeNTN(t, ntnFixture{Workspace: "Mining Team"})
	if name := NotionWorkspaceName(context.Background()); name != "Mining Team" {
		t.Errorf("workspace = %q, want Mining Team", name)
	}
}

func TestNotionLimitFallbacks(t *testing.T) {
	for _, name := range []string{envDownloadMaxBytes, envExtractMaxBytes, envExtractMaxFiles} {
		t.Setenv(name, "")
	}
	got := notionDownloadLimits()
	want := limits{
		downloadBytes: NotionDownloadMaxBytes,
		extractBytes:  NotionExtractMaxBytes,
		extractFiles:  NotionExtractMaxFiles,
	}
	if got != want {
		t.Errorf("limits = %+v, want the Notion fallback %+v", got, want)
	}
	if want.downloadBytes != 50<<20 || want.extractBytes != 100<<20 || want.extractFiles != 5000 {
		t.Errorf("the documented Notion limits changed: %+v", want)
	}

	t.Setenv(envDownloadMaxBytes, "60000000")
	t.Setenv(envExtractMaxBytes, "200000000")
	t.Setenv(envExtractMaxFiles, "9000")
	raised := notionDownloadLimits()
	if raised.downloadBytes != 60000000 || raised.extractBytes != 200000000 || raised.extractFiles != 9000 {
		t.Errorf("limits = %+v, want the raised environment values", raised)
	}

	t.Setenv(envExtractMaxFiles, "10")
	if notionDownloadLimits().extractFiles != NotionExtractMaxFiles {
		t.Errorf("a lowered SKILLS_EXTRACT_MAX_FILES was honoured")
	}
}

func TestAcquireNotionPage(t *testing.T) {
	srv, setBody := notionArchiveServer(t, pageZip(t, skillDoc))
	installFakeNTN(t, ntnFixture{Skills: map[string]json.RawMessage{
		notionPageUUID: json.RawMessage(`{"id":"` + notionPageUUID + `","version_id":"v1","url":"` +
			srv.URL + `/page.zip"}`),
	}})
	src, err := Parse("https://www.notion.so/" + notionPageHex)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tree, err := Acquire(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tree.Dir, skillFile)); err != nil {
		t.Fatalf("the exported page has no root SKILL.md: %v", err)
	}
	if tree.SkillPath != "" {
		t.Errorf("SkillPath = %q, want discovery to resolve the archive", tree.SkillPath)
	}

	// A later export is a different body: the pipeline must not cache anything.
	setBody(pageZip(t, strings.ReplaceAll(skillDoc, "body", "new body")))
	tree, err = Acquire(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(tree.Dir, skillFile))
	if err != nil || !strings.Contains(string(raw), "new body") {
		t.Fatalf("exported = %q, %v", raw, err)
	}
}

func TestAcquireNotionPack(t *testing.T) {
	srv, _ := notionArchiveServer(t, packZip(t))
	installFakeNTN(t, ntnFixture{
		Packs: json.RawMessage(notionPackFixture),
		Plugins: map[string]json.RawMessage{
			"pack-1": json.RawMessage(`{"id":"pack-1","version_id":"v1","url":"` + srv.URL + `/pack.zip"}`),
		},
	})
	src, err := Parse(NotionPackURL("pack-1"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	tree, err := Acquire(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	skillDir := filepath.Join(tree.Dir, filepath.FromSlash(NotionPackDir("pack-1")), "beta")
	if _, err := os.Stat(filepath.Join(skillDir, skillFile)); err != nil {
		t.Fatalf("the exported pack has no beta skill: %v", err)
	}

	// An unknown pack id fails instead of downloading something else.
	src.NotionPackID = "pack-404"
	if _, err := Acquire(context.Background(), src, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "No matching Notion packs") {
		t.Fatalf("err = %v, want the no-matching-packs error", err)
	}
}
