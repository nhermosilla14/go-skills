package e2e

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/skill"
)

// initOutput is the documented human output of an init that wrote SKILL.md.
func initOutput(name, displayPath string) string {
	return "Initialized skill: " + name + "\n\n" +
		"Created:\n  " + displayPath + "\n\n" +
		"Next steps:\n" +
		"  1. Edit " + displayPath + " to define your skill instructions\n" +
		"  2. Add supporting files next to SKILL.md and reference them from the instructions.\n"
}

func TestE2EInit(t *testing.T) {
	t.Run("default template", func(t *testing.T) {
		project := filepath.Join(t.TempDir(), "demo-project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		res := run(t, project, "init")
		if res.exit != 0 {
			t.Fatalf("exit = %d\nstderr:\n%s", res.exit, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("stdout = %q, want empty", res.stdout)
		}
		if want := initOutput("demo-project", "SKILL.md"); res.stderr != want {
			t.Errorf("stderr = %q, want %q", res.stderr, want)
		}
		requireInitSkill(t, project, "SKILL.md", "demo-project")
	})

	t.Run("named directory", func(t *testing.T) {
		project := t.TempDir()
		res := run(t, project, "init", "my-skill")
		if res.exit != 0 {
			t.Fatalf("exit = %d\nstderr:\n%s", res.exit, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("stdout = %q, want empty", res.stdout)
		}
		if want := initOutput("my-skill", "my-skill/SKILL.md"); res.stderr != want {
			t.Errorf("stderr = %q, want %q", res.stderr, want)
		}
		requireInitSkill(t, project, "my-skill/SKILL.md", "my-skill")
	})

	t.Run("existing file", func(t *testing.T) {
		project := t.TempDir()
		if res := run(t, project, "init"); res.exit != 0 {
			t.Fatalf("first init exit = %d\nstderr:\n%s", res.exit, res.stderr)
		}
		before, err := os.ReadFile(filepath.Join(project, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		res := run(t, project, "init")
		if res.exit != 0 {
			t.Fatalf("second init exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("stdout = %q, want empty", res.stdout)
		}
		if want := "Skill already exists at SKILL.md\n"; res.stderr != want {
			t.Errorf("stderr = %q, want %q", res.stderr, want)
		}
		after, err := os.ReadFile(filepath.Join(project, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Error("init overwrote the existing SKILL.md")
		}
	})

	t.Run("traversal rejected", func(t *testing.T) {
		root := t.TempDir()
		project := filepath.Join(root, "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		before := dirEntries(t, root)
		res := run(t, project, "init", "../escape")
		if res.exit != 2 {
			t.Fatalf("exit = %d, want 2\nstderr:\n%s", res.exit, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("stdout = %q, want empty on failure", res.stdout)
		}
		if !strings.Contains(res.stderr, "invalid skill name") {
			t.Errorf("stderr misses the invalid-name error:\n%s", res.stderr)
		}
		if !strings.Contains(res.stderr, "Run 'skills --help' for usage.") {
			t.Errorf("stderr misses the usage hint:\n%s", res.stderr)
		}
		if after := dirEntries(t, root); !reflect.DeepEqual(after, before) {
			t.Errorf("the temp project changed: before %v, after %v", before, after)
		}
		if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
			t.Errorf("%s was written outside the project: %v", filepath.Join(root, "escape"), err)
		}
	})
}

// requireInitSkill asserts that the generated SKILL.md parses and is what discovery finds.
func requireInitSkill(t *testing.T, project, rel, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	meta, err := skill.ParseSkillMD(raw)
	if err != nil {
		t.Fatalf("the generated SKILL.md does not parse: %v", err)
	}
	if meta.Name != name {
		t.Errorf("frontmatter name = %q, want %q", meta.Name, name)
	}
	if meta.Description != "A brief description of what this skill does" {
		t.Errorf("frontmatter description = %q", meta.Description)
	}
	found, err := skill.Discover(project, skill.Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found.Skills) != 1 || found.Skills[0].Name != name {
		t.Errorf("discovery found %+v, want exactly %q", found.Skills, name)
	}
}

// dirEntries lists the names in dir, sorted, so two listings compare deterministically.
func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}
