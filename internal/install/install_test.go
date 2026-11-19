package install

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

func newSkill(t *testing.T, project, name string) skill.Skill {
	t.Helper()
	dir := filepath.Join(project, "src", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return skill.Skill{Name: name, Dir: dir, RelDir: "src/" + name}
}

func allAgents(t *testing.T) []agents.Agent {
	t.Helper()
	selected, invalid := agents.Expand([]string{"*"})
	if len(invalid) != 0 || len(selected) != 2 {
		t.Fatalf("Expand(*) = %v, invalid %v", selected, invalid)
	}
	return selected
}

func TestPlanSymlinkForTwoDirs(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	entries := Plan(project, []skill.Skill{s}, allAgents(t), false, "")

	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Mode != ModeSymlink {
		t.Errorf("Mode = %s, want symlink", e.Mode)
	}
	wantCanonical := filepath.Join(project, ".agents", "skills", "alpha")
	if e.Canonical != wantCanonical {
		t.Errorf("Canonical = %s, want %s", e.Canonical, wantCanonical)
	}
	var paths []string
	for _, tg := range e.Targets {
		paths = append(paths, tg.Path)
	}
	want := []string{
		filepath.Join(project, ".agents", "skills", "alpha"),
		filepath.Join(project, ".claude", "skills", "alpha"),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("target paths = %v, want %v", paths, want)
	}
}

func TestPlanCopyForOneDir(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"claude-code"})
	entries := Plan(project, []skill.Skill{s}, selected, false, "")
	e := entries[0]
	if e.Mode != ModeCopy || e.Canonical != "" {
		t.Errorf("Mode = %s, Canonical = %q; want copy without canonical", e.Mode, e.Canonical)
	}
	if got, want := e.Targets[0].Path, filepath.Join(project, ".claude", "skills", "alpha"); got != want {
		t.Errorf("target = %s, want %s", got, want)
	}
}

func TestPlanForceCopyAndSortedByName(t *testing.T) {
	project := t.TempDir()
	skills := []skill.Skill{newSkill(t, project, "zeta"), newSkill(t, project, "alpha")}
	entries := Plan(project, skills, allAgents(t), true, "")
	if entries[0].Name != "alpha" || entries[1].Name != "zeta" {
		t.Errorf("order = %s, %s; want alpha, zeta", entries[0].Name, entries[1].Name)
	}
	if entries[0].Mode != ModeCopy {
		t.Errorf("forced copy: Mode = %s", entries[0].Mode)
	}
	if !entries[0].ForcedCopy {
		t.Error("ForcedCopy = false, want true: --copy overrode symlink mode")
	}
}

func TestPlanForcedCopyWhenCopyIsAlreadyTheDefault(t *testing.T) {
	project := t.TempDir()
	selected, _ := agents.Expand([]string{"claude-code"})
	entries := Plan(project, []skill.Skill{newSkill(t, project, "alpha")}, selected, true, "")
	if entries[0].ForcedCopy {
		t.Error("ForcedCopy = true, want false: a single agent directory is copy by default")
	}
}

func TestApplySymlinkModeLinksRelatively(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX relative link check")
	}
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	results := Apply(project, Plan(project, []skill.Skill{s}, allAgents(t), false, ""))

	for _, r := range results {
		if r.Status != Installed {
			t.Fatalf("result %+v, want installed", r)
		}
	}
	canonical := filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("canonical copy missing: %v", err)
	}
	link := filepath.Join(project, ".claude", "skills", "alpha")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("not a symlink: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Errorf("link target %s is absolute", target)
	}
	if target != filepath.Join("..", "..", ".agents", "skills", "alpha") {
		t.Errorf("link target = %s", target)
	}
	if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
		t.Errorf("link does not resolve: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "scripts.txt")); err != nil {
		t.Errorf("payload file not copied: %v", err)
	}
}

func TestApplyCopyModeWritesIndependentFiles(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"claude-code"})
	results := Apply(project, Plan(project, []skill.Skill{s}, selected, false, ""))
	if len(results) != 1 || results[0].Status != Installed || results[0].Mode != ModeCopy {
		t.Fatalf("results = %+v", results)
	}
	dest := filepath.Join(project, ".claude", "skills", "alpha")
	info, err := os.Lstat(dest)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("destination is not a plain directory: %v", err)
	}
}

func TestApplyReplacesOldInstallAndDropsStaleFiles(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	stale := filepath.Join(project, ".claude", "skills", "alpha", "old.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, _ := agents.Expand([]string{"claude-code"})
	Apply(project, Plan(project, []skill.Skill{s}, selected, false, ""))
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file survived reinstall: %v", err)
	}
}

func TestApplySkipsWhenSourceIsDestination(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	// Make the source itself the copy destination.
	s.Dir = filepath.Join(project, ".claude", "skills", "alpha")
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, _ := agents.Expand([]string{"claude-code"})
	results := Apply(project, Plan(project, []skill.Skill{s}, selected, false, ""))
	if results[0].Status != Skipped {
		t.Fatalf("status = %s, want skipped (%s)", results[0].Status, results[0].Message)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "SKILL.md")); err != nil {
		t.Errorf("source was deleted: %v", err)
	}
}

func TestApplyRefusesCategoryDirectory(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"claude-code"})
	entries := Plan(project, []skill.Skill{s}, selected, false, "")
	entries[0].Targets[0].Path = filepath.Join(project, ".claude", "skills", ".system")
	results := Apply(project, entries)
	if results[0].Status != Failed || !strings.Contains(results[0].Message, "category") {
		t.Errorf("result = %+v, want failed category error", results[0])
	}
}
