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

// updateFixture is a bare repository whose second commit changes one skill, moves another and
// leaves a third untouched. The v1 tag starts on the first commit.
type updateFixture struct {
	url    string
	bare   string
	first  string
	second string
}

func newUpdateFixture(t *testing.T) updateFixture {
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
	writeFixtureFile(t, work, "skills/moved/SKILL.md", "---\nname: moved\ndescription: d\n---\nversion one\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "one")
	first := runGit(t, work, "rev-parse", "HEAD")
	runGit(t, work, "tag", "v1")

	writeFixtureFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nversion two\n")
	writeFixtureFile(t, work, "skills/relocated/SKILL.md", "---\nname: moved\ndescription: d\n---\nversion one\n")
	if err := os.RemoveAll(filepath.Join(work, "skills", "moved")); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", "-A")
	runGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "two")
	second := runGit(t, work, "rev-parse", "HEAD")

	bare := filepath.Join(t.TempDir(), "repo.git")
	runGit(t, "", "clone", "-q", "--bare", work, bare)
	runGit(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(bare)}
	return updateFixture{url: u.String(), bare: bare, first: first, second: second}
}

// TestE2EUpdateRoundTrip installs from a local git fixture, advances its tag and updates three
// projects: a symlinked round trip with a relocated skill, a renamed skill and a copied install.
func TestE2EUpdateRoundTrip(t *testing.T) {
	fx := newUpdateFixture(t)
	before := cloneTempDirs(t)

	roundTrip := t.TempDir()
	res := run(t, roundTrip, "add", fx.url+"#v1", "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	renamed := t.TempDir()
	res = run(t, renamed, "add", fx.url+"#v1", "--skill", "alpha", "--rename", "alpha-new",
		"-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("renamed add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	copied := t.TempDir()
	res = run(t, copied, "add", fx.url+"#v1", "-a", "opencode", "-a", "claude-code", "--copy", "-y")
	if res.exit != 0 {
		t.Fatalf("copy add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}

	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)

	res = run(t, roundTrip, "update", "-p")
	if res.exit != 0 {
		t.Fatalf("update exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("update stdout = %q, want the human output on stderr", res.stdout)
	}
	if !strings.Contains(res.stderr, "Checking for skill updates…") {
		t.Errorf("stderr misses the header:\n%s", res.stderr)
	}
	alphaPath := filepath.Join(roundTrip, ".agents", "skills", "alpha")
	raw, err := os.ReadFile(filepath.Join(alphaPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("alpha SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(raw), "version two") {
		t.Errorf("alpha content = %q, want the advanced commit", raw)
	}
	lock := readLock(t, roundTrip)
	alpha := lock.Skills["alpha"]
	if alpha.Commit != fx.second {
		t.Errorf("alpha commit = %q, want %s", alpha.Commit, fx.second)
	}
	if alpha.ComputedHash != oracleHash(t, alphaPath) {
		t.Errorf("alpha computedHash = %q, want the installed hash", alpha.ComputedHash)
	}
	if alpha.SourceType != "git" || alpha.Ref != "v1" || alpha.SourceURL != fx.url {
		t.Errorf("alpha entry = %+v, want source, ref and sourceUrl preserved", alpha)
	}
	moved := lock.Skills["moved"]
	if moved.SkillPath != "skills/relocated/SKILL.md" {
		t.Errorf("moved skillPath = %q, want the relocated path", moved.SkillPath)
	}
	if beta := lock.Skills["beta"]; beta.Commit != fx.second {
		t.Errorf("beta commit = %q, want the fetched HEAD backfilled", beta.Commit)
	}
	betaRaw, err := os.ReadFile(filepath.Join(roundTrip, ".agents", "skills", "beta", "SKILL.md"))
	if err != nil {
		t.Fatalf("beta SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(betaRaw), "version one") {
		t.Errorf("beta content = %q, want the unchanged skill left alone", betaRaw)
	}
	link := filepath.Join(roundTrip, ".claude", "skills", "alpha")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("claude link was not preserved: %v", err)
	}

	res = run(t, roundTrip, "update", "-p")
	if res.exit != 0 || !strings.Contains(res.stderr, "✓ All project skills are up to date") {
		t.Fatalf("second update exit = %d, want up to date\nstderr:\n%s", res.exit, res.stderr)
	}
	res = run(t, roundTrip, "check")
	if res.exit != 0 || !strings.Contains(res.stdout, "✓ All project skills are up to date") {
		t.Errorf("check exit = %d after an update\nstdout:\n%s\nstderr:\n%s",
			res.exit, res.stdout, res.stderr)
	}

	res = run(t, renamed, "update", "alpha-new")
	if res.exit != 0 {
		t.Fatalf("renamed update exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	renamedLock := readLock(t, renamed)
	entry, ok := renamedLock.Skills["alpha-new"]
	if !ok {
		t.Fatalf("renamed lock lost its installed name: %+v", renamedLock.Skills)
	}
	if entry.SkillName != "alpha" {
		t.Errorf("skillName = %q, want the upstream name preserved", entry.SkillName)
	}
	if _, upstream := renamedLock.Skills["alpha"]; upstream {
		t.Error("lock gained the upstream name as a key")
	}
	renamedRaw, err := os.ReadFile(filepath.Join(renamed, ".agents", "skills", "alpha-new", "SKILL.md"))
	if err != nil {
		t.Fatalf("renamed SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(renamedRaw), "name: alpha-new") || !strings.Contains(string(renamedRaw), "version two") {
		t.Errorf("renamed content = %q, want the installed name and the new body", renamedRaw)
	}
	if info, err := os.Lstat(filepath.Join(renamed, ".claude", "skills", "alpha-new")); err != nil ||
		info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("renamed link was not preserved: %v", err)
	}

	res = run(t, copied, "update", "-p")
	if res.exit != 0 {
		t.Fatalf("copy update exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	copiedLock := readLock(t, copied)
	if entry := copiedLock.Skills["alpha"]; entry.InstallMode != "copy" {
		t.Errorf("installMode = %q, want copy preserved", entry.InstallMode)
	}
	copiedPath := filepath.Join(copied, ".claude", "skills", "alpha")
	if info, err := os.Lstat(copiedPath); err != nil || !info.IsDir() {
		t.Fatalf("copied skill is not a directory: %v", err)
	}
	copiedRaw, err := os.ReadFile(filepath.Join(copiedPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("copied SKILL.md missing: %v", err)
	}
	if !strings.Contains(string(copiedRaw), "version two") {
		t.Errorf("copied content = %q, want the new content", copiedRaw)
	}
	assertNoNewTempDirs(t, before)
}

// TestE2EUpdateUnreachableSource fails the whole run when a source cannot be fetched.
func TestE2EUpdateUnreachableSource(t *testing.T) {
	fx := newUpdateFixture(t)
	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if err := os.RemoveAll(fx.bare); err != nil {
		t.Fatal(err)
	}

	res = run(t, project, "update", "-p")
	if res.exit != 1 {
		t.Fatalf("update exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "✗ Failed to check skills from "+fx.url) {
		t.Errorf("stderr misses the failed source:\n%s", res.stderr)
	}
}

// TestE2EUpdateScopeAndNames covers the non-interactive scope choice and the name filter.
func TestE2EUpdateScopeAndNames(t *testing.T) {
	fx := newUpdateFixture(t)
	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}

	res = runGlobal(t, project, t.TempDir(), "update", "-g")
	if res.exit != 0 || !strings.Contains(res.stderr, "No global skills tracked in lock file.") {
		t.Fatalf("update -g exit = %d, stderr = %q", res.exit, res.stderr)
	}

	res = run(t, project, "update", "does-not-exist")
	if res.exit != 1 || !strings.Contains(res.stderr, "no installed skill matches this name") {
		t.Fatalf("unmatched update exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "global scope is not supported yet") {
		t.Errorf("named update still claims a global limitation:\n%s", res.stderr)
	}

	empty := t.TempDir()
	res = run(t, empty, "update")
	if res.exit != 0 || !strings.Contains(res.stderr, "No project skills to update.") {
		t.Fatalf("empty update exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
}
