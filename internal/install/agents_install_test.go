package install

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

func agentByID(t *testing.T, id string) agents.Agent {
	t.Helper()
	selected, invalid := agents.Expand([]string{id})
	if len(invalid) != 0 || len(selected) != 1 {
		t.Fatalf("Expand(%q) = %v, invalid %v", id, selected, invalid)
	}
	return selected[0]
}

func envAt(project, home string) agents.Environment {
	return agents.Environment{GOOS: runtime.GOOS, Home: home, Cwd: project, Vars: map[string]string{}}
}

func TestPlanDedupesSharedProjectDirectory(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"zencoder", "zenflow"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	e := entries[0]
	if e.Mode != ModeCopy {
		t.Fatalf("Mode = %s, want copy: one shared skills dir", e.Mode)
	}
	if len(e.Targets) != 2 {
		t.Fatalf("targets = %d, want one per agent id", len(e.Targets))
	}
	if e.Targets[0].Path != e.Targets[1].Path {
		t.Errorf("paths = %q and %q, want the same directory", e.Targets[0].Path, e.Targets[1].Path)
	}
}

func TestApplyWritesSharedDirectoryOnce(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	if err := os.MkdirAll(filepath.Join(project, ".zencoder"), 0o755); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(project, ".zencoder", "skills", "alpha")
	e := Entry{
		Skill:     s,
		Name:      "alpha",
		Canonical: filepath.Join(project, ".agents", "skills", "alpha"),
		Mode:      ModeSymlink,
		Targets: []Target{
			{Agent: agentByID(t, "zencoder"), Path: shared},
			{Agent: agentByID(t, "zenflow"), Path: shared},
		},
	}
	calls := 0
	link := func(link, target string) error {
		calls++
		return platformLink(link, target)
	}
	results := applyWith(project, []Entry{e}, link)
	if len(results) != 2 {
		t.Fatalf("results = %d, want one per agent", len(results))
	}
	if calls != 1 {
		t.Errorf("link calls = %d, want 1: the shared directory is written once", calls)
	}
	for _, r := range results {
		if r.Status != Installed {
			t.Errorf("result = %+v, want installed", r)
		}
	}
	if results[0].Agent == results[1].Agent {
		t.Error("both results name the same agent")
	}
}

func TestPlanMissingAgentDirectorySkipsInSymlinkMode(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"kiro-cli", "claude-code"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	if entries[0].Mode != ModeSymlink {
		t.Fatalf("Mode = %s, want symlink", entries[0].Mode)
	}
	results := Apply(project, entries)
	byAgent := map[string]Result{}
	for _, r := range results {
		byAgent[r.Agent] = r
	}
	if r := byAgent["kiro-cli"]; r.Status != Skipped || !strings.Contains(r.Message, "project directory not found") {
		t.Errorf("kiro-cli = %+v, want skipped for its missing .kiro", r)
	}
	if r := byAgent["claude-code"]; r.Status != Installed {
		t.Errorf("claude-code = %+v, want installed", r)
	}
	if _, err := os.Stat(filepath.Join(project, ".kiro")); !os.IsNotExist(err) {
		t.Errorf(".kiro was created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "alpha")); err != nil {
		t.Errorf("Claude Code did not create its directory: %v", err)
	}
}

func TestPlanCopyModeCreatesMissingAgentDirectory(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"kiro-cli"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	if entries[0].Mode != ModeCopy {
		t.Fatalf("Mode = %s, want copy", entries[0].Mode)
	}
	results := Apply(project, entries)
	if results[0].Status != Installed {
		t.Fatalf("result = %+v, want installed", results[0])
	}
	if _, err := os.Stat(filepath.Join(project, ".kiro", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("copy did not create the agent directory: %v", err)
	}
}

func TestApplyOpenClawOverlapIsSkipped(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	s.Dir = filepath.Join(project, "skills", "alpha")
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "SKILL.md"), []byte("---\nname: alpha\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, _ := agents.Expand([]string{"openclaw", "claude-code"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	results := Apply(project, entries)
	var openclaw *Result
	for i := range results {
		if results[i].Agent == "openclaw" {
			openclaw = &results[i]
		}
	}
	if openclaw == nil || openclaw.Status != Skipped || !strings.Contains(openclaw.Message, ErrOverlap.Error()) {
		t.Fatalf("openclaw = %+v, want skipped for source/destination overlap", openclaw)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "SKILL.md")); err != nil {
		t.Errorf("the source skill was touched: %v", err)
	}
}

func TestPlanEveIsAFlatCopy(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"eve", "claude-code"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	e := entries[0]
	if e.Mode != ModeSymlink {
		t.Fatalf("Mode = %s, want symlink: Claude Code adds a second directory", e.Mode)
	}
	var eve *Target
	for i := range e.Targets {
		if e.Targets[i].Agent.ID == "eve" {
			eve = &e.Targets[i]
		}
	}
	if eve == nil || !eve.Flat {
		t.Fatalf("eve target = %+v, want a flat target", eve)
	}
	if want := filepath.Join(project, "agent", "skills", "alpha.md"); eve.Path != want {
		t.Errorf("eve path = %q, want %q", eve.Path, want)
	}
	results := Apply(project, entries)
	for _, r := range results {
		if r.Agent == "eve" {
			if r.Status != Installed || r.Mode != ModeCopy {
				t.Errorf("eve result = %+v, want an installed copy", r)
			}
		}
	}
	info, err := os.Stat(eve.Path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("eve destination is not a file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "agent", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("eve also wrote a directory: %v", err)
	}
}

func TestApplyEveReducesFrontmatter(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "src", "alpha")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: Alpha\ndescription: d\nlicense: MIT\ncompatibility: \">=1\"\nversion: 2\nallowed-tools: bash\nmetadata:\n  internal: false\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s := skill.Skill{Name: "Alpha", Dir: src}
	selected, _ := agents.Expand([]string{"eve"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir()), Rename: "renamed"})
	if entries[0].Mode != ModeCopy {
		t.Fatalf("Mode = %s, want copy: Eve alone", entries[0].Mode)
	}
	results := Apply(project, entries)
	if results[0].Status != Installed {
		t.Fatalf("result = %+v", results[0])
	}
	got, err := os.ReadFile(filepath.Join(project, "agent", "skills", "renamed.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{"name: renamed", "description: d", "license: MIT", "compatibility: '>=1'", "version: 2", "metadata:\n    internal: false"} {
		if !strings.Contains(text, want) {
			t.Errorf("flat file misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "allowed-tools") {
		t.Errorf("flat file kept a dropped key:\n%s", text)
	}
	if !strings.HasSuffix(text, "body\n") {
		t.Errorf("flat file body = %q", text)
	}
}

func TestPlanModeDerivation(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	env := envAt(project, home)
	s := newSkill(t, project, "alpha")
	tests := []struct {
		name      string
		ids       []string
		opts      PlanOptions
		wantMode  Mode
		wantCanon string
		wantPaths []string
	}{
		{"single agent copies", []string{"claude-code"}, PlanOptions{}, ModeCopy, "",
			[]string{filepath.Join(project, ".claude", "skills", "alpha")}},
		{"two dirs link", []string{"opencode", "claude-code"}, PlanOptions{}, ModeSymlink,
			filepath.Join(project, ".agents", "skills", "alpha"),
			[]string{filepath.Join(project, ".claude", "skills", "alpha"), filepath.Join(project, ".agents", "skills", "alpha")}},
		{"Eve alone copies", []string{"eve"}, PlanOptions{}, ModeCopy, "",
			[]string{filepath.Join(project, "agent", "skills", "alpha.md")}},
		{"forced copy", []string{"opencode", "claude-code"}, PlanOptions{ForceCopy: true}, ModeCopy, "", nil},
		{"global single agent copies", []string{"claude-code"}, PlanOptions{Global: true}, ModeCopy, "",
			[]string{filepath.Join(home, ".claude", "skills", "alpha")}},
		{"global two dirs link", []string{"claude-code", "codex"}, PlanOptions{Global: true}, ModeSymlink,
			filepath.Join(home, ".agents", "skills", "alpha"),
			[]string{filepath.Join(home, ".claude", "skills", "alpha"), filepath.Join(home, ".agents", "skills", "alpha")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selected, invalid := agents.Expand(tc.ids)
			if len(invalid) != 0 {
				t.Fatalf("invalid %v", invalid)
			}
			opts := tc.opts
			opts.Env = env
			entries := Plan(project, []skill.Skill{s}, selected, opts)
			e := entries[0]
			if e.Mode != tc.wantMode {
				t.Errorf("Mode = %s, want %s", e.Mode, tc.wantMode)
			}
			if e.Canonical != tc.wantCanon {
				t.Errorf("Canonical = %q, want %q", e.Canonical, tc.wantCanon)
			}
			if tc.wantPaths != nil {
				var got []string
				for _, target := range e.Targets {
					got = append(got, target.Path)
				}
				if !reflect.DeepEqual(got, tc.wantPaths) {
					t.Errorf("paths = %v, want %v", got, tc.wantPaths)
				}
			}
		})
	}
}

func TestApplyLinkFailureFallsBackToCopy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fallback shape")
	}
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"opencode", "claude-code"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	failing := func(_, _ string) error { return errors.New("links are not permitted") }
	results := applyWith(project, entries, failing)
	var claude *Result
	for i := range results {
		if results[i].Agent == "claude-code" {
			claude = &results[i]
		}
	}
	if claude == nil || claude.Status != Installed || claude.Mode != ModeCopy {
		t.Fatalf("claude-code = %+v, want an installed copy", claude)
	}
	if !strings.Contains(claude.Message, "copied instead") {
		t.Errorf("message = %q, want the fallback notice", claude.Message)
	}
	info, err := os.Lstat(claude.Path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Errorf("fallback destination is not a plain directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(claude.Path, "SKILL.md")); err != nil {
		t.Errorf("fallback copy is incomplete: %v", err)
	}
}
