package install

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

func TestPlanEveSubagentTargets(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"eve"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{
		Env: envAt(project, t.TempDir()), Subagents: []string{"reviewer", "root", "reviewer", "."},
	})
	e := entries[0]
	if e.Mode != ModeCopy {
		t.Fatalf("Mode = %s, want copy", e.Mode)
	}
	if want := []string{"", "reviewer"}; !reflect.DeepEqual(e.Subagents, want) {
		t.Errorf("Subagents = %v, want %v", e.Subagents, want)
	}
	var paths []string
	for _, target := range e.Targets {
		if !target.Flat {
			t.Errorf("target %+v is not flat", target)
		}
		paths = append(paths, target.Path)
	}
	want := []string{
		filepath.Join(project, "agent", "skills", "alpha.md"),
		filepath.Join(project, "agent", "subagents", "reviewer", "skills", "alpha.md"),
	}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

func TestPlanEveSubagentOnly(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"eve"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{
		Env: envAt(project, t.TempDir()), Subagents: []string{"writer"},
	})
	e := entries[0]
	if want := []string{"writer"}; !reflect.DeepEqual(e.Subagents, want) {
		t.Errorf("Subagents = %v, want %v", e.Subagents, want)
	}
	if len(e.Targets) != 1 {
		t.Fatalf("targets = %v, want the subagent only", e.Targets)
	}
	if want := filepath.Join(project, "agent", "subagents", "writer", "skills", "alpha.md"); e.Targets[0].Path != want {
		t.Errorf("path = %q, want %q", e.Targets[0].Path, want)
	}
}

func TestPlanEveWithoutSubagentsIsRootOnly(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"eve"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{Env: envAt(project, t.TempDir())})
	e := entries[0]
	if want := []string{""}; !reflect.DeepEqual(e.Subagents, want) {
		t.Errorf("Subagents = %v, want %v", e.Subagents, want)
	}
	if want := filepath.Join(project, "agent", "skills", "alpha.md"); len(e.Targets) != 1 || e.Targets[0].Path != want {
		t.Errorf("targets = %v, want only %s", e.Targets, want)
	}
}

func TestPlanEveSubagentNameStaysInsideItsDirectory(t *testing.T) {
	project := t.TempDir()
	s := newSkill(t, project, "alpha")
	selected, _ := agents.Expand([]string{"eve"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{
		Env: envAt(project, t.TempDir()), Subagents: []string{"../../etc"},
	})
	base := filepath.Join(project, "agent", "subagents")
	if len(entries[0].Targets) != 1 {
		t.Fatalf("targets = %v, want one", entries[0].Targets)
	}
	path := entries[0].Targets[0].Path
	if !strings.HasPrefix(path, base+string(filepath.Separator)) {
		t.Errorf("path = %q, want it below %q", path, base)
	}
	if want := filepath.Join(base, "etc", "skills", "alpha.md"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestApplyWritesEveryEveSubagentWithReducedFrontmatter(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "src", "alpha")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: Alpha\ndescription: d\nallowed-tools: bash\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s := skill.Skill{Name: "Alpha", Dir: src}
	selected, _ := agents.Expand([]string{"eve"})
	entries := Plan(project, []skill.Skill{s}, selected, PlanOptions{
		Env: envAt(project, t.TempDir()), Subagents: []string{"", "reviewer"},
	})
	results := Apply(project, entries)
	if len(results) != 2 {
		t.Fatalf("results = %+v, want one per Eve file", results)
	}
	for _, res := range results {
		if res.Status != Installed {
			t.Errorf("result = %+v, want installed", res)
		}
		got, err := os.ReadFile(res.Path)
		if err != nil {
			t.Fatalf("reading %s: %v", res.Path, err)
		}
		if want := "---\nname: Alpha\ndescription: d\n---\nbody\n"; string(got) != want {
			t.Errorf("%s = %q, want %q", res.Path, got, want)
		}
	}
}
