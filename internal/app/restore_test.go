package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// restoreRepo is the second file:// source used by the restore isolation test.
const restoreRepo = "file:///other"

// restoreSource creates a source with the alpha and beta skills, both at "version one".
func restoreSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeSourceFile(t, src, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\none\n")
	writeSourceFile(t, src, "skills/beta/SKILL.md", "---\nname: beta\ndescription: d\n---\none\n")
	return src
}

// restoreRunner fetches the prepared source under both test URLs.
func restoreRunner(t *testing.T, src string) *updateRunner {
	t.Helper()
	runner := newUpdateRunner()
	runner.trees[updateRepo] = src
	runner.trees[restoreRepo] = src
	runner.commits[updateRepo] = strings.Repeat("a", 40)
	runner.commits[restoreRepo] = strings.Repeat("a", 40)
	return runner
}

// saveGlobalAgents records the last agent selection in the global lock of env.
func saveGlobalAgents(t *testing.T, env agents.Environment, ids []string) {
	t.Helper()
	lock := lockfile.NewGlobal()
	lock.SetLastSelectedAgents(ids)
	if err := lock.Save(agents.GlobalLockPath(env)); err != nil {
		t.Fatalf("saving the global lock: %v", err)
	}
}

func TestRestoreGroupsBySourceAndCommit(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
		"beta":  updateEntry(t, "", lockfile.Entry{SkillPath: "skills/beta/SKILL.md"}),
	})
	runner := restoreRunner(t, src)

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(runner.clones) != 1 {
		t.Errorf("clones = %v, want one checkout for the shared source and commit", runner.clones)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %+v, want both skills", rep.Results)
	}
	for _, res := range rep.Results {
		if res.Status != RestoreRestored {
			t.Errorf("%s = %v, want restored", res.Name, res.Status)
		}
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Stat(filepath.Join(project, ".agents", "skills", name, "SKILL.md")); err != nil {
			t.Errorf("%s was not restored: %v", name, err)
		}
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	alpha, _ := lock.Get("alpha")
	want, err := hash.Dir(filepath.Join(project, ".agents", "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if alpha.ComputedHash != want {
		t.Errorf("computedHash = %q, want the restored hash %q", alpha.ComputedHash, want)
	}
	if rep.LockPath == "" {
		t.Error("LockPath is empty, want the filled hashes written")
	}
}

func TestRestoreGroupsByPinnedCommit(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	first := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
	second := updateEntry(t, "", lockfile.Entry{
		SkillPath: "skills/beta/SKILL.md", Commit: strings.Repeat("b", 40),
	})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": first, "beta": second})
	runner := restoreRunner(t, src)

	if _, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner},
		updateTestEnv(t, project)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(runner.clones) != 2 {
		t.Errorf("clones = %v, want one checkout per pinned commit", runner.clones)
	}
}

func TestRestoreRestoresAMissingSkill(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	entry := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
	digest, err := hash.Dir(filepath.Join(src, "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	entry.ComputedHash = digest
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
	before := readFile(t, filepath.Join(project, lockfile.FileName))

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: restoreRunner(t, src)},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the missing folder reinstalled", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("the canonical copy was not restored: %v", err)
	}
	if after := readFile(t, filepath.Join(project, lockfile.FileName)); after != before {
		t.Error("restoring correct content rewrote the lock")
	}
}

func TestRestoreSkipsMatchingContentAndKeepsTheLock(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	env := updateTestEnv(t, project)
	runner := restoreRunner(t, src)

	if _, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner}, env); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	before := readFile(t, filepath.Join(project, lockfile.FileName))

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner}, env)
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreSkipped {
		t.Fatalf("results = %+v, want a skip", rep.Results)
	}
	if rep.LockPath != "" {
		t.Errorf("LockPath = %q, want no lock write", rep.LockPath)
	}
	if after := readFile(t, filepath.Join(project, lockfile.FileName)); after != before {
		t.Error("an unchanged restore rewrote the lock")
	}
}

func TestRestoreRepairsMissingLinks(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	env := updateTestEnv(t, project)
	runner := restoreRunner(t, src)

	if _, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner}, env); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	link := filepath.Join(project, ".claude", "skills", "alpha")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner}, env)
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRelinked {
		t.Fatalf("results = %+v, want a relink", rep.Results)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("link was not repaired: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symlink", link)
	}
}

func TestRestoreForceReinstalls(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	env := updateTestEnv(t, project)
	runner := restoreRunner(t, src)

	if _, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner}, env); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Force: true, Runner: runner}, env)
	if err != nil {
		t.Fatalf("forced restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want a forced reinstall", rep.Results)
	}
}

func TestRestoreReinstallsChangedContent(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	installed := installAlpha(t, project, ".agents/skills/alpha", "locally edited\n")
	entry := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
	locked, err := hash.Dir(filepath.Join(src, "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	entry.ComputedHash = locked
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
	runner := restoreRunner(t, src)

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want a reinstall", rep.Results)
	}
	if got := readFile(t, filepath.Join(installed, "SKILL.md")); !strings.Contains(got, "one") {
		t.Errorf("installed SKILL.md = %q, want the locked content", got)
	}
}

func TestRestoreCopyModeEntry(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{
			SkillPath: "skills/alpha/SKILL.md", InstallMode: "copy",
		}),
	})
	runner := restoreRunner(t, src)

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want a restored copy", rep.Results)
	}
	for _, dir := range []string{
		filepath.Join(project, ".agents", "skills", "alpha"),
		filepath.Join(project, ".claude", "skills", "alpha"),
	} {
		info, err := os.Lstat(dir)
		if err != nil {
			t.Fatalf("%s was not restored: %v", dir, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			t.Errorf("%s is not a plain directory", dir)
		}
	}
}

func TestRestoreKeepsARenamedSkill(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha-new": updateEntry(t, "", lockfile.Entry{
			SkillPath: "skills/alpha/SKILL.md", SkillName: "alpha",
		}),
	})
	runner := restoreRunner(t, src)

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Name != "alpha-new" {
		t.Fatalf("results = %+v, want the installed name", rep.Results)
	}
	installed := filepath.Join(project, ".agents", "skills", "alpha-new", "SKILL.md")
	if got := readFile(t, installed); !strings.Contains(got, "name: alpha-new") {
		t.Errorf("installed SKILL.md = %q, want the rename", got)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := lock.Get("alpha-new")
	if !ok || entry.SkillName != "alpha" {
		t.Errorf("entry = %+v, want the upstream name preserved", entry)
	}
}

func TestRestoreReportsUnsupportedSourceTypes(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"local":   {Source: "./src", SourceType: "local"},
		"down":    {Source: "https://example.com/x.md", SourceType: "download"},
		"known":   {Source: "https://example.com", SourceType: "well-known"},
		"package": {Source: "pkg", SourceType: "node_modules"},
	})
	runner := newUpdateRunner()

	rep, err := Restore(RestoreOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Unrestorable) != 4 {
		t.Fatalf("unrestorable = %+v, want every entry", rep.Unrestorable)
	}
	if rep.Failed() {
		t.Errorf("report = %+v, want an unsupported-only run to succeed", rep)
	}
	if len(runner.clones) != 0 || len(rep.Checks) != 0 {
		t.Errorf("clones = %v, checks = %+v, want no fetch", runner.clones, rep.Checks)
	}
}

func TestRestoreIsolatesSourceFailures(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	other := updateEntry(t, "", lockfile.Entry{
		Source: restoreRepo, SourceURL: restoreRepo, SkillPath: "skills/beta/SKILL.md",
	})
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
		"beta":  other,
	})
	runner := restoreRunner(t, src)
	runner.fail[restoreRepo] = "fatal: could not read from remote"

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !rep.Failed() {
		t.Error("report is clean, want the failed source to fail the run")
	}
	if len(rep.Checks) != 2 {
		t.Fatalf("checks = %+v, want one per source", rep.Checks)
	}
	if rep.Checks[0].Err != "" || rep.Checks[1].Err == "" {
		t.Errorf("checks = %+v, want the second source to fail", rep.Checks)
	}
	if len(rep.Results) != 1 || rep.Results[0].Name != "alpha" {
		t.Fatalf("results = %+v, want only the working source restored", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("alpha was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "beta")); !os.IsNotExist(err) {
		t.Errorf("beta was restored from a failed source: %v", err)
	}
}

func TestRestoreDryRunWritesNothing(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	runner := restoreRunner(t, src)

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode"}, DryRun: true, Runner: runner},
		updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !rep.DryRun {
		t.Error("DryRun is false")
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the planned restore", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("a dry run installed the skill: %v", err)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if entry, _ := lock.Get("alpha"); entry.ComputedHash != "" {
		t.Errorf("computedHash = %q, want the lock untouched", entry.ComputedHash)
	}
	if rep.LockPath != "" {
		t.Errorf("LockPath = %q, want no write in a dry run", rep.LockPath)
	}
}

func TestRestoreAgentFallback(t *testing.T) {
	tests := []struct {
		name       string
		agents     []string
		saved      []string
		detectDir  string
		wantClaude bool
		wantContin bool
	}{
		{name: "flag wins over the saved selection", agents: []string{"opencode"},
			saved: []string{"claude-code"}},
		{name: "saved selection", saved: []string{"claude-code"}, wantClaude: true},
		{name: "detected agent", detectDir: ".continue", wantContin: true},
		{name: "nothing selected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			src := restoreSource(t)
			saveProjectLock(t, project, map[string]lockfile.Entry{
				"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
			})
			env := updateTestEnv(t, project)
			if len(tc.saved) > 0 {
				saveGlobalAgents(t, env.AgentEnv, tc.saved)
			}
			if tc.detectDir != "" {
				if err := os.MkdirAll(filepath.Join(project, tc.detectDir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Restore(RestoreOptions{Agents: tc.agents, Runner: restoreRunner(t, src)}, env); err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if !exists(filepath.Join(project, ".agents", "skills", "alpha")) {
				t.Error("the canonical copy was not restored")
			}
			if got := exists(filepath.Join(project, ".claude", "skills", "alpha")); got != tc.wantClaude {
				t.Errorf(".claude link exists = %v, want %v", got, tc.wantClaude)
			}
			if got := exists(filepath.Join(project, ".continue", "skills", "alpha")); got != tc.wantContin {
				t.Errorf(".continue link exists = %v, want %v", got, tc.wantContin)
			}
		})
	}
}

func TestRestoreMissingOrEmptyLock(t *testing.T) {
	env := updateTestEnv(t, t.TempDir())
	if _, err := Restore(RestoreOptions{}, env); err == nil ||
		!strings.Contains(err.Error(), "skills-lock.json") {
		t.Errorf("err = %v, want a missing-lock error", err)
	}

	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{})
	_, err := Restore(RestoreOptions{}, updateTestEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "No project skills found") {
		t.Errorf("err = %v, want the empty-lock message", err)
	}
}

func TestRestoreInvalidAgentIsAUsageError(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	_, err := Restore(RestoreOptions{Agents: []string{"nope"}, Runner: newUpdateRunner()},
		updateTestEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "invalid agents") {
		t.Errorf("err = %v, want a usage error", err)
	}
}

// exists reports whether path exists, including a symlink.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
