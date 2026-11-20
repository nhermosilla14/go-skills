package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// downloadSkillDoc is a valid SKILL.md body used by the download tests.
const downloadSkillDoc = "---\nname: alpha\ndescription: test skill\n---\nversion one\n"

// downloadServer serves the body it holds at every path, and lets a test replace it to simulate an
// upstream change.
type downloadServer struct {
	*httptest.Server
	mu          sync.Mutex
	body        []byte
	contentType string
}

func newDownloadServer(t *testing.T, body []byte) *downloadServer {
	t.Helper()
	ds := &downloadServer{body: body, contentType: "text/markdown"}
	ds.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ds.mu.Lock()
		body, contentType := ds.body, ds.contentType
		ds.mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ds.Close)
	return ds
}

func (d *downloadServer) set(body []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.body = body
}

func (d *downloadServer) setContentType(contentType string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contentType = contentType
}

// downloadEnv isolates the temporary checkouts like the update tests do, so the e2e leak check of
// the shared temp directory never sees them.
func downloadEnv(t *testing.T, project string) Env {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	return addEnv(t, project)
}

// addDownload installs the served skill into project and returns the add report.
func addDownload(t *testing.T, project, source string) Report {
	t.Helper()
	rep, err := Add(AddOptions{Source: source, Agents: []string{"opencode"}, Yes: true},
		downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	return rep
}

// lockEntryOf loads the project lock and returns one entry.
func lockEntryOf(t *testing.T, project, name string) lockfile.Entry {
	t.Helper()
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := lock.Get(name)
	if !ok {
		t.Fatalf("lock has no %s entry: %+v", name, lock.Skills)
	}
	return entry
}

// installedSkillDir is the canonical directory of an installed skill.
func installedSkillDir(project, name string) string {
	return filepath.Join(project, ".agents", "skills", name)
}

func TestAddDownloadSingleFile(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	source := srv.URL + "/skill.md"

	addDownload(t, project, source)

	got, err := os.ReadFile(filepath.Join(installedSkillDir(project, "alpha"), "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md: %v", err)
	}
	if !strings.Contains(string(got), "version one") {
		t.Errorf("installed = %q, want the served body", got)
	}

	entry := lockEntryOf(t, project, "alpha")
	if entry.SourceType != "download" {
		t.Errorf("sourceType = %q, want download", entry.SourceType)
	}
	if entry.Source != source || entry.SourceURL != source {
		t.Errorf("source = %q, sourceUrl = %q, want %q", entry.Source, entry.SourceURL, source)
	}
	if entry.SkillPath != "SKILL.md" {
		t.Errorf("skillPath = %q, want SKILL.md", entry.SkillPath)
	}
	if entry.Commit != "" || entry.Ref != "" {
		t.Errorf("ref = %q, commit = %q, want none for a download", entry.Ref, entry.Commit)
	}
	want, err := hash.Dir(installedSkillDir(project, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.ComputedHash != want {
		t.Errorf("computedHash = %q, want %q", entry.ComputedHash, want)
	}
}

func TestAddDownloadArchive(t *testing.T) {
	bodies := map[string][]byte{"zip": zipSkillArchive(t), "tar.gz": tarGzSkillArchive(t)}
	for format, body := range bodies {
		t.Run(format, func(t *testing.T) {
			project := t.TempDir()
			srv := newDownloadServer(t, body)
			srv.setContentType("application/octet-stream")
			addDownload(t, project, srv.URL+"/skills")

			entry := lockEntryOf(t, project, "alpha")
			if entry.SourceType != "download" || entry.SkillPath != "alpha/SKILL.md" {
				t.Errorf("entry = %+v, want a download with the collapsed path", entry)
			}
			if _, err := os.Stat(filepath.Join(installedSkillDir(project, "alpha"), "reference.md")); err != nil {
				t.Errorf("supporting file missing: %v", err)
			}
		})
	}
}

// zipSkillArchive is a zip holding pkg/alpha as one skill with a supporting file.
func zipSkillArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"pkg/alpha/SKILL.md":     downloadSkillDoc,
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
	return buf.Bytes()
}

// tarGzSkillArchive is the gzip-wrapped tar of the same tree.
func tarGzSkillArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"pkg/alpha/SKILL.md":     downloadSkillDoc,
		"pkg/alpha/reference.md": "supporting file\n",
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
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

func TestAddDownloadDoesNotRecordASecretURL(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	rep := addDownload(t, project, srv.URL+"/skill.md?token=abc123")

	entry := lockEntryOf(t, project, "alpha")
	if entry.Source != "" || entry.SourceURL != "" {
		t.Errorf("source = %q, sourceUrl = %q, want both omitted", entry.Source, entry.SourceURL)
	}
	if entry.SourceType != "download" || entry.SkillPath != "SKILL.md" || entry.ComputedHash == "" {
		t.Errorf("entry = %+v, want the rest of the record", entry)
	}
	warned := strings.Join(rep.Warnings, "\n")
	if !strings.Contains(warned, "re-add") {
		t.Errorf("warnings = %q, want the re-add advice", warned)
	}
	if strings.Contains(warned, "abc123") || strings.Contains(rep.Source, "abc123") ||
		strings.Contains(rep.Source, "?") {
		t.Errorf("the query leaked: warnings = %q, source = %q", warned, rep.Source)
	}

	// Without a recorded URL the entry cannot be updated, and the report says why.
	update, err := Update(UpdateOptions{Project: true, Yes: true}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(update.Uncheckable) != 1 || update.Uncheckable[0].Reason != "No source recorded" {
		t.Errorf("uncheckable = %+v, want the missing source", update.Uncheckable)
	}
	if update.Failed() {
		t.Errorf("update failed an entry it cannot check: %+v", update.Results)
	}
}

func TestUpdateDownloadReinstallsChangedContent(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	addDownload(t, project, srv.URL+"/skill.md")

	srv.set([]byte(strings.ReplaceAll(downloadSkillDoc, "version one", "version two")))
	rep, err := Update(UpdateOptions{Project: true, Yes: true}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want the changed skill updated", rep.Results)
	}
	if !rep.Changed() || rep.Failed() {
		t.Errorf("report = %+v", rep)
	}
	got, err := os.ReadFile(filepath.Join(installedSkillDir(project, "alpha"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "version two") {
		t.Errorf("installed = %q, want the new content", got)
	}
	want, err := hash.Dir(installedSkillDir(project, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	entry := lockEntryOf(t, project, "alpha")
	if entry.SourceType != "download" || entry.ComputedHash != want {
		t.Errorf("entry = %+v, want the download and the new hash %q", entry, want)
	}
}

func TestUpdateDownloadKeepsUnchangedContent(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	addDownload(t, project, srv.URL+"/skill.md")

	rep, err := Update(UpdateOptions{Project: true, Yes: true}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateCurrent {
		t.Fatalf("results = %+v, want the skill current", rep.Results)
	}
	if rep.Changed() {
		t.Error("update reinstalled an unchanged download")
	}
}

func TestUpdateDownloadReinstallsAMissingFolder(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	addDownload(t, project, srv.URL+"/skill.md")
	if err := os.RemoveAll(installedSkillDir(project, "alpha")); err != nil {
		t.Fatal(err)
	}

	rep, err := Update(UpdateOptions{Project: true, Yes: true}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want the missing folder restored", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(installedSkillDir(project, "alpha"), "SKILL.md")); err != nil {
		t.Errorf("the skill was not reinstalled: %v", err)
	}
}

func TestCheckDownloadReportsChangedContent(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	addDownload(t, project, srv.URL+"/skill.md")

	rep, err := Check(context.Background(), CheckOptions{}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Updates) != 0 || len(rep.Failures) != 0 {
		t.Fatalf("report = %+v, want a current skill", rep)
	}

	srv.set([]byte(strings.ReplaceAll(downloadSkillDoc, "version one", "version two")))
	rep, err = Check(context.Background(), CheckOptions{}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Updates) != 1 || rep.Updates[0] != "alpha" {
		t.Fatalf("updates = %v, want alpha", rep.Updates)
	}
	if !rep.Failed() {
		t.Error("check found an update but does not fail")
	}
}

func TestRestoreDownloadsAgain(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	addDownload(t, project, srv.URL+"/skill.md")
	if err := os.RemoveAll(installedSkillDir(project, "alpha")); err != nil {
		t.Fatal(err)
	}

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Unrestorable) != 0 {
		t.Fatalf("unrestorable = %+v, want the download restored", rep.Unrestorable)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the skill restored", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(installedSkillDir(project, "alpha"), "SKILL.md")); err != nil {
		t.Errorf("the skill was not restored: %v", err)
	}
}

func TestRestoreUnreachableDownloadFails(t *testing.T) {
	project := t.TempDir()
	srv := newDownloadServer(t, []byte(downloadSkillDoc))
	addDownload(t, project, srv.URL+"/skill.md")
	if err := os.RemoveAll(installedSkillDir(project, "alpha")); err != nil {
		t.Fatal(err)
	}
	srv.Close()

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}}, downloadEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !rep.Failed() {
		t.Errorf("report = %+v, want the unreachable URL to fail", rep)
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Err == "" {
		t.Errorf("checks = %+v, want the fetch failure", rep.Checks)
	}
}
