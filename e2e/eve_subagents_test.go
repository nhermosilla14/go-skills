package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/app"
)

// hasStep reports whether the flow asks step.
func hasStep(steps []app.StepID, want app.StepID) bool {
	for _, step := range steps {
		if step == want {
			return true
		}
	}
	return false
}

// newEveProject creates an Eve project with a reviewer and a writer subagent.
func newEveProject(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	writeFixtureFile(t, project, "package.json", `{"dependencies":{"eve":"1.0.0"}}`)
	writeFixtureFile(t, project, "agent/keep", "x")
	for _, name := range []string{"reviewer", "writer"} {
		writeFixtureFile(t, project, "agent/subagents/"+name+"/.keep", "x")
	}
	return project
}

// TestE2EEveSubagents installs for the root agent and one subagent, updates the placement, lists it
// and removes only the recorded files.
func TestE2EEveSubagents(t *testing.T) {
	fx := newGitFixture(t)
	project := newEveProject(t)

	res := run(t, project, "add", fx.url+"#v1", "-s", "alpha",
		"--subagent", "root", "--subagent", "reviewer", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	body := "---\nname: alpha\ndescription: d\n---\nversion one\n"
	root := filepath.Join(project, "agent", "skills", "alpha.md")
	reviewer := filepath.Join(project, "agent", "subagents", "reviewer", "skills", "alpha.md")
	writer := filepath.Join(project, "agent", "subagents", "writer", "skills", "alpha.md")
	for _, file := range []string{root, reviewer} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s missing: %v", file, err)
		}
		if string(raw) != body {
			t.Errorf("%s = %q, want the reduced flat file %q", file, raw, body)
		}
	}
	if _, err := os.Stat(writer); !os.IsNotExist(err) {
		t.Errorf("the unselected writer got a file: %v", err)
	}
	lock := readLock(t, project)
	entry, ok := lock.Skills["alpha"]
	if !ok {
		t.Fatalf("lock has no alpha entry: %+v", lock.Skills)
	}
	if want := []string{"", "reviewer"}; strings.Join(entry.Subagents, ",") != strings.Join(want, ",") {
		t.Errorf("subagents = %v, want %v", entry.Subagents, want)
	}

	// Move the tag: update reinstalls the recorded placement.
	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)
	res = run(t, project, "update", "-p")
	if res.exit != 0 {
		t.Fatalf("update exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, file := range []string{root, reviewer} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s missing: %v", file, err)
		}
		if !strings.Contains(string(raw), "version two") {
			t.Errorf("%s = %q, want the new content", file, raw)
		}
	}
	if updated := readLock(t, project).Skills["alpha"]; strings.Join(updated.Subagents, ",") != ",reviewer" {
		t.Errorf("subagents after update = %v, want the placement kept", updated.Subagents)
	}

	res = run(t, project, "list")
	if res.exit != 0 {
		t.Fatalf("list exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "Agents: Eve (root, reviewer)") {
		t.Errorf("list stdout misses the Eve placement:\n%s", res.stdout)
	}

	// A copy the lock does not record belongs to another placement and stays.
	if err := os.MkdirAll(filepath.Dir(writer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writer, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res = run(t, project, "remove", "alpha", "-y")
	if res.exit != 0 {
		t.Fatalf("remove exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, file := range []string{root, reviewer} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("%s survived removal: %v", file, err)
		}
	}
	if _, err := os.Stat(writer); err != nil {
		t.Errorf("the unrecorded writer copy was removed: %v", err)
	}
	if _, ok := readLock(t, project).Skills["alpha"]; ok {
		t.Error("lock entry survived the removal")
	}
}

// TestE2EEveSubagentPrompt drives the interactive flow with a scripted prompter.
func TestE2EEveSubagentPrompt(t *testing.T) {
	project := newEveProject(t)
	src := t.TempDir()
	writeFixtureFile(t, src, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nbody\n")

	env := app.Env{
		WorkDir: project,
		IsTTY:   true,
		Now:     func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		AgentEnv: agents.Environment{
			GOOS: "linux", Home: t.TempDir(), Cwd: project, Vars: map[string]string{},
			ApplicationsDir: t.TempDir(), EtcDir: t.TempDir(),
		},
	}
	script := app.NewScripted(t, app.Answers{
		Agents: []string{"eve"}, Subagents: []string{"", "writer"}, Proceed: true,
	})
	rep, err := app.Add(app.AddOptions{Source: src}, env, script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	asked := script.Asked()
	if len(asked) != 1 || !hasStep(asked[0].Steps, app.StepSubagents) {
		t.Fatalf("asked = %+v, want the subagent prompt", asked)
	}
	for _, rel := range []string{"agent/skills/alpha.md", "agent/subagents/writer/skills/alpha.md"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
}

// TestE2ERestoreEveFlat rebuilds the flat files from the lock and accepts an unchanged install in
// frozen mode.
func TestE2ERestoreEveFlat(t *testing.T) {
	fx := newGitFixture(t)
	project := newEveProject(t)

	res := run(t, project, "add", fx.url+"#v1", "-s", "alpha",
		"--subagent", "root", "--subagent", "reviewer", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	body := "---\nname: alpha\ndescription: d\n---\nversion one\n"
	root := filepath.Join(project, "agent", "skills", "alpha.md")
	reviewer := filepath.Join(project, "agent", "subagents", "reviewer", "skills", "alpha.md")
	for _, file := range []string{root, reviewer} {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}

	res = run(t, project, "install")
	if res.exit != 0 {
		t.Fatalf("install exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "Restored 1") {
		t.Errorf("stderr misses the restore:\n%s", res.stderr)
	}
	for _, file := range []string{root, reviewer} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s was not restored: %v", file, err)
		}
		if string(raw) != body {
			t.Errorf("%s = %q, want the reduced flat file %q", file, raw, body)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("restoring an Eve entry wrote a canonical copy: %v", err)
	}

	res = runEnv(t, project, []string{"CI=1"}, "install")
	if res.exit != 0 {
		t.Fatalf("frozen install exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "Restored 0, relinked 0, skipped 1, failed 0") {
		t.Errorf("frozen install stderr misses the skip:\n%s", res.stderr)
	}
}
