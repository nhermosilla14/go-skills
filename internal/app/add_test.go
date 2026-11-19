package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

func newSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	must := func(p, content string) {
		t.Helper()
		full := filepath.Join(src, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("skills/flat/SKILL.md", "---\nname: flat\ndescription: flat skill\n---\n")
	must("skills/broken/SKILL.md", "---\ndescription: no name\n---\n")
	return src
}

func testEnv(project string) Env {
	return Env{
		WorkDir: project,
		Now:     func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

func TestAddListInstallsNothing(t *testing.T) {
	project := t.TempDir()
	rep, err := Add(AddOptions{Source: newSource(t), List: true}, testEnv(project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Listed) != 1 || rep.Listed[0].Name != "flat" {
		t.Errorf("Listed = %+v, want flat", rep.Listed)
	}
	if len(rep.Warnings) != 1 {
		t.Errorf("Warnings = %v, want one for broken skill", rep.Warnings)
	}
	if _, err := os.Stat(filepath.Join(project, lockfile.FileName)); !os.IsNotExist(err) {
		t.Errorf("--list wrote a lock: %v", err)
	}
}

func TestAddRequiresYes(t *testing.T) {
	_, err := Add(AddOptions{Source: newSource(t), Agents: []string{"opencode"}}, testEnv(t.TempDir()))
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("err = %v, want ErrPromptRequired", err)
	}
}

func TestAddRejectsInvalidAgent(t *testing.T) {
	_, err := Add(AddOptions{Source: newSource(t), Agents: []string{"claude_code"}, Yes: true}, testEnv(t.TempDir()))
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestAddInstallsAndRecords(t *testing.T) {
	project := t.TempDir()
	src := newSource(t)
	rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode", "claude-code"}, Yes: true}, testEnv(project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results failed: %+v", rep.Results)
	}
	if len(rep.Results) != 2 || rep.Results[0].Status != install.Installed {
		t.Errorf("results = %+v", rep.Results)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e, ok := lock.Get("flat")
	if !ok {
		t.Fatalf("lock has no flat entry: %+v", lock.Skills)
	}
	if e.SourceType != "local" {
		t.Errorf("entry = %+v", e)
	}
	if e.SkillPath != "" {
		t.Errorf("skillPath = %q, want omitted for local sources", e.SkillPath)
	}
	if e.ComputedHash == "" {
		t.Error("computedHash empty")
	}
	if rep.LockPath == "" {
		t.Error("LockPath not reported")
	}
}

func TestLocalSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX absolute paths")
	}
	tests := map[string]struct{ project, source, want string }{
		"inside":  {"/p", "/p/src/x", "./src/x"},
		"root":    {"/p", "/p", "."},
		"outside": {"/p", "/q/x", "/q/x"},
		"sibling": {"/p", "/pp/x", "/pp/x"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := localSource(tc.project, tc.source); got != tc.want {
				t.Errorf("localSource = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAddRecordsInstallModeOnlyWhenCopyIsForced(t *testing.T) {
	src := newSource(t)

	forced := t.TempDir()
	if _, err := Add(AddOptions{Source: src, Agents: []string{"opencode", "claude-code"}, Yes: true, Copy: true}, testEnv(forced)); err != nil {
		t.Fatalf("Add --copy: %v", err)
	}
	lock, err := lockfile.Load(filepath.Join(forced, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := lock.Get("flat"); e.InstallMode != "copy" {
		t.Errorf("installMode = %q, want copy", e.InstallMode)
	}

	derived := t.TempDir()
	if _, err := Add(AddOptions{Source: src, Agents: []string{"claude-code"}, Yes: true, Copy: true}, testEnv(derived)); err != nil {
		t.Fatalf("Add copy by default: %v", err)
	}
	lock, err = lockfile.Load(filepath.Join(derived, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if e, _ := lock.Get("flat"); e.InstallMode != "" {
		t.Errorf("installMode = %q, want omitted when copy is the default", e.InstallMode)
	}
}

func TestAddRenameRewritesInstalledName(t *testing.T) {
	src := t.TempDir()
	writeSourceFile(t, src, "skills/alpha/SKILL.md", "---\nname: Alpha Skill\ndescription: d\n---\nbody\n")
	before, err := os.ReadFile(filepath.Join(src, "skills/alpha/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	rep, err := Add(AddOptions{
		Source: src, Agents: []string{"claude-code"}, Yes: true, Rename: "alpha-new",
	}, testEnv(project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results failed: %+v", rep.Results)
	}
	installed, err := os.ReadFile(filepath.Join(project, ".claude", "skills", "alpha-new", "SKILL.md"))
	if err != nil {
		t.Fatalf("installed SKILL.md: %v", err)
	}
	if want := "---\nname: alpha-new\ndescription: d\n---\nbody\n"; string(installed) != want {
		t.Errorf("installed = %q, want %q", installed, want)
	}
	after, err := os.ReadFile(filepath.Join(src, "skills/alpha/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("the source SKILL.md was modified")
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := lock.Get("alpha-new")
	if !ok {
		t.Fatalf("lock has no alpha-new entry: %+v", lock.Skills)
	}
	if entry.SkillName != "Alpha Skill" {
		t.Errorf("skillName = %q, want the upstream name", entry.SkillName)
	}
}

func TestAddRenameRestrictions(t *testing.T) {
	one := t.TempDir()
	writeSourceFile(t, one, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\n")
	two := t.TempDir()
	writeSourceFile(t, two, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\n")
	writeSourceFile(t, two, "skills/beta/SKILL.md", "---\nname: beta\ndescription: d\n---\n")

	tests := []struct {
		name   string
		source string
		rename string
		want   string
	}{
		{"several skills", two, "renamed", "installs one skill under a new name"},
		{"unsafe name", one, "Bad Name", "--rename must be a skill name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Add(AddOptions{
				Source: tc.source, Agents: []string{"claude-code"}, Yes: true, Rename: tc.rename,
			}, testEnv(t.TempDir()))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestAddSelectsByNormalizedName(t *testing.T) {
	src := t.TempDir()
	writeSourceFile(t, src, "skills/conv/SKILL.md", "---\nname: Convex Best Practices\ndescription: d\n---\n")
	writeSourceFile(t, src, "skills/other/SKILL.md", "---\nname: Other\ndescription: d\n---\n")

	project := t.TempDir()
	rep, err := Add(AddOptions{
		Source: src, Agents: []string{"opencode"}, Skills: []string{"convex-best-practices"}, Yes: true,
	}, testEnv(project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Skill != "convex-best-practices" {
		t.Errorf("results = %+v, want only the normalized match", rep.Results)
	}
}

func writeSourceFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
