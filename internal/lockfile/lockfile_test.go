package lockfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const wantAlpha = `{
  "version": 2,
  "skills": {
    "alpha": {
      "source": "./src/alpha",
      "sourceType": "local",
      "skillPath": "skills/alpha/SKILL.md",
      "computedHash": "abc"
    }
  }
}
`

func TestSaveWritesSortedIndentedDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f := New()
	f.Put("alpha", Entry{
		Source:       "./src/alpha",
		SourceType:   "local",
		SkillPath:    "skills/alpha/SKILL.md",
		ComputedHash: "abc",
	})
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != wantAlpha {
		t.Errorf("document =\n%s\nwant\n%s", got, wantAlpha)
	}
}

func TestSaveKeepsEmptyComputedHashAndNoHTMLEscaping(t *testing.T) {
	f := New()
	f.Put("a<b", Entry{Source: "x&y", SourceType: "download"})
	data, err := f.encode()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"computedHash": ""`) {
		t.Errorf("empty computedHash not written:\n%s", s)
	}
	if !strings.Contains(s, `"a<b"`) || !strings.Contains(s, `"x&y"`) {
		t.Errorf("characters escaped:\n%s", s)
	}
}

func TestSortedKeysUTF16(t *testing.T) {
	// U+FF5E sorts before U+1F389 in UTF-8 bytes, but after it in UTF-16 code units.
	keys := []string{"z", "\uFF5E", "\U0001F389"}
	f := New()
	for _, k := range keys {
		f.Put(k, Entry{Source: k, SourceType: "local"})
	}
	data, err := f.encode()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	iEmoji := strings.Index(s, "\U0001F389")
	iTilde := strings.Index(s, "\uFF5E")
	iZ := strings.Index(s, `"z"`)
	if iZ >= iEmoji || iEmoji >= iTilde {
		t.Errorf("keys not in UTF-16 order: z=%d emoji=%d tilde=%d", iZ, iEmoji, iTilde)
	}
}

func TestLoadPreservesUnknownFieldsOnRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := `{"version":2,"future":{"a": [1, 2]},"skills":{"alpha":{"source":"s","sourceType":"local","computedHash":"h","vendor":"keep"}}}`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, `"future": {`) || !strings.Contains(s, `"a": [`) {
		t.Errorf("unknown top-level field lost:\n%s", s)
	}
	if !strings.Contains(s, `"vendor": "keep"`) {
		t.Errorf("unknown entry field lost:\n%s", s)
	}
}

func TestPutKeepsUnknownFieldsOfPreviousEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{"version":2,"skills":{"alpha":{"source":"s","sourceType":"local","computedHash":"h","vendor":"keep"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Put("alpha", Entry{Source: "new", SourceType: "local"})
	data, _ := f.encode()
	if !strings.Contains(string(data), `"vendor": "keep"`) {
		t.Errorf("Put dropped unknown field:\n%s", data)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(f.Skills) != 0 {
		t.Errorf("skills = %v, want none", f.Skills)
	}
}

func TestLoadRejectsOtherVersionsAndBrokenFiles(t *testing.T) {
	tests := map[string]string{
		"version 3":      `{"version":3,"skills":{}}`,
		"no version":     `{"skills":{}}`,
		"no skills":      `{"version":2}`,
		"not json":       `<<<<<<< HEAD`,
		"skills is null": `{"version":2,"skills":null}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Error("Load: want error")
			}
			if _, err := os.Stat(path); err != nil {
				t.Errorf("broken lock was touched: %v", err)
			}
		})
	}
}

func TestProjectLockHasNoTimestamps(t *testing.T) {
	f := New()
	f.Put("alpha", Entry{Source: "./src", SourceType: "local", ComputedHash: "abc"})
	data, err := f.encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"installedAt", "updatedAt"} {
		if strings.Contains(string(data), key) {
			t.Errorf("project lock contains %q; upstream v2 has no timestamps:\n%s", key, data)
		}
	}
}

func TestSubagentsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	f := New()
	f.Put("alpha", Entry{
		Source: "owner/repo", SourceType: "github", ComputedHash: "abc",
		Subagents: []string{"", "reviewer"},
	})
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"subagents": [`) || !strings.Contains(string(raw), `"",`) {
		t.Errorf("subagents field not written as an array:\n%s", raw)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := loaded.Get("alpha")
	if !ok {
		t.Fatal("alpha entry missing")
	}
	want := []string{"", "reviewer"}
	if !reflect.DeepEqual(entry.Subagents, want) {
		t.Errorf("Subagents = %v, want %v", entry.Subagents, want)
	}
}

func TestSubagentsOmittedWhenEmpty(t *testing.T) {
	f := New()
	f.Put("alpha", Entry{Source: "s", SourceType: "local", ComputedHash: "h"})
	data, err := f.encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "subagents") {
		t.Errorf("empty subagents field written:\n%s", data)
	}
}

func TestSubagentsSurviveAnUnrelatedRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	in := `{"version":2,"skills":{"alpha":{"source":"s","sourceType":"local","computedHash":"h","subagents":["","reviewer"]}}}`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, _ := f.Get("alpha")
	entry.ComputedHash = "new"
	f.Put("alpha", entry)
	if err := f.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"subagents": [`) || !strings.Contains(string(raw), `"reviewer"`) {
		t.Errorf("subagents field lost on rewrite:\n%s", raw)
	}
}
