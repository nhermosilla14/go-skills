package lockfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "skills", GlobalFileName)
	f := NewGlobal()
	f.Put("alpha", GlobalEntry{
		Source:          "owner/repo",
		SourceType:      "github",
		SourceURL:       "https://github.com/owner/repo",
		Ref:             "main",
		Commit:          strings.Repeat("a", 40),
		SkillPath:       "skills/alpha/SKILL.md",
		SkillName:       "alpha-upstream",
		SkillFolderHash: strings.Repeat("b", 40),
		InstalledAt:     "2026-01-01T00:00:00.000Z",
		UpdatedAt:       "2026-02-01T00:00:00.000Z",
		InstallMode:     "copy",
	})
	f.SetLastSelectedAgents([]string{"claude-code", "codex"})
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		`"version": 3`,
		`"skills": {`,
		`"alpha": {`,
		`"skillFolderHash": "` + strings.Repeat("b", 40) + `"`,
		`"installedAt": "2026-01-01T00:00:00.000Z"`,
		`"lastSelectedAgents": [`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("document misses %q:\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("document has no trailing newline")
	}
	if strings.Contains(text, "\t") {
		t.Error("document is not two-space indented")
	}

	loaded, err := LoadGlobal(path)
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	entry, ok := loaded.Get("alpha")
	if !ok {
		t.Fatal("alpha entry lost")
	}
	if entry.InstalledAt != "2026-01-01T00:00:00.000Z" || entry.InstallMode != "copy" {
		t.Errorf("entry = %+v", entry)
	}
	if len(loaded.LastSelectedAgents) != 2 || loaded.LastSelectedAgents[1] != "codex" {
		t.Errorf("lastSelectedAgents = %v", loaded.LastSelectedAgents)
	}
}

func TestGlobalPreservesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), GlobalFileName)
	in := `{"version":3,"future":{"a":[1,2]},"dismissed":{"findSkillsPrompt":true,"other":1},` +
		`"skills":{"alpha":{"source":"s","sourceType":"local","skillFolderHash":"h","vendor":"keep"}}}`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := LoadGlobal(path)
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	for _, want := range []string{
		`"future": {`, `"a": [`, `"vendor": "keep"`, `"findSkillsPrompt": true`, `"other": 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("document lost %q:\n%s", want, text)
		}
	}
}

func TestGlobalOldVersionsAreReadAsEmptyAndLeftAlone(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("version "+version, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), GlobalFileName)
			in := `{"version":` + version + `,"skills":{"alpha":{"source":"s"}}}`
			if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := LoadGlobal(path)
			if err != nil {
				t.Fatalf("LoadGlobal: %v", err)
			}
			if len(f.Skills) != 0 {
				t.Errorf("skills = %v, want an empty document", f.Skills)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != in {
				t.Errorf("the old file was rewritten:\n%s", raw)
			}
		})
	}
}

func TestGlobalCorruptFileIsRefused(t *testing.T) {
	tests := map[string]string{
		"not json":            `<<<<<<< HEAD`,
		"no version":          `{"skills":{}}`,
		"no skills":           `{"version":3}`,
		"null skills":         `{"version":3,"skills":null}`,
		"bad dismissed":       `{"version":3,"skills":{},"dismissed":[]}`,
		"bad selected agents": `{"version":3,"skills":{},"lastSelectedAgents":"x"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), GlobalFileName)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadGlobal(path); err == nil {
				t.Fatal("LoadGlobal: want error")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != body {
				t.Errorf("corrupt lock was rewritten:\n%s", raw)
			}
		})
	}
}

func TestGlobalFailedEncodeLeavesFileIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), GlobalFileName)
	old := `{"version":3,"skills":{}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewGlobal()
	f.extra = map[string]json.RawMessage{"broken": []byte("{")}
	if err := f.Save(path); err == nil {
		t.Fatal("Save: want an encoding error")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != old {
		t.Errorf("a failed encode changed the file:\n%s", raw)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*.tmp"))
	if len(matches) != 0 {
		t.Errorf("temporary files left behind: %v", matches)
	}
}

func TestGlobalDeletedLastEntryKeepsEmptySkills(t *testing.T) {
	path := filepath.Join(t.TempDir(), GlobalFileName)
	f := NewGlobal()
	f.Put("alpha", GlobalEntry{Source: "s", SourceType: "local"})
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	f.Delete("alpha")
	if err := f.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"skills": {}`) {
		t.Errorf("document = %s, want an empty skills map", raw)
	}
}
