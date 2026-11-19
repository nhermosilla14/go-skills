package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
)

func TestWriteListHuman(t *testing.T) {
	rep := app.ListReport{
		Scope: "project",
		Skills: []app.SkillInfo{
			{Name: "alpha", Path: "/p/.agents/skills/alpha", Agents: []string{"Claude Code", "Cursor"},
				Source: "owner/repo", Status: app.StatusTracked},
			{Name: "beta", Path: "/p/.agents/skills/beta", Agents: []string{},
				Source: "other/repo", RenamedFrom: "orig", Status: app.StatusModified},
			{Name: "gone", Agents: []string{}, Source: "owner/gone", Status: app.StatusMissing},
		},
	}
	var out bytes.Buffer
	WriteList(&out, rep)
	want := `Project Skills

alpha /p/.agents/skills/alpha
  Agents: Claude Code, Cursor  Source: owner/repo
beta  /p/.agents/skills/beta
  Agents: not linked  Source: other/repo (renamed from orig) modified (differs from the lock)

Missing (in the lock file, not on disk)
  gone  Source: owner/gone
`
	if out.String() != want {
		t.Errorf("output =\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteListTruncatesAgents(t *testing.T) {
	rep := app.ListReport{Scope: "project", Skills: []app.SkillInfo{{
		Name: "alpha", Path: "/p/alpha",
		Agents: []string{"A", "B", "C", "D", "E", "F", "G"},
		Source: "owner/repo", Status: app.StatusTracked,
	}}}
	var out bytes.Buffer
	WriteList(&out, rep)
	if !strings.Contains(out.String(), "Agents: A, B, C, D, E +2 more") {
		t.Errorf("output = %q, want the collapsed agent list", out.String())
	}
}

func TestWriteListEmptyProject(t *testing.T) {
	var out bytes.Buffer
	WriteList(&out, app.ListReport{Scope: "project"})
	want := "Project Skills\n\nNo project skills found.\nTry listing global skills with -g\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestWriteListSanitizesUntrustedText(t *testing.T) {
	rep := app.ListReport{Scope: "project", Skills: []app.SkillInfo{{
		Name: "ev\nil", Path: "/p/\x1b[31mred", Agents: []string{"Claude\nCode"},
		Source: "owner/repo\nFORGED", Status: app.StatusUntracked,
	}}}
	var out bytes.Buffer
	WriteList(&out, rep)
	if strings.ContainsAny(out.String(), "\x1b\r") {
		t.Errorf("output carries terminal escapes: %q", out.String())
	}
	if !strings.Contains(out.String(), "ev il") || !strings.Contains(out.String(), "Claude Code") {
		t.Errorf("output = %q, want sanitized text", out.String())
	}
	if !strings.Contains(out.String(), "Source: untracked") {
		t.Errorf("output = %q, want the untracked source", out.String())
	}
}

func TestListJSONShape(t *testing.T) {
	rep := app.ListReport{Scope: "project", Skills: []app.SkillInfo{
		{Name: "alpha", Path: "/p/.agents/skills/alpha", Agents: []string{"Claude Code"},
			Source: "owner/repo", SourceURL: "https://github.com/owner/repo", SourceType: "github",
			Status: app.StatusTracked},
		{Name: "gone", Agents: []string{}, Source: "owner/repo", SourceType: "github", Status: app.StatusMissing},
	}}
	var out bytes.Buffer
	if err := WriteListJSON(&out, ListJSON(rep)); err != nil {
		t.Fatalf("WriteListJSON: %v", err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("JSON carries ANSI escapes")
	}
	var doc []map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if len(doc) != 2 {
		t.Fatalf("document = %v, want two entries", doc)
	}
	alpha := doc[0]
	for key, want := range map[string]any{
		"name": "alpha", "path": "/p/.agents/skills/alpha", "scope": "project",
		"source": "owner/repo", "sourceUrl": "https://github.com/owner/repo",
		"sourceType": "github", "renamedFrom": nil, "status": "tracked",
	} {
		if alpha[key] != want {
			t.Errorf("alpha[%q] = %v, want %v", key, alpha[key], want)
		}
	}
	if agents, _ := alpha["agents"].([]any); len(agents) != 1 || agents[0] != "Claude Code" {
		t.Errorf("alpha agents = %v", alpha["agents"])
	}
	gone := doc[1]
	if gone["path"] != nil || gone["status"] != "missing" {
		t.Errorf("gone = %v, want a null path and the missing status", gone)
	}
	if agents, _ := gone["agents"].([]any); agents == nil || len(agents) != 0 {
		t.Errorf("gone agents = %v, want an empty array", gone["agents"])
	}
	if !strings.Contains(out.String(), `"path": null`) || !strings.Contains(out.String(), `"renamedFrom": null`) {
		t.Errorf("output misses null fields:\n%s", out.String())
	}
}

func TestListJSONEmptyIsOneLineArray(t *testing.T) {
	var out bytes.Buffer
	if err := WriteListJSON(&out, nil); err != nil {
		t.Fatalf("WriteListJSON: %v", err)
	}
	if out.String() != "[]\n" {
		t.Errorf("output = %q, want [] on one line", out.String())
	}
}

func TestWriteCheck(t *testing.T) {
	var out bytes.Buffer
	WriteCheck(&out, app.CheckReport{
		Updates:  []string{"alpha"},
		Modified: []string{"beta"},
		Failures: []string{"gamma: git ls-remote failed"},
	})
	want := "↑ alpha has an update\n✎ beta has local changes\n✗ gamma: git ls-remote failed\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}

	out.Reset()
	WriteCheck(&out, app.CheckReport{})
	if out.String() != "✓ All project skills are up to date\n" {
		t.Errorf("clean output = %q", out.String())
	}

	out.Reset()
	WriteCheck(&out, app.CheckReport{Failures: []string{"alpha\x1b[31m: boom"}})
	if strings.Contains(out.String(), "\x1b") {
		t.Errorf("check output carries escapes: %q", out.String())
	}
}
