package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
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

func TestAddDetectsAgentsWhenNoneIsNamed(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: home, Cwd: project, Vars: map[string]string{},
		ApplicationsDir: t.TempDir(), EtcDir: t.TempDir(),
	}
	rep, err := Add(AddOptions{Source: newSource(t), Yes: true}, env)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rep.Results {
		got[r.Agent] = true
	}
	if !got["cursor"] || !got["deepagents"] {
		t.Errorf("results = %v, want the detected agent and the hidden universal ones", got)
	}
	if got["eve"] || got["replit"] || got["antigravity"] {
		t.Errorf("results = %v, want no Eve, replit or Antigravity", got)
	}
	if len(rep.Results) != 20 {
		t.Errorf("results = %d, want 20 (cursor plus the 19 other auto-added universal agents)", len(rep.Results))
	}
}

func TestAddExplicitAgentsBypassDetection(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: home, Cwd: project, Vars: map[string]string{},
		ApplicationsDir: t.TempDir(), EtcDir: t.TempDir(),
	}
	rep, err := Add(AddOptions{Source: newSource(t), Agents: []string{"eve"}, Yes: true}, env)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Agent != "eve" {
		t.Fatalf("results = %+v, want only eve", rep.Results)
	}
	if rep.Results[0].Status != install.Installed {
		t.Errorf("eve = %+v, want installed", rep.Results[0])
	}
	if _, err := os.Stat(filepath.Join(project, "agent", "skills", "flat.md")); err != nil {
		t.Errorf("eve flat file missing: %v", err)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := lock.Get("flat"); !ok || e.ComputedHash == "" {
		t.Errorf("lock entry = %+v, want a hash for the flat file", e)
	}
}

func TestAddNothingDetectedInstallsEveryAgent(t *testing.T) {
	project := t.TempDir()
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: t.TempDir(), Cwd: project, Vars: map[string]string{},
		ApplicationsDir: t.TempDir(), EtcDir: t.TempDir(),
	}
	rep, err := Add(AddOptions{Source: newSource(t), Yes: true}, env)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Results) != 79 {
		t.Errorf("results = %d, want one per agent", len(rep.Results))
	}
	if rep.Failed() {
		t.Errorf("results failed: %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "flat", "SKILL.md")); err != nil {
		t.Errorf("canonical copy missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "flat")); err != nil {
		t.Errorf("Claude Code directory not created: %v", err)
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

func TestAddRetriesDiscoveryWithFullDepthForAMissingSkill(t *testing.T) {
	src := t.TempDir()
	writeSourceFile(t, src, "SKILL.md", "---\nname: root\ndescription: d\n---\n")
	writeSourceFile(t, src, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\n")

	project := t.TempDir()
	rep, err := Add(AddOptions{
		Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}, Yes: true,
	}, testEnv(project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Skill != "alpha" {
		t.Errorf("results = %+v, want the skill found by the full-depth retry", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("retried skill was not installed: %v", err)
	}
}

func TestLockEntryForRemoteSource(t *testing.T) {
	in := recordInput{
		project: "/p",
		source: source.Source{
			Kind:      source.KindGitHub,
			Source:    "owner/repo",
			SourceURL: "https://github.com/owner/repo.git",
			Ref:       "v1",
		},
		commit: "0123456789abcdef0123456789abcdef01234567",
	}
	entry := lockEntry(in, install.Entry{Skill: skill.Skill{Name: "x", RelDir: "skills/x"}}, "hash")

	if entry.Source != "owner/repo" {
		t.Errorf("source = %q, want owner/repo", entry.Source)
	}
	// Upstream omits sourceUrl for GitHub sources: owner/repo re-derives the clone URL.
	if entry.SourceURL != "" {
		t.Errorf("sourceUrl = %q, want omitted for a GitHub source", entry.SourceURL)
	}
	if entry.SourceType != "github" || entry.Ref != "v1" || entry.Commit != in.commit {
		t.Errorf("entry = %+v", entry)
	}
	if entry.SkillPath != "skills/x/SKILL.md" {
		t.Errorf("skillPath = %q, want the source-relative SKILL.md", entry.SkillPath)
	}
	if entry.SkillName != "" || entry.InstallMode != "" {
		t.Errorf("unexpected optionals: %+v", entry)
	}
}

func TestLockSourceTypeMapsAzureToGit(t *testing.T) {
	if got := lockSourceType(source.KindAzure); got != "git" {
		t.Errorf("lockSourceType(azure) = %q, want git", got)
	}
	if got := lockSourceType(source.KindGitLab); got != "gitlab" {
		t.Errorf("lockSourceType(gitlab) = %q", got)
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

func TestLockEntryRecordsSourceURLForGenericGit(t *testing.T) {
	in := recordInput{
		project: "/p",
		source: source.Source{
			Kind:      source.KindGit,
			Source:    "file:///srv/repo",
			SourceURL: "file:///srv/repo",
		},
		commit: "0123456789abcdef0123456789abcdef01234567",
	}
	entry := lockEntry(in, install.Entry{Skill: skill.Skill{Name: "x", RelDir: "skills/x"}}, "hash")
	if entry.SourceType != "git" || entry.SourceURL != "file:///srv/repo" {
		t.Errorf("sourceType = %q, sourceUrl = %q", entry.SourceType, entry.SourceURL)
	}
}
