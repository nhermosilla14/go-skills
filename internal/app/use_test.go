package app

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/skill"
)

func TestSelectUseSkill(t *testing.T) {
	alpha := skill.Skill{Name: "alpha", Dir: "/src/alpha"}
	beta := skill.Skill{Name: "beta", Dir: "/src/beta"}
	tests := []struct {
		name     string
		all      []skill.Skill
		selector string
		flag     string
		want     string
		wantErr  []string
	}{
		{"at selector", []skill.Skill{alpha, beta}, "alpha", "", "alpha", nil},
		{"flag selector", []skill.Skill{alpha, beta}, "", "beta", "beta", nil},
		{"same selector twice", []skill.Skill{alpha, beta}, "alpha", "alpha", "alpha", nil},
		{"single skill without selector", []skill.Skill{alpha}, "", "", "alpha", nil},
		{"case-insensitive selector", []skill.Skill{alpha}, "ALPHA", "", "alpha", nil},
		{
			"several skills without selector",
			[]skill.Skill{alpha, beta}, "", "", "",
			[]string{"multiple skills", "alpha", "beta"},
		},
		{
			"selector matches nothing",
			[]skill.Skill{alpha, beta}, "", "missing", "",
			[]string{"No matching skill", "alpha", "beta"},
		},
		{
			"conflicting selectors",
			[]skill.Skill{alpha, beta}, "alpha", "beta", "",
			[]string{"Conflicting skill selectors", "alpha", "beta"},
		},
		{
			"empty source",
			nil, "", "", "",
			[]string{"No valid skills"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectUseSkill(tc.all, tc.selector, tc.flag, "./source")
			if len(tc.wantErr) > 0 {
				if err == nil {
					t.Fatalf("err = nil, want a diagnostic")
				}
				for _, want := range tc.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("err = %q, want containing %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("selectUseSkill: %v", err)
			}
			if got.Name != tc.want {
				t.Errorf("selected %q, want %q", got.Name, tc.want)
			}
		})
	}
}

func TestBuildUsePromptGolden(t *testing.T) {
	contents := []byte("---\nname: demo\ndescription: demo skill\n---\n\nBody.\n")
	tests := []struct {
		name       string
		supporting bool
		golden     string
	}{
		{"without supporting files", false, "use-prompt.golden"},
		{"with supporting files", true, "use-prompt-supporting.golden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildUsePrompt(contents, "/tmp/skills-use-demo/demo", tc.supporting)
			raw, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			want := strings.ReplaceAll(string(raw), "\r\n", "\n")
			if got != want {
				t.Errorf("prompt differs from %s:\n--- got ---\n%q\n--- want ---\n%q", tc.golden, got, want)
			}
		})
	}
}

func TestUseMaterializesAndKeepsTheSkill(t *testing.T) {
	source := t.TempDir()
	example := filepath.Join(source, "skills", "Weird Name!")
	writeUseFile(t, filepath.Join(example, "SKILL.md"),
		"---\nname: example\ndescription: example skill\n---\n\nBody.\n")
	writeUseFile(t, filepath.Join(example, "notes.txt"), "notes\n")
	writeUseFile(t, filepath.Join(example, "sub", "data.bin"), "\x00\x01")
	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "outside.txt")
		writeUseFile(t, outside, "outside\n")
		if err := os.Symlink(filepath.Join(example, "notes.txt"), filepath.Join(example, "alias.txt")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(example, "escape.txt")); err != nil {
			t.Fatal(err)
		}
	}

	tmp := t.TempDir()
	isolateTemp(t, tmp)
	project := t.TempDir()
	var out bytes.Buffer
	rep, err := Use(UseOptions{Source: source}, Env{WorkDir: project}, &out)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(rep.Dir)) })

	if base := filepath.Base(rep.Dir); base != "weird-name" {
		t.Errorf("materialized as %q, want the sanitized folder name", base)
	}
	if !strings.HasPrefix(rep.Dir, filepath.Clean(tmp)+string(filepath.Separator)) {
		t.Errorf("materialized dir %q is not under the temp root %q", rep.Dir, tmp)
	}
	for _, rel := range []string{"SKILL.md", "notes.txt", filepath.Join("sub", "data.bin")} {
		if _, err := os.Stat(filepath.Join(rep.Dir, rel)); err != nil {
			t.Errorf("%s missing after Use: %v", rel, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Lstat(filepath.Join(rep.Dir, "alias.txt"))
		if err != nil {
			t.Fatalf("internal symlink was not copied: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Error("internal symlink was not dereferenced")
		}
		if _, err := os.Stat(filepath.Join(rep.Dir, "escape.txt")); !os.IsNotExist(err) {
			t.Errorf("escaping symlink was copied: %v", err)
		}
	}
	if out.String() != rep.Prompt {
		t.Error("the returned prompt differs from what was written")
	}
	if !strings.Contains(rep.Prompt, "Supporting files for this skill were downloaded to:\n"+rep.Dir) {
		t.Errorf("prompt misses the supporting-files section:\n%s", rep.Prompt)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("use wrote a project lock: %v", err)
	}
}

func TestUseCleansTheCloneWorkspace(t *testing.T) {
	repo := gitSkillRepo(t)
	tmp := t.TempDir()
	isolateTemp(t, tmp)
	remote := (&url.URL{Scheme: "file", Path: filepath.ToSlash(repo)}).String()

	var out bytes.Buffer
	rep, err := Use(UseOptions{Source: remote}, Env{}, &out)
	if err != nil {
		t.Fatalf("Use: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(rep.Dir)) })

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	kept := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "skills-use-") {
			kept++
			continue
		}
		t.Errorf("leftover temporary entry %q", entry.Name())
	}
	if kept != 1 {
		t.Errorf("kept %d materialized directories, want 1", kept)
	}
}

func TestUseInternalSkillsGate(t *testing.T) {
	source := t.TempDir()
	writeUseFile(t, filepath.Join(source, "skills", "secret", "SKILL.md"),
		"---\nname: secret\ndescription: internal skill\nmetadata:\n  internal: true\n---\n\nSecret.\n")

	var out bytes.Buffer
	if _, err := Use(UseOptions{Source: source}, Env{}, &out); err == nil {
		t.Fatal("Use exposed an internal skill without INSTALL_INTERNAL_SKILLS")
	}
	rep, err := Use(UseOptions{Source: source}, Env{IncludeInternal: true}, &out)
	if err != nil {
		t.Fatalf("Use with internal skills enabled: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(rep.Dir)) })
	if !strings.Contains(rep.Prompt, "Secret.") {
		t.Errorf("prompt misses the internal skill:\n%s", rep.Prompt)
	}
}

func TestUseHonoursFullDepth(t *testing.T) {
	source := t.TempDir()
	writeUseFile(t, filepath.Join(source, "SKILL.md"),
		"---\nname: root-skill\ndescription: root skill\n---\n\nRoot.\n")
	writeUseFile(t, filepath.Join(source, "skills", "hidden", "SKILL.md"),
		"---\nname: hidden-skill\ndescription: hidden skill\n---\n\nHidden.\n")

	var out bytes.Buffer
	if _, err := Use(UseOptions{Source: source, Skill: "hidden-skill"}, Env{}, &out); err == nil {
		t.Fatal("Use without --full-depth found a skill under a root SKILL.md")
	}
	rep, err := Use(UseOptions{Source: source, Skill: "hidden-skill", FullDepth: true}, Env{}, &out)
	if err != nil {
		t.Fatalf("Use with --full-depth: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(rep.Dir)) })
	if !strings.Contains(rep.Prompt, "Hidden.") {
		t.Errorf("prompt does not carry the selected skill:\n%s", rep.Prompt)
	}
}

func writeUseFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateTemp points the OS temp directory at dir for the duration of the test.
func isolateTemp(t *testing.T, dir string) {
	t.Helper()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
}

// gitSkillRepo creates a local git repository holding one skill.
func gitSkillRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	writeUseFile(t, filepath.Join(dir, "skills", "demo", "SKILL.md"),
		"---\nname: demo\ndescription: demo skill\n---\n\nBody.\n")
	runUseGit(t, dir, "init", "-q")
	runUseGit(t, dir, "config", "user.email", "test@example.com")
	runUseGit(t, dir, "config", "user.name", "Test")
	runUseGit(t, dir, "add", ".")
	runUseGit(t, dir, "commit", "-qm", "init")
	return dir
}

func runUseGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
