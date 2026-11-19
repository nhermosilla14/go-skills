package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// newEveProject creates an Eve project with a reviewer and a writer subagent.
func newEveProject(t *testing.T) (string, Env) {
	t.Helper()
	project := t.TempDir()
	writeSourceFile(t, project, "package.json", `{"dependencies":{"eve":"1.0.0"}}`)
	writeSourceFile(t, project, "agent/keep", "x")
	for _, name := range []string{"reviewer", "writer"} {
		writeSourceFile(t, project, "agent/subagents/"+name+"/.keep", "x")
	}
	return project, addEnv(t, project)
}

// eveFlatContent renders what Eve's flat install of a source skill holds.
func eveFlatContent(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeSourceFile(t, dir, "SKILL.md", content)
	out, err := install.FlatContent(skill.Skill{Name: "alpha", Dir: dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestAddEveSubagentFlags(t *testing.T) {
	src := newSource(t)
	tests := []struct {
		name  string
		flags []string
		want  []string
		files []string
	}{
		{"root", []string{"root"}, []string{""}, []string{"agent/skills/flat.md"}},
		{"dot", []string{"."}, []string{""}, []string{"agent/skills/flat.md"}},
		{"named", []string{"reviewer"}, []string{"reviewer"},
			[]string{"agent/subagents/reviewer/skills/flat.md"}},
		{"root and reviewer", []string{"root", "reviewer"}, []string{"", "reviewer"},
			[]string{"agent/skills/flat.md", "agent/subagents/reviewer/skills/flat.md"}},
		{"unknown name", []string{"newcomer"}, []string{"newcomer"},
			[]string{"agent/subagents/newcomer/skills/flat.md"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project, env := newEveProject(t)
			rep, err := Add(AddOptions{Source: src, Subagents: tc.flags, Yes: true}, env)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			if rep.Failed() {
				t.Fatalf("results = %+v", rep.Results)
			}
			for _, rel := range tc.files {
				if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); err != nil {
					t.Errorf("%s missing: %v", rel, err)
				}
			}
			lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
			if err != nil {
				t.Fatal(err)
			}
			entry, ok := lock.Get("flat")
			if !ok {
				t.Fatalf("lock has no flat entry: %+v", lock.Skills)
			}
			if !reflect.DeepEqual(entry.Subagents, tc.want) {
				t.Errorf("subagents = %v, want %v", entry.Subagents, tc.want)
			}
			if len(tc.flags) == 1 && tc.flags[0] == "reviewer" {
				if _, err := os.Stat(filepath.Join(project, "agent", "skills", "flat.md")); !os.IsNotExist(err) {
					t.Errorf("the root agent got a file it was not selected for: %v", err)
				}
			}
		})
	}
}

func TestAddRejectsSubagentOutsideAnEveProject(t *testing.T) {
	project := t.TempDir()
	_, err := Add(AddOptions{Source: newSource(t), Subagents: []string{"root"}, Yes: true},
		addEnv(t, project))
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
}

func TestAddRejectsSubagentWithGlobal(t *testing.T) {
	project, env := newEveProject(t)
	_, err := Add(AddOptions{Source: newSource(t), Subagents: []string{"root"}, Global: true, Yes: true}, env)
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
	if _, statErr := os.Stat(filepath.Join(project, "agent", "skills", "flat.md")); !os.IsNotExist(statErr) {
		t.Errorf("a rejected run installed files: %v", statErr)
	}
}

func TestEveSubagentPromptScripted(t *testing.T) {
	project, env := newEveProject(t)
	env.IsTTY = true
	script := NewScripted(t, Answers{
		Agents: []string{"eve"}, Subagents: []string{"", "writer"}, Proceed: true,
	})
	rep, err := Add(AddOptions{Source: newSource(t)}, env, script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	asked := script.Asked()
	if len(asked) != 1 {
		t.Fatalf("asked %d times, want once", len(asked))
	}
	if !strings.Contains(strings.Join(ids(asked[0].Steps), ","), string(StepSubagents)) {
		t.Fatalf("steps = %v, want the subagent prompt", asked[0].Steps)
	}
	var choices []string
	for _, c := range asked[0].Subagents {
		choices = append(choices, c.Name)
	}
	if got, want := strings.Join(choices, ","), ",reviewer,writer"; got != want {
		t.Errorf("subagent choices = %q, want %q", got, want)
	}
	for _, rel := range []string{"agent/skills/flat.md", "agent/subagents/writer/skills/flat.md"} {
		if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(project, "agent", "subagents", "reviewer", "skills", "flat.md")); !os.IsNotExist(err) {
		t.Errorf("an unselected subagent got a file: %v", err)
	}
}

// writeEveFile writes one flat install file.
func writeEveFile(t *testing.T, project, rel, content string) string {
	t.Helper()
	writeSourceFile(t, project, rel, content)
	return filepath.Join(project, filepath.FromSlash(rel))
}

// TestCheckReadsEveFlatInstall verifies an Eve placement through the flat files the lock records,
// so a deleted or changed copy is a local change even when another copy is on disk.
func TestCheckReadsEveFlatInstall(t *testing.T) {
	project, env := newEveProject(t)
	body := "---\nname: alpha\ndescription: d\n---\none\n"
	root := writeEveFile(t, project, "agent/skills/alpha.md", body)
	reviewer := writeEveFile(t, project, "agent/subagents/reviewer/skills/alpha.md", body)
	digest, err := hash.SingleFile(root)
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": {
		Source: "./src", SourceType: "local",
		Subagents: []string{"", "reviewer"}, ComputedHash: digest,
	}})

	rep, err := Check(context.Background(), CheckOptions{}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("report = %+v, want the flat install to be current", rep)
	}

	// A copy the lock does not list is not the recorded placement: deleting the root file while a
	// subagent copy remains is a local change, not a current install.
	writeEveFile(t, project, "agent/subagents/writer/skills/alpha.md", body)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	rep, err = Check(context.Background(), CheckOptions{}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Modified) != 1 || rep.Modified[0] != "alpha" {
		t.Errorf("modified = %v, want the recorded placement reported", rep.Modified)
	}
	if _, err := os.Stat(reviewer); err != nil {
		t.Errorf("the check touched an installed file: %v", err)
	}
}

func TestUpdateReappliesTheEvePlacement(t *testing.T) {
	project, env := newEveProject(t)
	body := "---\nname: alpha\ndescription: d\n---\none\n"
	root := writeEveFile(t, project, "agent/skills/alpha.md", body)
	reviewer := writeEveFile(t, project, "agent/subagents/reviewer/skills/alpha.md", body)
	digest, err := hash.SingleFile(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := updateEntry(t, "", lockfile.Entry{
		SkillPath: "skills/alpha/SKILL.md", Subagents: []string{"", "reviewer"},
	})
	entry.ComputedHash = digest
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})

	upstream := updateSource(t, "two\n")
	runner := newUpdateRunner()
	runner.trees[updateRepo] = upstream
	runner.commits[updateRepo] = strings.Repeat("a", 40)

	rep, err := Update(UpdateOptions{Project: true, Runner: runner}, env, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want one update", rep.Results)
	}
	for _, file := range []string{root, reviewer} {
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if !strings.Contains(string(got), "two") {
			t.Errorf("%s = %q, want the new content", file, got)
		}
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := lock.Get("alpha")
	if want := []string{"", "reviewer"}; !reflect.DeepEqual(updated.Subagents, want) {
		t.Errorf("subagents = %v, want %v", updated.Subagents, want)
	}

	// A second run finds the same content and leaves the files alone.
	before, err := os.ReadFile(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	rep, err = Update(UpdateOptions{Project: true, Runner: runner}, env, nil)
	if err != nil {
		t.Fatalf("second Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateCurrent {
		t.Fatalf("results = %+v, want current", rep.Results)
	}
	after, err := os.ReadFile(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("an unchanged update rewrote the lock")
	}
}

func TestRemoveEveSubagentsLeavesOthersAlone(t *testing.T) {
	project, env := newEveProject(t)
	body := "---\nname: alpha\ndescription: d\n---\nbody\n"
	root := writeEveFile(t, project, "agent/skills/alpha.md", body)
	reviewer := writeEveFile(t, project, "agent/subagents/reviewer/skills/alpha.md", body)
	writer := writeEveFile(t, project, "agent/subagents/writer/skills/alpha.md", body)
	digest, err := hash.SingleFile(root)
	if err != nil {
		t.Fatal(err)
	}
	entry := updateEntry(t, "", lockfile.Entry{
		SkillPath: "skills/alpha/SKILL.md", Subagents: []string{"", "reviewer"},
	})
	entry.ComputedHash = digest
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})

	rep, err := Remove(RemoveOptions{Skills: []string{"alpha"}, Yes: true}, env)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	for _, file := range []string{root, reviewer} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("%s survived removal: %v", file, err)
		}
	}
	if _, err := os.Stat(writer); err != nil {
		t.Errorf("an unrecorded subagent copy was removed: %v", err)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lock.Get("alpha"); ok {
		t.Error("lock entry survived the removal")
	}
}

func TestListShowsTheEvePlacement(t *testing.T) {
	project, env := newEveProject(t)
	body := "---\nname: alpha\ndescription: d\n---\nbody\n"
	root := writeEveFile(t, project, "agent/skills/alpha.md", body)
	writeEveFile(t, project, "agent/subagents/reviewer/skills/alpha.md", body)
	digest, err := hash.SingleFile(root)
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": updateEntry(t, "",
		lockfile.Entry{SkillPath: "skills/alpha/SKILL.md", Subagents: []string{"", "reviewer"},
			ComputedHash: digest})})

	rep, err := List(ListOptions{}, env)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rep.Skills) != 1 {
		t.Fatalf("skills = %d, want the copies merged: %+v", len(rep.Skills), rep.Skills)
	}
	info := rep.Skills[0]
	if info.Status != StatusTracked {
		t.Errorf("status = %q, want tracked", info.Status)
	}
	if want := []string{"Eve"}; !reflect.DeepEqual(info.Agents, want) {
		t.Errorf("agents = %v, want %v", info.Agents, want)
	}
	if want := []string{"", "reviewer"}; !reflect.DeepEqual(info.Subagents, want) {
		t.Errorf("subagents = %v, want %v", info.Subagents, want)
	}
	if info.Path != root {
		t.Errorf("path = %q, want the root file %q", info.Path, root)
	}
}

func TestRestoreEveFlat(t *testing.T) {
	project, env := newEveProject(t)
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": updateEntry(t, "",
		lockfile.Entry{SkillPath: "skills/alpha/SKILL.md", Subagents: []string{"", "reviewer"}})})

	rep, err := Restore(RestoreOptions{Runner: restoreRunner(t, src)}, env)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the Eve entry restored", rep.Results)
	}
	want := eveFlatContent(t, "---\nname: alpha\ndescription: d\n---\none\n")
	for _, rel := range []string{"agent/skills/alpha.md", "agent/subagents/reviewer/skills/alpha.md"} {
		got, err := os.ReadFile(filepath.Join(project, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s missing: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want the reduced flat file %q", rel, got, want)
		}
	}

	// The restored content matches the lock: a second run skips, and frozen mode accepts it.
	before := readFile(t, filepath.Join(project, lockfile.FileName))
	rep, err = Restore(RestoreOptions{Runner: restoreRunner(t, src)}, env)
	if err != nil {
		t.Fatalf("second Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreSkipped {
		t.Fatalf("results = %+v, want a skip", rep.Results)
	}
	env.AgentEnv.Vars["CI"] = "1"
	rep, err = Restore(RestoreOptions{Runner: restoreRunner(t, src)}, env)
	if err != nil {
		t.Fatalf("frozen Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreSkipped {
		t.Fatalf("results = %+v, want a frozen skip", rep.Results)
	}
	if after := readFile(t, filepath.Join(project, lockfile.FileName)); after != before {
		t.Error("a skipped Eve restore rewrote the lock")
	}
}

func TestFrozenRestoreRejectsAMissingEveFile(t *testing.T) {
	project, env := newEveProject(t)
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": updateEntry(t, "",
		lockfile.Entry{SkillPath: "skills/alpha/SKILL.md", Subagents: []string{"", "reviewer"},
			ComputedHash: "abc"})})
	if _, err := Restore(RestoreOptions{Runner: restoreRunner(t, src)}, env); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := os.Remove(filepath.Join(project, "agent", "subagents", "reviewer", "skills", "alpha.md")); err != nil {
		t.Fatal(err)
	}
	env.AgentEnv.Vars["CI"] = "1"
	_, err := Restore(RestoreOptions{Runner: restoreRunner(t, src)}, env)
	if err == nil || !strings.Contains(err.Error(), "alpha is missing") {
		t.Fatalf("err = %v, want a frozen missing-file failure", err)
	}
}

func TestFrozenRestoreAcceptsAMixedEveInstall(t *testing.T) {
	project, env := newEveProject(t)
	src := restoreSource(t)
	canonical := filepath.Join(project, ".agents", "skills", "alpha")
	writeSourceFile(t, project, ".agents/skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\none\n")
	body := eveFlatContent(t, "---\nname: alpha\ndescription: d\n---\none\n")
	writeEveFile(t, project, "agent/skills/alpha.md", body)
	writeEveFile(t, project, "agent/subagents/reviewer/skills/alpha.md", body)
	digest, err := hash.Dir(canonical)
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": updateEntry(t, "",
		lockfile.Entry{SkillPath: "skills/alpha/SKILL.md", Subagents: []string{"", "reviewer"},
			ComputedHash: digest})})

	env.AgentEnv.Vars["CI"] = "1"
	rep, err := Restore(RestoreOptions{Runner: restoreRunner(t, src), Agents: []string{"opencode", "eve"}}, env)
	if err != nil {
		t.Fatalf("frozen Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreSkipped {
		t.Fatalf("results = %+v, want a skip", rep.Results)
	}
}
