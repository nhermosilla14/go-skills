package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// fakeGitRunner returns canned output and records the commands it was asked to run.
type fakeGitRunner struct {
	out  source.Output
	err  error
	cmds []source.Command
}

func (f *fakeGitRunner) Run(_ context.Context, cmd source.Command) (source.Output, error) {
	f.cmds = append(f.cmds, cmd)
	return f.out, f.err
}

// installedLockEntry writes one skill and returns a matching lock entry with its content hash.
func installedLockEntry(t *testing.T, project, rel, name string, entry lockfile.Entry) lockfile.Entry {
	t.Helper()
	dir := writeProjectSkill(t, project, rel, name, "d")
	digest, err := hash.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry.ComputedHash = digest
	return entry
}

func TestCheckUpToDate(t *testing.T) {
	project := t.TempDir()
	commit := strings.Repeat("a", 40)
	entry := installedLockEntry(t, project, ".agents/skills/alpha", "alpha", lockfile.Entry{
		Source: "file:///repo", SourceURL: "file:///repo", SourceType: "git", Ref: "main", Commit: commit,
	})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})

	runner := &fakeGitRunner{out: source.Output{Stdout: commit + "\trefs/heads/main\n"}}
	rep, err := Check(context.Background(), CheckOptions{Runner: runner}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Errorf("report = %+v, want clean", rep)
	}
	if len(runner.cmds) != 1 {
		t.Fatalf("commands = %d, want one ls-remote", len(runner.cmds))
	}
	args := strings.Join(runner.cmds[0].Args, " ")
	for _, want := range []string{"ls-remote", "file:///repo", "main"} {
		if !strings.Contains(args, want) {
			t.Errorf("args = %q, want %q", args, want)
		}
	}
}

func TestCheckReportsAnUpdate(t *testing.T) {
	project := t.TempDir()
	commit := strings.Repeat("a", 40)
	entry := installedLockEntry(t, project, ".agents/skills/alpha", "alpha", lockfile.Entry{
		Source: "file:///repo", SourceURL: "file:///repo", SourceType: "git", Commit: commit,
	})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})

	head := strings.Repeat("b", 40)
	runner := &fakeGitRunner{out: source.Output{Stdout: head + "\tHEAD\n"}}
	rep, err := Check(context.Background(), CheckOptions{Runner: runner}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Updates) != 1 || rep.Updates[0] != "alpha" {
		t.Errorf("updates = %v, want alpha", rep.Updates)
	}
	if !rep.Failed() {
		t.Error("report is clean, want a failure exit")
	}
}

func TestCheckReportsLocalChanges(t *testing.T) {
	project := t.TempDir()
	dir := writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	digest, err := hash.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "./src", SourceType: "local", ComputedHash: digest},
	})
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillBody("alpha", "first", false)+"more\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Check(context.Background(), CheckOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Modified) != 1 || rep.Modified[0] != "alpha" {
		t.Errorf("modified = %v, want alpha", rep.Modified)
	}
}

func TestCheckUnreachableSourceIsAFailure(t *testing.T) {
	project := t.TempDir()
	entry := installedLockEntry(t, project, ".agents/skills/alpha", "alpha", lockfile.Entry{
		Source: "file:///repo", SourceURL: "file:///repo", SourceType: "git",
	})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})

	runner := &fakeGitRunner{
		out: source.Output{Stderr: "fatal: could not read from remote"},
		err: errors.New("exit status 128"),
	}
	rep, err := Check(context.Background(), CheckOptions{Runner: runner}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Failures) != 1 {
		t.Fatalf("failures = %v, want one", rep.Failures)
	}
	if !strings.Contains(rep.Failures[0], "alpha") || !strings.Contains(rep.Failures[0], "could not read") {
		t.Errorf("failure = %q", rep.Failures[0])
	}
}

func TestCheckLocalSourceIsNeverOutdated(t *testing.T) {
	project := t.TempDir()
	entry := installedLockEntry(t, project, ".agents/skills/alpha", "alpha", lockfile.Entry{
		Source: "./src", SourceType: "local", Commit: strings.Repeat("c", 40),
	})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})

	runner := &fakeGitRunner{}
	rep, err := Check(context.Background(), CheckOptions{Runner: runner}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Errorf("report = %+v, want clean", rep)
	}
	if len(runner.cmds) != 0 {
		t.Errorf("commands = %v, want none for a local source", runner.cmds)
	}
}

func TestCheckHashLessEntryIsALocalChange(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "./src", SourceType: "local"},
	})

	rep, err := Check(context.Background(), CheckOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Modified) != 1 || rep.Modified[0] != "alpha" {
		t.Errorf("modified = %v, want a hash-less entry reported as changed", rep.Modified)
	}
}

func TestCheckFiltersByName(t *testing.T) {
	project := t.TempDir()
	commit := strings.Repeat("a", 40)
	entries := map[string]lockfile.Entry{}
	for _, name := range []string{"alpha", "beta"} {
		entries[name] = installedLockEntry(t, project, filepath.Join(".agents", "skills", name), name, lockfile.Entry{
			Source: "file:///repo", SourceURL: "file:///repo", SourceType: "git", Commit: commit,
		})
	}
	saveProjectLock(t, project, entries)

	runner := &fakeGitRunner{out: source.Output{Stdout: commit + "\tHEAD\n"}}
	rep, err := Check(context.Background(), CheckOptions{Skills: []string{"ALPHA"}, Runner: runner}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Errorf("report = %+v, want clean", rep)
	}
	if len(runner.cmds) != 1 {
		t.Errorf("commands = %d, want only the selected skill", len(runner.cmds))
	}

	rep, err = Check(context.Background(), CheckOptions{Skills: []string{"nope"}, Runner: runner}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(rep.Failures) != 1 || !strings.Contains(rep.Failures[0], "nope") {
		t.Errorf("failures = %v, want the unmatched name", rep.Failures)
	}
}

func TestCheckDoesNotCheckMissingSkills(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"gone": {Source: "./src", SourceType: "local", ComputedHash: strings.Repeat("d", 64)},
	})

	rep, err := Check(context.Background(), CheckOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if rep.Failed() {
		t.Errorf("report = %+v, want nothing to check", rep)
	}
}

func TestCheckRejectsCorruptLock(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, lockfile.FileName), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(context.Background(), CheckOptions{}, testEnv(project)); err == nil {
		t.Fatal("Check succeeded with a corrupt lock")
	}
}
