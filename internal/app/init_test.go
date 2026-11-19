package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/skill"
)

// initEnv is an environment whose working directory is dir.
func initEnv(dir string) Env {
	return Env{WorkDir: dir}
}

func TestInitDefaultNameFromDirectory(t *testing.T) {
	project := filepath.Join(t.TempDir(), "demo-project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err := Init(InitOptions{}, initEnv(project))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if rep.Name != "demo-project" {
		t.Errorf("Name = %q, want demo-project", rep.Name)
	}
	if rep.DisplayPath != "SKILL.md" {
		t.Errorf("DisplayPath = %q, want SKILL.md", rep.DisplayPath)
	}
	if rep.Exists {
		t.Error("Exists = true on a fresh directory")
	}
	meta, err := skill.ParseSkillMD([]byte(readFile(t, filepath.Join(project, "SKILL.md"))))
	if err != nil {
		t.Fatalf("the template does not parse: %v", err)
	}
	if meta.Name != "demo-project" {
		t.Errorf("frontmatter name = %q, want demo-project", meta.Name)
	}
}

func TestInitNamedDirectory(t *testing.T) {
	project := t.TempDir()
	rep, err := Init(InitOptions{Name: "my-skill"}, initEnv(project))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if rep.Name != "my-skill" || rep.DisplayPath != "my-skill/SKILL.md" {
		t.Fatalf("report = %+v, want the named skill", rep)
	}
	raw, err := os.ReadFile(filepath.Join(project, "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("reading the template: %v", err)
	}
	meta, err := skill.ParseSkillMD(raw)
	if err != nil {
		t.Fatalf("the template does not parse: %v", err)
	}
	if meta.Name != "my-skill" || meta.Description != "A brief description of what this skill does" {
		t.Errorf("frontmatter = %+v, want the named skill and the documented description", meta)
	}
}

func TestInitCreatesMissingParents(t *testing.T) {
	project := filepath.Join(t.TempDir(), "missing", "nested")
	rep, err := Init(InitOptions{Name: "my-skill"}, initEnv(project))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "my-skill", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md was not created: %v", err)
	}
	if want := "my-skill/SKILL.md"; rep.DisplayPath != want {
		t.Errorf("DisplayPath = %q, want %q", rep.DisplayPath, want)
	}
}

func TestInitNeverOverwrites(t *testing.T) {
	project := t.TempDir()
	existing := filepath.Join(project, "my-skill", "SKILL.md")
	writeUseFile(t, existing, "mine\n")
	rep, err := Init(InitOptions{Name: "my-skill"}, initEnv(project))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !rep.Exists {
		t.Error("Exists = false for an existing SKILL.md")
	}
	if got := readFile(t, existing); got != "mine\n" {
		t.Errorf("existing file = %q, want it untouched", got)
	}
}

func TestInitRejectsUnsafeNames(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"dot", "."},
		{"dot dot", ".."},
		{"parent", "../x"},
		{"parent backslash", `..\x`},
		{"absolute", "/abs"},
		{"absolute backslash", `\abs`},
		{"drive", "C:"},
		{"drive path", `C:\x`},
		{"home", "~"},
		{"home path", "~/x"},
		{"separator", "a/b"},
		{"backslash", `a\b`},
		{"trailing parent separator", "../"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			_, err := Init(InitOptions{Name: tc.want}, initEnv(project))
			if !errors.Is(err, ErrUsage) {
				t.Fatalf("err = %v, want ErrUsage", err)
			}
			entries, readErr := os.ReadDir(project)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Errorf("the working directory gained %d entries after a rejected name", len(entries))
			}
			parent, readErr := os.ReadDir(filepath.Dir(project))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(parent) != 1 {
				t.Errorf("the parent gained entries after a rejected name: %v", parent)
			}
		})
	}
}

func TestInitNormalizesTrailingSeparators(t *testing.T) {
	project := t.TempDir()
	rep, err := Init(InitOptions{Name: "my-skill/"}, initEnv(project))
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if rep.Name != "my-skill" || rep.DisplayPath != "my-skill/SKILL.md" {
		t.Errorf("report = %+v, want the normalized name", rep)
	}
}

func TestInitEscapesTheNameForTheSkillParser(t *testing.T) {
	names := []string{
		"demo: x",
		"my skill # best",
		"true",
		"123",
		"[demo]",
		`say "hello"`,
		"null",
		"1.5",
		"yes",
		"@demo",
		"%demo",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			project := t.TempDir()
			if _, err := Init(InitOptions{Name: name}, initEnv(project)); err != nil {
				t.Fatalf("Init: %v", err)
			}
			raw, err := os.ReadFile(filepath.Join(project, name, "SKILL.md"))
			if err != nil {
				t.Fatalf("reading the template: %v", err)
			}
			meta, err := skill.ParseSkillMD(raw)
			if err != nil {
				t.Fatalf("the template does not parse: %v", err)
			}
			if meta.Name != name {
				t.Errorf("frontmatter name = %q, want %q", meta.Name, name)
			}
		})
	}
}

func TestInitTemplateGolden(t *testing.T) {
	got := string(initTemplate("demo-skill"))
	raw, err := os.ReadFile(filepath.Join("testdata", "init-SKILL.md.golden"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got != want {
		t.Errorf("template differs from init-SKILL.md.golden:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Error("the template must end with exactly one newline")
	}
}
