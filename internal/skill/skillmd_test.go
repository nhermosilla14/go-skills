package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseSkillMD(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Meta
		wantErr string
	}{
		{
			name:  "LF",
			input: "---\nname: alpha\ndescription: first\n---\nbody\n",
			want:  Meta{Name: "alpha", Description: "first"},
		},
		{
			name:  "CRLF and BOM",
			input: "\uFEFF---\r\nname: alpha\r\ndescription: first\r\n---\r\nbody",
			want:  Meta{Name: "alpha", Description: "first"},
		},
		{
			name:    "js opener is not frontmatter",
			input:   "---js\nname: alpha\ndescription: x\n---\n",
			wantErr: "missing required frontmatter field(s): name",
		},
		{
			name:    "no frontmatter",
			input:   "# just text\n",
			wantErr: "missing required frontmatter field(s): name, description",
		},
		{
			name:    "missing description",
			input:   "---\nname: alpha\n---\n",
			wantErr: "missing required frontmatter field(s): description",
		},
		{
			name:    "numeric name",
			input:   "---\nname: 123\ndescription: x\n---\n",
			wantErr: "must be strings: name",
		},
		{
			name:    "empty name after sanitizing",
			input:   "---\nname: \"\\x01\"\ndescription: x\n---\n",
			wantErr: "empty required frontmatter field(s): name",
		},
		{
			name:    "duplicate keys",
			input:   "---\nname: a\nname: b\ndescription: x\n---\n",
			wantErr: "invalid frontmatter",
		},
		{
			name:    "multiple documents",
			input:   "---\nname: a\n...\nname: b\ndescription: x\n---\n",
			wantErr: "multiple YAML documents",
		},
		{
			name:  "newlines in description collapse",
			input: "---\nname: a\ndescription: |\n  one\n  two\n---\n",
			want:  Meta{Name: "a", Description: "one two"},
		},
		{
			name:  "internal true",
			input: "---\nname: a\ndescription: x\nmetadata:\n  internal: true\n---\n",
			want:  Meta{Name: "a", Description: "x", Internal: true},
		},
		{
			name:  "internal string 1",
			input: "---\nname: a\ndescription: x\nmetadata:\n  internal: \"1\"\n---\n",
			want:  Meta{Name: "a", Description: "x", Internal: true},
		},
		{
			name:  "internal TRUE is not internal",
			input: "---\nname: a\ndescription: x\nmetadata:\n  internal: 'TRUE'\n---\n",
			want:  Meta{Name: "a", Description: "x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSkillMD([]byte(tc.input))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSkillMD: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// writeFile creates a file, with its parents, under the source root.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func skillMD(name string) string {
	return "---\nname: " + name + "\ndescription: " + name + " skill\n---\nbody\n"
}

func names(skills []Skill) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

func TestDiscoverLayoutsAndWarnings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/flat/SKILL.md", skillMD("flat-skill"))
	writeFile(t, root, "skills/cat/nested/SKILL.md", skillMD("nested-skill"))
	writeFile(t, root, "skills/broken/SKILL.md", "---\ndescription: no name\n---\n")
	writeFile(t, root, "skills/node_modules/x/SKILL.md", skillMD("ignored"))
	writeFile(t, root, "skills/loose.txt", "not a dir")

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{"flat-skill", "nested-skill"}
	if got := names(res.Skills); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	if got := res.Skills[1].SkillPath(); got != "skills/cat/nested/SKILL.md" {
		t.Errorf("SkillPath = %q", got)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "skills/broken/SKILL.md") {
		t.Errorf("warnings = %v, want one for skills/broken/SKILL.md", res.Warnings)
	}
}

func TestDiscoverRootSkillShadowsEverything(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "SKILL.md", skillMD("root-skill"))
	writeFile(t, root, "skills/flat/SKILL.md", skillMD("flat-skill"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"root-skill"}) {
		t.Errorf("names = %v, want only the root skill", got)
	}
	if got := res.Skills[0].SkillPath(); got != "SKILL.md" {
		t.Errorf("root SkillPath = %q", got)
	}
}

func TestDiscoverMalformedRootShadowsEverything(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "SKILL.md", "---\ndescription: no name\n---\n")
	writeFile(t, root, "skills/flat/SKILL.md", skillMD("flat-skill"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Skills) != 0 {
		t.Errorf("skills = %v, want none: a malformed root SKILL.md shadows the tree", names(res.Skills))
	}
}

func TestDiscoverShallowerShadowsDeeper(t *testing.T) {
	root := t.TempDir()
	// "a" sorts before "flat", so only the depth pass keeps the shallow skill.
	writeFile(t, root, "skills/a/dup/SKILL.md", skillMD("dup"))
	writeFile(t, root, "skills/flat/SKILL.md", skillMD("dup"))
	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := res.Skills[0].RelDir; got != "skills/flat" {
		t.Errorf("winner RelDir = %q, want skills/flat", got)
	}
	if len(res.Skills) != 1 {
		t.Errorf("skills = %d, want 1", len(res.Skills))
	}
}

func TestDiscoverSkillDirDoesNotDescend(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/outer/SKILL.md", skillMD("outer"))
	writeFile(t, root, "skills/outer/inner/SKILL.md", skillMD("inner"))
	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := []string{"outer"}; !reflect.DeepEqual(names(res.Skills), want) {
		t.Errorf("names = %v, want %v", names(res.Skills), want)
	}
}

func TestDiscoverInternalGating(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/hidden/SKILL.md",
		"---\nname: hidden\ndescription: x\nmetadata:\n  internal: true\n---\n")
	res, _ := Discover(root, Options{})
	if len(res.Skills) != 0 || len(res.Warnings) != 0 {
		t.Errorf("internal hidden: skills=%v warnings=%v", names(res.Skills), res.Warnings)
	}
	res, _ = Discover(root, Options{IncludeInternal: true})
	if want := []string{"hidden"}; !reflect.DeepEqual(names(res.Skills), want) {
		t.Errorf("internal included: names = %v", names(res.Skills))
	}
}

func TestDiscoverRejectsFileSource(t *testing.T) {
	file := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(file, Options{}); err == nil {
		t.Error("Discover on a file: want error")
	}
}
