package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/hash"
)

// legacyVector is one fixed input of the version 1 hash.
type legacyVector struct {
	name  string
	files map[string]string
	want  string
}

func TestLegacyHashVectors(t *testing.T) {
	vectors := []legacyVector{
		{name: "empty", files: nil, want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{name: "one file", files: map[string]string{"SKILL.md": "# hi\n"},
			want: "615cf79d5358268f527f9e2f1722904ae6d2a78046d81c3f4f6ac46a1674bd82"},
		{name: "nested", files: map[string]string{"SKILL.md": "a\n", "docs/b.md": "b\n"},
			want: "9684ebf39335adde13c2e3a46cc270e0a1157dc69aeea0e059994e9407ea2a60"},
		{name: "empty content", files: map[string]string{"a": "x", "b": ""},
			want: "06d76959eff8d3d55696e8b490f93bb345bc1e58a941a13b1445ad6588f9e688"},
		{name: "utf16 order", files: map[string]string{"B.md": "2", "b.md": "1"},
			want: "2790e2bbb94a2eaf5fa4102adc38ba8b2aded0ed09771fbd19bce086ca418909"},
	}
	for _, tc := range vectors {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tc.files {
				writeLegacyFile(t, dir, rel, content)
			}
			got, err := legacyHash(dir)
			if err != nil {
				t.Fatalf("legacyHash: %v", err)
			}
			if got != tc.want {
				t.Errorf("legacyHash = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestLegacyHashSkipsGitAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeLegacyFile(t, dir, "SKILL.md", "x")
	writeLegacyFile(t, dir, ".git/objects/a", "ignored")
	writeLegacyFile(t, dir, "node_modules/pkg/index.js", "ignored")
	writeLegacyFile(t, dir, "nested/node_modules/pkg/index.js", "ignored")
	got, err := legacyHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	only := t.TempDir()
	writeLegacyFile(t, only, "SKILL.md", "x")
	want, err := legacyHash(only)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("legacyHash = %s, want %s: excluded directories counted", got, want)
	}
}

func TestLoadMigratesVersion1(t *testing.T) {
	project := t.TempDir()
	installed := filepath.Join(project, ".agents", "skills", "alpha")
	writeLegacyFile(t, installed, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	writeLegacyFile(t, installed, "notes.txt", "keep\n")
	legacy, err := legacyHash(installed)
	if err != nil {
		t.Fatal(err)
	}
	want, err := hash.Dir(installed)
	if err != nil {
		t.Fatal(err)
	}

	edited := filepath.Join(project, ".agents", "skills", "beta")
	writeLegacyFile(t, edited, "SKILL.md", "---\nname: beta\ndescription: b\n---\n")
	editedLegacy, err := legacyHash(edited)
	if err != nil {
		t.Fatal(err)
	}
	writeLegacyFile(t, edited, "SKILL.md", "---\nname: beta\ndescription: b\n---\nedited\n")

	in := `{"version":1,"future":"kept","skills":{
		"alpha":{"source":"./src","sourceType":"local","computedHash":"` + legacy + `","vendor":"keep"},
		"beta":{"source":"./src","sourceType":"local","computedHash":"` + editedLegacy + `"},
		"gone":{"source":"./src","sourceType":"local","computedHash":"` + editedLegacy + `"},
		"empty":{"source":"./src","sourceType":"local","computedHash":""}}}`
	path := filepath.Join(project, FileName)
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := f.Skills["alpha"].ComputedHash; got != want {
		t.Errorf("alpha hash = %q, want the v2 hash %q", got, want)
	}
	for _, key := range []string{"beta", "gone", "empty"} {
		if got := f.Skills[key].ComputedHash; got != "" {
			t.Errorf("%s hash = %q, want empty", key, got)
		}
	}
	if f.Skills["alpha"].Source != "./src" || f.Skills["alpha"].SkillName != "" {
		t.Errorf("alpha entry changed: %+v", f.Skills["alpha"])
	}

	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	if !strings.Contains(text, `"version": 2`) {
		t.Errorf("migration was not persisted:\n%s", text)
	}
	if !strings.Contains(text, `"future": "kept"`) || !strings.Contains(text, `"vendor": "keep"`) {
		t.Errorf("unknown fields lost:\n%s", text)
	}
}

func TestLoadMigratesLocalSourceInstalledInPlace(t *testing.T) {
	project := t.TempDir()
	source := filepath.Join(project, "src", "alpha")
	writeLegacyFile(t, source, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	legacy, err := legacyHash(source)
	if err != nil {
		t.Fatal(err)
	}
	want, err := hash.Dir(source)
	if err != nil {
		t.Fatal(err)
	}
	in := `{"version":1,"skills":{"alpha":{"source":"./src/alpha","sourceType":"local","computedHash":"` +
		legacy + `"}}}`
	if err := os.WriteFile(filepath.Join(project, FileName), []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(filepath.Join(project, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Skills["alpha"].ComputedHash; got != want {
		t.Errorf("hash = %q, want %q", got, want)
	}
}

func writeLegacyFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
