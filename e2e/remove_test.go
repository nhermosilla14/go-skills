package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2ERemoveUnmanagedGuard refuses a named removal from a non-dot agent directory the lock
// does not describe, and still removes the copy the lock tracks.
func TestE2ERemoveUnmanagedGuard(t *testing.T) {
	project := t.TempDir()
	hand := filepath.Join(project, "skills", "hand")
	if err := os.MkdirAll(hand, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hand, "SKILL.md"),
		[]byte("---\nname: hand\ndescription: hand written\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "remove", "hand", "-y")
	if res.exit != 1 {
		t.Fatalf("untracked remove exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "refusing to delete it") {
		t.Errorf("stderr misses the guard:\n%s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(hand, "SKILL.md")); err != nil {
		t.Errorf("the hand-written directory was deleted: %v", err)
	}

	res = run(t, project, "add", sourceDir(t), "-a", "openclaw", "-s", "flat-skill", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	tracked := filepath.Join(project, "skills", "flat-skill")
	if _, err := os.Stat(filepath.Join(tracked, "SKILL.md")); err != nil {
		t.Fatalf("the tracked copy was not installed in skills/: %v", err)
	}
	res = run(t, project, "remove", "flat-skill", "-y")
	if res.exit != 0 {
		t.Fatalf("tracked remove exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(tracked); !os.IsNotExist(err) {
		t.Errorf("the tracked copy survived removal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hand, "SKILL.md")); err != nil {
		t.Errorf("the guard damaged another skill: %v", err)
	}
}

// TestE2ERemoveOwnership walks the canonical copy and lock through two single-agent removals, then
// checks that --all never deletes a hand-written directory and that a copy-mode install is fully
// removed.
func TestE2ERemoveOwnership(t *testing.T) {
	project := t.TempDir()
	res := run(t, project, "add", sourceDir(t), "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	canonical := filepath.Join(project, ".agents", "skills", "flat-skill")
	link := filepath.Join(project, ".claude", "skills", "flat-skill")

	res = run(t, project, "remove", "flat-skill", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("remove claude-code exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("claude-code link still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
		t.Errorf("canonical copy removed while another agent links it: %v", err)
	}
	if _, ok := readLock(t, project).Skills["flat-skill"]; !ok {
		t.Fatal("lock entry removed while another agent still links the skill")
	}

	res = run(t, project, "remove", "flat-skill", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("remove opencode exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Errorf("canonical copy survived the last removal: %v", err)
	}
	if _, ok := readLock(t, project).Skills["flat-skill"]; ok {
		t.Fatal("lock entry survived the last removal")
	}

	hand := filepath.Join(project, ".agents", "skills", "hand")
	if err := os.MkdirAll(hand, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hand, "SKILL.md"),
		[]byte("---\nname: hand\ndescription: hand written skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = run(t, project, "remove", "--all")
	if res.exit != 0 {
		t.Fatalf("remove --all exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(hand, "SKILL.md")); err != nil {
		t.Errorf("--all deleted the hand-written skill: %v", err)
	}
	if !strings.Contains(res.stderr, "not recorded in the lock file") {
		t.Errorf("stderr does not report the untracked skill:\n%s", res.stderr)
	}
	if _, ok := readLock(t, project).Skills["nested-skill"]; ok {
		t.Error("--all left the tracked nested-skill lock entry")
	}

	res = run(t, project, "remove", "does-not-exist", "-y")
	if res.exit != 1 {
		t.Fatalf("unknown name exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "no installed skill or lock source matches") {
		t.Errorf("unknown name stderr = %q", res.stderr)
	}

	copied := t.TempDir()
	res = run(t, copied, "add", sourceDir(t), "-a", "opencode", "-a", "claude-code", "-y", "--copy")
	if res.exit != 0 {
		t.Fatalf("copy-mode add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	res = run(t, copied, "remove", "--all")
	if res.exit != 0 {
		t.Fatalf("copy-mode remove --all exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, path := range []string{
		filepath.Join(copied, ".agents", "skills", "flat-skill"),
		filepath.Join(copied, ".claude", "skills", "flat-skill"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s survived the copy-mode removal: %v", path, err)
		}
	}
	if len(readLock(t, copied).Skills) != 0 {
		t.Errorf("copy-mode lock = %+v, want every entry removed", readLock(t, copied).Skills)
	}
}
