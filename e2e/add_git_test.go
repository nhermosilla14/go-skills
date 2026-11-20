package e2e

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// gitFixture is a bare repository served through a file:// URL, with a tag on its first commit.
type gitFixture struct {
	url    string
	bare   string
	first  string
	second string
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFixtureFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newGitFixture(t *testing.T) gitFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("file:// fixtures use POSIX paths")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	work := t.TempDir()
	runGit(t, work, "init", "-q", "-b", "main")
	writeFixtureFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nversion one\n")
	writeFixtureFile(t, work, "skills/beta/SKILL.md", "---\nname: beta\ndescription: d\n---\nversion one\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "one")
	first := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "tag", "v1")

	writeFixtureFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nversion two\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "two")
	second := runGit(t, work, "rev-parse", "HEAD")

	bare := filepath.Join(t.TempDir(), "repo.git")
	runGit(t, "", "clone", "-q", "--bare", work, bare)
	runGit(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(bare)}
	return gitFixture{url: u.String(), bare: bare, first: first, second: second}
}

// cloneTempDirs lists the temporary checkouts alive under the system temp directory.
func cloneTempDirs(t *testing.T) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "skills-*"))
	if err != nil {
		t.Fatal(err)
	}
	set := make(map[string]bool, len(matches))
	for _, m := range matches {
		set[m] = true
	}
	return set
}

func assertNoNewTempDirs(t *testing.T, before map[string]bool) {
	t.Helper()
	for dir := range cloneTempDirs(t) {
		if !before[dir] {
			t.Errorf("temporary checkout left behind: %s", dir)
		}
	}
}

func TestE2EGitSourcePinnedCommit(t *testing.T) {
	fx := newGitFixture(t)
	before := cloneTempDirs(t)

	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	installed := filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(got), "version one") {
		t.Errorf("installed %q, want the tagged commit", got)
	}

	lock := readLock(t, project)
	entry, ok := lock.Skills["alpha"]
	if !ok {
		t.Fatalf("lock has no alpha entry: %+v", lock.Skills)
	}
	if entry.Source != fx.url || entry.SourceType != "git" {
		t.Errorf("source = %q, sourceType = %q", entry.Source, entry.SourceType)
	}
	if entry.Ref != "v1" || entry.Commit != fx.first {
		t.Errorf("ref = %q, commit = %q, want v1 and %s", entry.Ref, entry.Commit, fx.first)
	}
	if entry.SkillPath != "skills/alpha/SKILL.md" {
		t.Errorf("skillPath = %q, want the repo-relative SKILL.md", entry.SkillPath)
	}
	if entry.SourceURL != entry.Source {
		t.Errorf("sourceUrl = %q, want the fetch URL %q for a generic git source", entry.SourceURL, entry.Source)
	}
	assertNoNewTempDirs(t, before)

	// Move the tag and install the recorded commit: the pin must win over the moved tag.
	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)
	pinned := t.TempDir()
	res = run(t, pinned, "add", fx.url+"#"+fx.first, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("pinned exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	got, err = os.ReadFile(filepath.Join(pinned, ".agents", "skills", "alpha", "SKILL.md"))
	if err != nil {
		t.Fatalf("pinned SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(got), "version one") {
		t.Errorf("pinned install = %q, want the recorded commit despite the moved tag", got)
	}
	pinnedLock := readLock(t, pinned)
	pinnedEntry := pinnedLock.Skills["alpha"]
	if pinnedEntry.Commit != fx.first || pinnedEntry.Ref != fx.first {
		t.Errorf("pinned ref = %q, commit = %q, want %s", pinnedEntry.Ref, pinnedEntry.Commit, fx.first)
	}
}

func TestE2EListRemoteSource(t *testing.T) {
	fx := newGitFixture(t)
	before := cloneTempDirs(t)

	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "--list")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "alpha\t") || !strings.Contains(res.stdout, "skills/alpha/SKILL.md") {
		t.Errorf("list output = %q, want the skill and its path", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("--list wrote a lock: %v", err)
	}
	assertNoNewTempDirs(t, before)
}

func TestE2EGitSkillSelector(t *testing.T) {
	fx := newGitFixture(t)
	project := t.TempDir()

	res := run(t, project, "add", fx.url+"#v1@alpha", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	entries, err := os.ReadDir(filepath.Join(project, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "alpha" {
		t.Errorf("installed %v, want only the selected alpha skill", entries)
	}
}

func TestE2EGitCloneFailureCleansTemp(t *testing.T) {
	fx := newGitFixture(t)
	before := cloneTempDirs(t)

	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#does-not-exist", "--list")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "failed to clone") {
		t.Errorf("stderr = %q, want a clone failure", res.stderr)
	}
	assertNoNewTempDirs(t, before)
}

func TestE2EGitMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH handling differs on Windows")
	}
	project := t.TempDir()
	res := runEnv(t, project, []string{"PATH=" + t.TempDir()}, "add", "owner/repo#main", "--list")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "git is required") {
		t.Errorf("stderr = %q, want a missing-git message", res.stderr)
	}
}

func TestE2ENetworkGitHubSource(t *testing.T) {
	if os.Getenv("SKILLS_E2E_NETWORK") != "1" {
		t.Skip("set SKILLS_E2E_NETWORK=1 to run tests that need the network")
	}
	project := t.TempDir()
	res := run(t, project, "add", "vercel-labs/agent-skills", "--list")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if strings.TrimSpace(res.stdout) == "" {
		t.Error("no skills listed from the public repository")
	}
}
