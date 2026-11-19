package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findListed returns the JSON entry for a skill name.
func findListed(t *testing.T, doc []map[string]any, name string) map[string]any {
	t.Helper()
	for _, entry := range doc {
		if entry["name"] == name {
			return entry
		}
	}
	t.Fatalf("no %q entry in %v", name, doc)
	return nil
}

func TestE2EManagementRoundTrip(t *testing.T) {
	project := t.TempDir()
	source := sourceDir(t)
	res := run(t, project, "add", source, "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}

	// One skill drifts from the lock, one disappears from disk, one was never tracked.
	flat := filepath.Join(project, ".agents", "skills", "flat-skill", "SKILL.md")
	raw, err := os.ReadFile(flat)
	if err != nil {
		t.Fatalf("flat-skill missing: %v", err)
	}
	if err := os.WriteFile(flat, append(raw, []byte("local edit\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(project, ".agents", "skills", "nested-skill"),
		filepath.Join(project, ".claude", "skills", "nested-skill"),
	} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	extra := filepath.Join(project, ".agents", "skills", "extra")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "SKILL.md"),
		[]byte("---\nname: extra\ndescription: untracked skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res = run(t, project, "list")
	if res.exit != 0 {
		t.Fatalf("list exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{
		"Project Skills",
		"modified (differs from the lock)",
		"Source: untracked",
		"Missing (in the lock file, not on disk)",
		"nested-skill",
		"extra",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("list stdout misses %q:\n%s", want, res.stdout)
		}
	}
	if strings.Contains(res.stderr, "error") {
		t.Errorf("list stderr carries an error:\n%s", res.stderr)
	}

	res = run(t, project, "list", "--json")
	if res.exit != 0 {
		t.Fatalf("list --json exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stdout, "\x1b[") {
		t.Errorf("JSON carries ANSI escapes:\n%s", res.stdout)
	}
	doc := decodeDocument(t, res.stdout)
	if len(doc) != 3 {
		t.Fatalf("document has %d entries, want extra, flat-skill and nested-skill:\n%s", len(doc), res.stdout)
	}
	drifted := findListed(t, doc, "flat-skill")
	if drifted["status"] != "modified" {
		t.Errorf("flat-skill = %v, want modified", drifted)
	}
	untracked := findListed(t, doc, "extra")
	if untracked["status"] != "untracked" {
		t.Errorf("extra = %v, want untracked", untracked)
	}
	if untracked["source"] != nil || untracked["sourceType"] != nil {
		t.Errorf("extra = %v, want null lock fields", untracked)
	}
	missing := findListed(t, doc, "nested-skill")
	if missing["status"] != "missing" || missing["path"] != nil {
		t.Errorf("nested-skill = %v, want missing with a null path", missing)
	}
	if agents, _ := missing["agents"].([]any); len(agents) != 0 {
		t.Errorf("nested-skill agents = %v, want an empty array", missing["agents"])
	}

	before, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	res = run(t, project, "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "✎ flat-skill has local changes") {
		t.Errorf("check stdout misses the local change:\n%s", res.stdout)
	}
	after, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("check rewrote skills-lock.json")
	}

	res = run(t, project, "list", "-g")
	if res.exit != 2 {
		t.Fatalf("list -g exit = %d, want 2\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "global scope is not supported yet") {
		t.Errorf("list -g stderr = %q", res.stderr)
	}
}

func TestE2ECheckReportsGitUpdates(t *testing.T) {
	fx := newGitFixture(t)
	project := t.TempDir()
	res := run(t, project, "add", fx.url+"#v1", "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}

	res = run(t, project, "check")
	if res.exit != 0 {
		t.Fatalf("check exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "✓ All project skills are up to date") {
		t.Errorf("check stdout misses the clean summary:\n%s", res.stdout)
	}

	// The recorded ref moves upstream, so both skills have an update.
	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)

	res = run(t, project, "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{"↑ alpha has an update", "↑ beta has an update"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("check stdout misses %q:\n%s", want, res.stdout)
		}
	}

	res = run(t, project, "check", "alpha")
	if res.exit != 1 || !strings.Contains(res.stdout, "↑ alpha has an update") {
		t.Errorf("targeted check exit = %d, stdout = %q", res.exit, res.stdout)
	}
	if strings.Contains(res.stdout, "beta") {
		t.Errorf("targeted check checked another skill:\n%s", res.stdout)
	}

	res = run(t, project, "check", "nope")
	if res.exit != 1 || !strings.Contains(res.stdout, "nope") {
		t.Errorf("unmatched check exit = %d, stdout = %q", res.exit, res.stdout)
	}
}
