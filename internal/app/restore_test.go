package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

// frozenEnv is a project environment that looks like CI: any restore runs frozen by default.
func frozenEnv(t *testing.T, project string) Env {
	t.Helper()
	env := updateTestEnv(t, project)
	env.AgentEnv.Vars["CI"] = "1"
	return env
}

// frozenSnapshot records the project tree, the lock bytes and the lock modification time, so a
// failing frozen run can prove it changed nothing.
type frozenSnapshot struct {
	tree    map[string]string
	lock    string
	lockMod time.Time
	hasLock bool
}

func snapshotFrozen(t *testing.T, project string) frozenSnapshot {
	t.Helper()
	snap := frozenSnapshot{tree: map[string]string{}}
	err := filepath.WalkDir(project, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(project, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			snap.tree[rel] = "link:" + target
		case d.IsDir():
			snap.tree[rel] = "dir"
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			snap.tree[rel] = "file:" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotting %s: %v", project, err)
	}
	lockPath := filepath.Join(project, lockfile.FileName)
	if info, err := os.Stat(lockPath); err == nil {
		snap.hasLock = true
		snap.lockMod = info.ModTime()
		snap.lock = readFile(t, lockPath)
	}
	return snap
}

// assertUnchanged fails when project no longer matches the snapshot, lock bytes and mtime included.
func (s frozenSnapshot) assertUnchanged(t *testing.T, project string) {
	t.Helper()
	after := snapshotFrozen(t, project)
	if !reflect.DeepEqual(after.tree, s.tree) {
		t.Errorf("tree changed:\n got %v\nwant %v", after.tree, s.tree)
	}
	if after.hasLock != s.hasLock || after.lock != s.lock {
		t.Errorf("lock changed:\n got %q\nwant %q", after.lock, s.lock)
	}
	if !after.lockMod.Equal(s.lockMod) {
		t.Errorf("lock mtime = %v, want %v", after.lockMod, s.lockMod)
	}
}

func TestFrozenRestoreEnvironmentTable(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want bool
	}{
		{name: "nothing set", vars: map[string]string{}},
		{name: "unrelated variable", vars: map[string]string{"PATH": "/bin"}},
		{name: "empty CI", vars: map[string]string{"CI": ""}},
	}
	for _, name := range ciVariables {
		tests = append(tests, struct {
			name string
			vars map[string]string
			want bool
		}{name: name, vars: map[string]string{name: "1"}, want: true})
	}
	// Any non-empty value counts, as upstream's Boolean() does.
	tests = append(tests,
		struct {
			name string
			vars map[string]string
			want bool
		}{name: "CI=0", vars: map[string]string{"CI": "0"}, want: true},
		struct {
			name string
			vars map[string]string
			want bool
		}{name: "CI=false", vars: map[string]string{"CI": "false"}, want: true},
	)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := frozenRestore(FrozenFromEnvironment, tc.vars); got != tc.want {
				t.Errorf("frozenRestore(%v) = %v, want %v", tc.vars, got, tc.want)
			}
		})
	}
}

func TestFrozenRestoreFlagOverrides(t *testing.T) {
	ci := map[string]string{"CI": "1"}
	if !frozenRestore(FrozenEnabled, map[string]string{}) {
		t.Error("--frozen-lockfile did not force frozen mode")
	}
	if !frozenRestore(FrozenEnabled, ci) {
		t.Error("--frozen-lockfile lost to the environment")
	}
	if frozenRestore(FrozenDisabled, ci) {
		t.Error("--no-frozen-lockfile did not override the environment")
	}
	if frozenRestore(FrozenDisabled, map[string]string{}) {
		t.Error("--no-frozen-lockfile forced frozen mode")
	}
}

func TestFrozenRestoreFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, project string) (*updateRunner, Env)
		want  []string
	}{
		{
			name: "missing lock",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				return newUpdateRunner(), frozenEnv(t, project)
			},
			want: []string{"skills-lock.json is absent or has no skills"},
		},
		{
			name: "empty lock",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				saveProjectLock(t, project, map[string]lockfile.Entry{})
				return newUpdateRunner(), frozenEnv(t, project)
			},
			want: []string{"skills-lock.json is absent or has no skills"},
		},
		{
			name: "entry without a hash",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				saveProjectLock(t, project, map[string]lockfile.Entry{
					"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
				})
				return restoreRunner(t, restoreSource(t)), frozenEnv(t, project)
			},
			want: []string{"alpha has no content hash in skills-lock.json", "migrated from version 1"},
		},
		{
			name: "missing installed folder",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				src := restoreSource(t)
				entry := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
				digest, err := hash.Dir(filepath.Join(src, "skills", "alpha"))
				if err != nil {
					t.Fatal(err)
				}
				entry.ComputedHash = digest
				saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
				return restoreRunner(t, src), frozenEnv(t, project)
			},
			want: []string{"alpha is missing; skills-lock.json expects the installed content"},
		},
		{
			name: "changed content",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				installed := installAlpha(t, project, ".agents/skills/alpha", "edited\n")
				digest, err := hash.Dir(installed)
				if err != nil {
					t.Fatal(err)
				}
				entry := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
				entry.ComputedHash = digest
				saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
				writeSourceFile(t, installed, "SKILL.md", "---\nname: alpha\ndescription: d\n---\ndrifted\n")
				return restoreRunner(t, restoreSource(t)), frozenEnv(t, project)
			},
			want: []string{"alpha has changed since skills-lock.json was written"},
		},
		{
			name: "skill gone from the source",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
				digest, err := hash.Dir(installed)
				if err != nil {
					t.Fatal(err)
				}
				entry := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
				entry.ComputedHash = digest
				saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
				src := t.TempDir()
				writeSourceFile(t, src, "skills/beta/SKILL.md", "---\nname: beta\ndescription: d\n---\n")
				return restoreRunner(t, src), frozenEnv(t, project)
			},
			want: []string{"alpha not found in file:///repo"},
		},
		{
			name: "source cannot be fetched",
			setup: func(t *testing.T, project string) (*updateRunner, Env) {
				installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
				digest, err := hash.Dir(installed)
				if err != nil {
					t.Fatal(err)
				}
				entry := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
				entry.ComputedHash = digest
				saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
				runner := restoreRunner(t, restoreSource(t))
				runner.fail[updateRepo] = "fatal: could not read from remote"
				return runner, frozenEnv(t, project)
			},
			want: []string{"alpha cannot be restored from file:///repo:", "fatal: could not read from remote"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			runner, env := tc.setup(t, project)
			before := snapshotFrozen(t, project)

			_, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: runner}, env)
			if err == nil {
				t.Fatal("Restore succeeded, want a frozen failure")
			}
			message := err.Error()
			if !strings.Contains(message, "Cannot install with --frozen-lockfile") {
				t.Errorf("err = %v, want the frozen-lockfile diagnostic", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(message, want) {
					t.Errorf("err = %v, want containing %q", err, want)
				}
			}
			before.assertUnchanged(t, project)
		})
	}
}

func TestFrozenRestoreAggregateMessage(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	digest, err := hash.Dir(filepath.Join(src, "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	missingFolder := updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
	missingFolder.ComputedHash = digest
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": missingFolder,
		"beta":  updateEntry(t, "", lockfile.Entry{SkillPath: "skills/beta/SKILL.md"}),
	})
	_, err = Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: restoreRunner(t, src)},
		frozenEnv(t, project))
	if err == nil {
		t.Fatal("Restore succeeded, want a frozen failure")
	}
	want := "Cannot install with --frozen-lockfile because skills-lock.json is not up to date:\n" +
		"  alpha is missing; skills-lock.json expects the installed content\n" +
		"  beta has no content hash in skills-lock.json " +
		"(migrated from version 1, and the installed skill was missing or did not match)\n" +
		"Run again with --no-frozen-lockfile to update it."
	if err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

func TestFrozenRestoreAcceptsMatchingContent(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
		"beta":  updateEntry(t, "", lockfile.Entry{SkillPath: "skills/beta/SKILL.md"}),
	})
	env := updateTestEnv(t, project)
	runner := restoreRunner(t, src)
	if _, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner},
		env); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	before := snapshotFrozen(t, project)
	env.AgentEnv.Vars["CI"] = "1"

	rep, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner}, env)
	if err != nil {
		t.Fatalf("frozen restore: %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %+v, want both skills checked", rep.Results)
	}
	for _, res := range rep.Results {
		if res.Status != RestoreSkipped {
			t.Errorf("%s = %v, want a skip", res.Name, res.Status)
		}
	}
	if rep.LockPath != "" {
		t.Errorf("LockPath = %q, want no lock write", rep.LockPath)
	}
	before.assertUnchanged(t, project)
}

func TestFrozenRestoreDryRun(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
		"beta":  updateEntry(t, "", lockfile.Entry{SkillPath: "skills/beta/SKILL.md"}),
	})
	env := updateTestEnv(t, project)
	runner := restoreRunner(t, src)
	if _, err := Restore(RestoreOptions{Agents: []string{"opencode", "claude-code"}, Runner: runner},
		env); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	link := filepath.Join(project, ".claude", "skills", "alpha")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	before := snapshotFrozen(t, project)
	env.AgentEnv.Vars["CI"] = "1"

	rep, err := Restore(RestoreOptions{
		Agents: []string{"opencode", "claude-code"}, DryRun: true, Runner: runner,
	}, env)
	if err != nil {
		t.Fatalf("frozen dry run: %v", err)
	}
	var relinked bool
	for _, res := range rep.Results {
		if res.Name == "alpha" && res.Status == RestoreRelinked {
			relinked = true
		}
	}
	if !relinked {
		t.Errorf("results = %+v, want the planned relink", rep.Results)
	}
	if rep.LockPath != "" {
		t.Errorf("LockPath = %q, want no lock write", rep.LockPath)
	}
	before.assertUnchanged(t, project)
}

func TestFrozenRestoreDryRunStillValidates(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	_, err := Restore(RestoreOptions{Agents: []string{"opencode"}, DryRun: true,
		Runner: restoreRunner(t, restoreSource(t))}, frozenEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "has no content hash") {
		t.Fatalf("err = %v, want the frozen validation to report the empty hash", err)
	}
}

func TestNonFrozenRestoreRepairsInCI(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	env := frozenEnv(t, project)

	rep, err := Restore(RestoreOptions{
		Agents: []string{"opencode"}, Frozen: FrozenDisabled, Runner: restoreRunner(t, src),
	}, env)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the repair", rep.Results)
	}
	if rep.LockPath == "" {
		t.Error("LockPath is empty, want the filled hash written")
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := lock.Get("alpha")
	want, err := hash.Dir(filepath.Join(project, ".agents", "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.ComputedHash != want {
		t.Errorf("computedHash = %q, want %q", entry.ComputedHash, want)
	}
}

func TestFrozenRestoreV1EmptyHash(t *testing.T) {
	project := t.TempDir()
	src := restoreSource(t)
	legacy := `{
  "version": 1,
  "skills": {
    "alpha": {"source": "` + updateRepo + `", "sourceUrl": "` + updateRepo + `",
      "sourceType": "git", "commit": "` + strings.Repeat("a", 40) + `",
      "skillPath": "skills/alpha/SKILL.md", "computedHash": ""}
  }
}
`
	lockPath := filepath.Join(project, lockfile.FileName)
	if err := os.WriteFile(lockPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotFrozen(t, project)

	_, err := Restore(RestoreOptions{Agents: []string{"opencode"}, Runner: restoreRunner(t, src)},
		frozenEnv(t, project))
	if err == nil || !strings.Contains(err.Error(), "migrated from version 1") {
		t.Fatalf("err = %v, want the version 1 empty-hash message", err)
	}
	before.assertUnchanged(t, project)

	rep, err := Restore(RestoreOptions{
		Agents: []string{"opencode"}, Frozen: FrozenDisabled, Runner: restoreRunner(t, src),
	}, frozenEnv(t, project))
	if err != nil {
		t.Fatalf("normal restore: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != RestoreRestored {
		t.Fatalf("results = %+v, want the skill refreshed", rep.Results)
	}
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := lock.Get("alpha")
	want, err := hash.Dir(filepath.Join(project, ".agents", "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if entry.ComputedHash != want {
		t.Errorf("computedHash = %q, want the refreshed hash %q", entry.ComputedHash, want)
	}
	if !strings.Contains(readFile(t, lockPath), `"version": 2`) {
		t.Error("the repaired lock is not a version 2 lock")
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
