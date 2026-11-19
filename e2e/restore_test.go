package e2e

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// snapshotTree records the files and links below root, keyed by their relative path.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = "link:" + target
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", root, err)
	}
	return out
}

// restoreDelete removes the installed trees but keeps the lock.
func restoreDelete(t *testing.T, project string) {
	t.Helper()
	for _, dir := range []string{".agents", ".claude"} {
		if err := os.RemoveAll(filepath.Join(project, dir)); err != nil {
			t.Fatal(err)
		}
	}
}

// TestE2ERestoreFromLock installs from a local git fixture, deletes the installed trees and rebuilds
// them from the lock: the content, hashes and relative links must come back, even after the tag
// moves, and --force and --dry-run must behave.
func TestE2ERestoreFromLock(t *testing.T) {
	fx := newGitFixture(t)
	before := cloneTempDirs(t)

	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	agentsSnapshot := snapshotTree(t, filepath.Join(project, ".agents"))
	claudeSnapshot := snapshotTree(t, filepath.Join(project, ".claude"))
	lockBefore, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}

	restoreDelete(t, project)
	res = run(t, project, "install", "-a", "opencode", "-a", "claude-code")
	if res.exit != 0 {
		t.Fatalf("install exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("install stdout = %q, want the human output on stderr", res.stdout)
	}
	if !strings.Contains(res.stderr, "Restored 2, relinked 0, skipped 0, failed 0") {
		t.Errorf("stderr misses the summary:\n%s", res.stderr)
	}
	if got := snapshotTree(t, filepath.Join(project, ".agents")); !reflect.DeepEqual(got, agentsSnapshot) {
		t.Errorf(".agents differs after restore:\n got %v\nwant %v", got, agentsSnapshot)
	}
	if got := snapshotTree(t, filepath.Join(project, ".claude")); !reflect.DeepEqual(got, claudeSnapshot) {
		t.Errorf(".claude differs after restore:\n got %v\nwant %v", got, claudeSnapshot)
	}
	target, err := os.Readlink(filepath.Join(project, ".claude", "skills", "alpha"))
	if err != nil {
		t.Fatalf("alpha link missing: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Errorf("link target = %q, want a relative target", target)
	}
	lockAfter, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(lockBefore) != string(lockAfter) {
		t.Error("restore rewrote an already correct lock")
	}
	if alpha := readLock(t, project).Skills["alpha"]; alpha.ComputedHash != oracleHash(t, filepath.Join(project, ".agents", "skills", "alpha")) {
		t.Errorf("alpha computedHash = %q, want the restored content hash", alpha.ComputedHash)
	}

	// Move the tag upstream: the recorded commit still wins.
	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)
	restoreDelete(t, project)
	res = run(t, project, "install", "-a", "opencode", "-a", "claude-code")
	if res.exit != 0 {
		t.Fatalf("pinned install exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err := os.ReadFile(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "version one") {
		t.Errorf("restored alpha = %q, want the locked commit despite the moved tag", raw)
	}
	if alpha := readLock(t, project).Skills["alpha"]; alpha.Commit != fx.first {
		t.Errorf("commit = %q, want %s kept", alpha.Commit, fx.first)
	}

	// --force reinstalls the correct content again.
	res = run(t, project, "install", "-a", "opencode", "-a", "claude-code", "--force")
	if res.exit != 0 {
		t.Fatalf("forced install exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "Restored 2") {
		t.Errorf("forced install stderr misses the reinstall:\n%s", res.stderr)
	}

	// --dry-run plans the same work and writes nothing.
	restoreDelete(t, project)
	res = run(t, project, "install", "-a", "opencode", "-a", "claude-code", "--dry-run")
	if res.exit != 0 {
		t.Fatalf("dry-run exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{"would restore alpha", "would restore beta"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("dry-run stderr misses %q:\n%s", want, res.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".agents")); !os.IsNotExist(err) {
		t.Errorf("a dry run recreated .agents: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude")); !os.IsNotExist(err) {
		t.Errorf("a dry run recreated .claude: %v", err)
	}
	assertNoNewTempDirs(t, before)
}

// TestE2ERestoreFrozen exercises the frozen-lockfile rules: a clean lock restores under CI, a
// mutated skill fails without touching anything, a version 1 empty hash fails with its message and
// is repaired by normal mode, and --no-frozen-lockfile repairs in CI.
func TestE2ERestoreFrozen(t *testing.T) {
	fx := newGitFixture(t)
	tmpBefore := cloneTempDirs(t)
	project := t.TempDir()
	agents := []string{"-a", "opencode", "-a", "claude-code"}
	if res := run(t, project, append([]string{"add", fx.url + "#v1", "-y"}, agents...)...); res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	lockPath := filepath.Join(project, "skills-lock.json")

	res := runEnv(t, project, []string{"CI=1"}, append([]string{"install"}, agents...)...)
	if res.exit != 0 {
		t.Fatalf("frozen install of a clean lock exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "Restored 0, relinked 0, skipped 2, failed 0") {
		t.Errorf("frozen install stderr misses the summary:\n%s", res.stderr)
	}

	// A mutated skill fails frozen and leaves the tree and the lock untouched.
	alpha := filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")
	if err := os.WriteFile(alpha, []byte("---\nname: alpha\ndescription: d\n---\nmutated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, project)
	beforeLock, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	res = runEnv(t, project, []string{"CI=1"}, append([]string{"install"}, agents...)...)
	if res.exit != 1 {
		t.Fatalf("frozen install of drifted content exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{
		"Cannot install with --frozen-lockfile",
		"alpha has changed since skills-lock.json was written",
		"Run again with --no-frozen-lockfile to update it.",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr misses %q:\n%s", want, res.stderr)
		}
	}
	if after := snapshotTree(t, project); !reflect.DeepEqual(after, before) {
		t.Errorf("a failed frozen run changed the project:\n got %v\nwant %v", after, before)
	}
	afterLock, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !afterLock.ModTime().Equal(beforeLock.ModTime()) {
		t.Errorf("lock mtime = %v, want %v", afterLock.ModTime(), beforeLock.ModTime())
	}

	// --no-frozen-lockfile repairs the drift even in CI.
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	res = runEnv(t, project, []string{"CI=1"},
		append([]string{"install", "--no-frozen-lockfile"}, agents...)...)
	if res.exit != 0 {
		t.Fatalf("non-frozen install in CI exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if raw, err := os.ReadFile(alpha); err != nil || !strings.Contains(string(raw), "version one") {
		t.Errorf("alpha was not repaired: %q, %v", raw, err)
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != string(lockBytes) {
		t.Errorf("repairing the same content rewrote the lock: %v", err)
	}

	// A version 1 lock whose migrated hash is empty fails frozen and is repaired by normal mode.
	entry := readLock(t, project).Skills["alpha"]
	v1 := fmt.Sprintf(`{
  "version": 1,
  "skills": {
    "alpha": {"source": %q, "sourceUrl": %q, "sourceType": "git", "commit": %q,
      "skillPath": %q, "computedHash": ""}
  }
}
`, entry.Source, entry.SourceURL, entry.Commit, entry.SkillPath)
	if err := os.WriteFile(lockPath, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeLock, err = os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	res = runEnv(t, project, []string{"CI=1"}, append([]string{"install"}, agents...)...)
	if res.exit != 1 {
		t.Fatalf("frozen install of a v1 lock exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{
		"Cannot install with --frozen-lockfile",
		"alpha has no content hash in skills-lock.json",
		"migrated from version 1",
	} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr misses %q:\n%s", want, res.stderr)
		}
	}
	if after, err := os.ReadFile(lockPath); err != nil || string(after) != v1 {
		t.Errorf("a failed frozen run rewrote the v1 lock: %v", err)
	}
	afterLock, err = os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !afterLock.ModTime().Equal(beforeLock.ModTime()) {
		t.Errorf("lock mtime = %v, want %v", afterLock.ModTime(), beforeLock.ModTime())
	}

	res = runEnv(t, project, []string{"CI=1"},
		append([]string{"install", "--no-frozen-lockfile"}, agents...)...)
	if res.exit != 0 {
		t.Fatalf("normal install of a v1 lock exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	repaired := readLock(t, project)
	if repaired.Version != 2 {
		t.Errorf("lock version = %d, want 2", repaired.Version)
	}
	want := oracleHash(t, filepath.Join(project, ".agents", "skills", "alpha"))
	if got := repaired.Skills["alpha"].ComputedHash; got != want {
		t.Errorf("alpha computedHash = %q, want the refreshed hash %q", got, want)
	}
	assertNoNewTempDirs(t, tmpBefore)
}

func TestE2ERestoreMissingLock(t *testing.T) {
	project := t.TempDir()
	res := run(t, project, "install")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "skills-lock.json") {
		t.Errorf("stderr = %q, want the missing-lock message", res.stderr)
	}
}

// TestE2ERestoreCopyMode puts a copy-mode entry back as plain directories, never linking.
func TestE2ERestoreCopyMode(t *testing.T) {
	fx := newGitFixture(t)
	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "-a", "opencode", "-a", "claude-code", "--copy", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if lock := readLock(t, project); lock.Skills["alpha"].InstallMode != "copy" {
		t.Fatalf("installMode = %q, want copy", lock.Skills["alpha"].InstallMode)
	}
	restoreDelete(t, project)

	res = run(t, project, "install", "-a", "opencode", "-a", "claude-code")
	if res.exit != 0 {
		t.Fatalf("install exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, dir := range []string{
		filepath.Join(project, ".agents", "skills", "alpha"),
		filepath.Join(project, ".claude", "skills", "alpha"),
	} {
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatalf("%s was not restored: %v", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			t.Errorf("%s is not a plain directory", dir)
		}
	}
	if lock := readLock(t, project); lock.Skills["alpha"].InstallMode != "copy" {
		t.Errorf("installMode = %q, want copy preserved", lock.Skills["alpha"].InstallMode)
	}
}
