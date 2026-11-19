package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// updateRepo is the file:// source every update test records in the lock.
const updateRepo = "file:///repo"

// updateRunner fakes the git clone and rev-parse of an update: a clone copies a prepared tree and
// rev-parse reports the commit registered for that URL.
type updateRunner struct {
	trees   map[string]string // clone URL → prepared checkout
	commits map[string]string // clone URL → HEAD commit
	fail    map[string]string // clone URL → stderr of a failed clone
	clones  []string          // clone URLs, in call order
	dirs    map[string]string // checkout dir → clone URL
}

func newUpdateRunner() *updateRunner {
	return &updateRunner{
		trees:   map[string]string{},
		commits: map[string]string{},
		fail:    map[string]string{},
		dirs:    map[string]string{},
	}
}

func (r *updateRunner) Run(_ context.Context, cmd source.Command) (source.Output, error) {
	args := cmd.Args
	for len(args) >= 2 && args[0] == "-c" {
		args = args[2:]
	}
	switch {
	case len(args) >= 1 && args[0] == "init":
		// The commit-pin path initialises a repository in place before fetching the SHA.
		return source.Output{}, nil
	case len(args) >= 4 && args[0] == "remote" && args[1] == "add" && args[2] == "origin":
		url := args[3]
		r.clones = append(r.clones, url)
		r.dirs[filepath.Clean(cmd.Dir)] = url
		if message, ok := r.fail[url]; ok {
			return source.Output{Stderr: message}, errors.New("exit status 128")
		}
		return source.Output{}, nil
	case len(args) >= 1 && args[0] == "fetch":
		return source.Output{}, nil
	case len(args) >= 1 && args[0] == "checkout":
		url := r.dirs[filepath.Clean(cmd.Dir)]
		tree, ok := r.trees[url]
		if !ok {
			return source.Output{Stderr: "fatal: repository not found"}, errors.New("exit status 128")
		}
		return source.Output{}, copyTree(tree, cmd.Dir)
	case len(args) >= 2 && args[0] == "clone":
		url, dir := args[len(args)-2], args[len(args)-1]
		r.clones = append(r.clones, url)
		if message, ok := r.fail[url]; ok {
			return source.Output{Stderr: message}, errors.New("exit status 128")
		}
		tree, ok := r.trees[url]
		if !ok {
			return source.Output{Stderr: "fatal: repository not found"}, errors.New("exit status 128")
		}
		r.dirs[filepath.Clean(dir)] = url
		return source.Output{}, copyTree(tree, dir)
	case len(args) >= 1 && args[0] == "rev-parse":
		commit := r.commits[r.dirs[filepath.Clean(cmd.Dir)]]
		if commit == "" {
			commit = strings.Repeat("0", 40)
		}
		return source.Output{Stdout: commit + "\n"}, nil
	}
	return source.Output{}, errors.New("unexpected git command: " + strings.Join(args, " "))
}

// copyTree copies a prepared checkout into the directory the fake clone was given.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// updateTestEnv is a deterministic project environment for the update tests.
func updateTestEnv(t *testing.T, project string) Env {
	t.Helper()
	// Keep the temporary checkouts out of the shared system temp directory: the e2e package runs
	// concurrently and treats new skills-* directories there as leaks.
	t.Setenv("TMPDIR", t.TempDir())
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: t.TempDir(), Cwd: project, Vars: map[string]string{},
		ApplicationsDir: t.TempDir(), EtcDir: t.TempDir(),
	}
	return env
}

// updateSource creates a source tree holding one skill named alpha with the given body.
func updateSource(t *testing.T, body string) string {
	t.Helper()
	src := t.TempDir()
	writeSourceFile(t, src, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\n"+body)
	return src
}

// installAlpha writes an installed alpha copy below the project and returns its directory.
func installAlpha(t *testing.T, project, rel, body string) string {
	t.Helper()
	writeProjectSkill(t, project, rel, "alpha", "d")
	p := filepath.Join(project, filepath.FromSlash(rel), "SKILL.md")
	if err := os.WriteFile(p, []byte("---\nname: alpha\ndescription: d\n---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(project, filepath.FromSlash(rel))
}

// updateEntry fills the fields every test shares and hashes hashFrom when it is not empty.
func updateEntry(t *testing.T, hashFrom string, entry lockfile.Entry) lockfile.Entry {
	t.Helper()
	if entry.Source == "" {
		entry.Source = updateRepo
	}
	if entry.SourceURL == "" {
		entry.SourceURL = updateRepo
	}
	if entry.SourceType == "" {
		entry.SourceType = "git"
	}
	if entry.Commit == "" {
		entry.Commit = strings.Repeat("a", 40)
	}
	if hashFrom != "" {
		digest, err := hash.Dir(hashFrom)
		if err != nil {
			t.Fatal(err)
		}
		entry.ComputedHash = digest
	}
	return entry
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(raw)
}

func TestUpdateUnchangedSkillIsCurrent(t *testing.T) {
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "one\n")
	runner.commits[updateRepo] = strings.Repeat("a", 40)

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Err != "" {
		t.Fatalf("checks = %+v, want one successful check", rep.Checks)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateCurrent {
		t.Fatalf("results = %+v, want alpha current", rep.Results)
	}
	if rep.Failed() || rep.Changed() || rep.Skipped() {
		t.Errorf("report = %+v, want a clean run", rep)
	}
	if rep.LockPath != "" {
		t.Errorf("LockPath = %q, want no lock write", rep.LockPath)
	}
}

func TestUpdateBackfillsTheCommitOfACurrentSkill(t *testing.T) {
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	entry := updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "one\n")
	head := strings.Repeat("b", 40)
	runner.commits[updateRepo] = head

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateCurrent {
		t.Fatalf("results = %+v, want the content reported current", rep.Results)
	}
	if rep.Changed() {
		t.Error("an unchanged skill counts as changed")
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if updated, _ := lock.Get("alpha"); updated.Commit != head {
		t.Errorf("commit = %q, want the fetched HEAD %s", updated.Commit, head)
	}
}

func TestUpdateReinstallsChangedContent(t *testing.T) {
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	entry := updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"})
	saveProjectLock(t, project, map[string]lockfile.Entry{"alpha": entry})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "two\n")
	runner.commits[updateRepo] = strings.Repeat("b", 40)

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want alpha updated", rep.Results)
	}
	if got := readFile(t, filepath.Join(installed, "SKILL.md")); !strings.Contains(got, "two") {
		t.Errorf("installed SKILL.md = %q, want the new content", got)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := lock.Get("alpha")
	if updated.ComputedHash == entry.ComputedHash {
		t.Error("computedHash did not change")
	}
	if updated.Commit != runner.commits[updateRepo] {
		t.Errorf("commit = %q, want the fetched HEAD", updated.Commit)
	}
	if updated.SkillPath != "skills/alpha/SKILL.md" || updated.Source != updateRepo {
		t.Errorf("entry = %+v, want the rest preserved", updated)
	}
	if rep.LockPath == "" {
		t.Error("LockPath is empty, want the lock write")
	}
}

func TestUpdateRestoresAMissingSkill(t *testing.T) {
	project := t.TempDir()
	src := updateSource(t, "one\n")
	digest, err := hash.Dir(filepath.Join(src, "skills", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md", ComputedHash: digest}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = src

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want a reinstall of the missing skill", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("canonical copy not restored: %v", err)
	}
}

func TestUpdateFollowsARelocatedSkill(t *testing.T) {
	project := t.TempDir()
	src := t.TempDir()
	writeSourceFile(t, src, "skills/moved/SKILL.md", "---\nname: alpha\ndescription: d\n---\none\n")
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/old/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = src

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want the relocated skill reinstalled", rep.Results)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if entry, _ := lock.Get("alpha"); entry.SkillPath != "skills/moved/SKILL.md" {
		t.Errorf("skillPath = %q, want the discovered path", entry.SkillPath)
	}
}

func TestUpdateSkipsAmbiguousSkills(t *testing.T) {
	project := t.TempDir()
	src := t.TempDir()
	writeSourceFile(t, src, "skills/one/SKILL.md", "---\nname: dup skill\ndescription: d\n---\none\n")
	writeSourceFile(t, src, "skills/two/SKILL.md", "---\nname: dup_skill\ndescription: d\n---\ntwo\n")
	installed := installAlpha(t, project, ".agents/skills/dup-skill", "local\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"dup-skill": updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/one/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = src

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateSkipped {
		t.Fatalf("results = %+v, want an ambiguous skip", rep.Results)
	}
	if !strings.Contains(rep.Results[0].Reason, "several skills match") {
		t.Errorf("reason = %q", rep.Results[0].Reason)
	}
	if !rep.Skipped() || rep.Failed() {
		t.Errorf("report = %+v, want a skip without failure", rep)
	}
	if got := readFile(t, filepath.Join(installed, "SKILL.md")); !strings.Contains(got, "local") {
		t.Errorf("installed SKILL.md = %q, want it left alone", got)
	}
}

func TestUpdateKeepsARenamedSkill(t *testing.T) {
	project := t.TempDir()
	installed := filepath.Join(project, ".agents", "skills", "alpha-new")
	writeSourceFile(t, project, ".agents/skills/alpha-new/SKILL.md",
		"---\nname: alpha-new\ndescription: d\n---\none\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha-new": updateEntry(t, installed, lockfile.Entry{
			SkillPath: "skills/alpha/SKILL.md", SkillName: "alpha",
		}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "two\n")

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Name != "alpha-new" || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want alpha-new updated under its installed name", rep.Results)
	}
	got := readFile(t, filepath.Join(installed, "SKILL.md"))
	if !strings.Contains(got, "name: alpha-new") || !strings.Contains(got, "two") {
		t.Errorf("installed SKILL.md = %q, want the rename and the new content", got)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := lock.Get("alpha-new")
	if !ok {
		t.Fatalf("lock lost the installed name: %+v", lock.Skills)
	}
	if entry.SkillName != "alpha" {
		t.Errorf("skillName = %q, want the upstream name preserved", entry.SkillName)
	}
	if _, renamed := lock.Get("alpha"); renamed {
		t.Error("lock gained an entry under the upstream name")
	}
}

func TestUpdatePreservesCopyMode(t *testing.T) {
	project := t.TempDir()
	canonical := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	agentDir := filepath.Join(project, ".claude", "skills", "alpha")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "SKILL.md"),
		[]byte("---\nname: alpha\ndescription: d\n---\none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, canonical, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md", InstallMode: "copy"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "two\n")

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want a reinstall", rep.Results)
	}
	for _, dir := range []string{canonical, agentDir} {
		if got := readFile(t, filepath.Join(dir, "SKILL.md")); !strings.Contains(got, "two") {
			t.Errorf("%s = %q, want the new content", dir, got)
		}
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			t.Errorf("%s is not a plain directory: %v", dir, err)
		}
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if entry, _ := lock.Get("alpha"); entry.InstallMode != "copy" {
		t.Errorf("installMode = %q, want copy preserved", entry.InstallMode)
	}
}

func TestUpdateReportsLocalAndLegacyEntries(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"local-skill": {Source: "./src", SourceType: "local", ComputedHash: strings.Repeat("a", 64)},
		"legacy":      {Source: "owner/repo", SourceType: "github", ComputedHash: strings.Repeat("b", 64)},
	})

	runner := newUpdateRunner()
	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Uncheckable) != 2 {
		t.Fatalf("uncheckable = %+v, want both entries", rep.Uncheckable)
	}
	byName := map[string]UncheckableSkill{}
	for _, entry := range rep.Uncheckable {
		byName[entry.Name] = entry
	}
	if byName["local-skill"].Reason != "Local path" || byName["local-skill"].Source != "./src" {
		t.Errorf("local entry = %+v", byName["local-skill"])
	}
	if byName["legacy"].Reason != "No skill path recorded" || byName["legacy"].Source != "owner/repo" {
		t.Errorf("legacy entry = %+v", byName["legacy"])
	}
	if rep.Failed() || !rep.Skipped() {
		t.Errorf("report = %+v, want skips without failure", rep)
	}
	if len(runner.clones) != 0 {
		t.Errorf("clones = %v, want none for uncheckable entries", runner.clones)
	}
}

func TestUpdateUnreachableSourceIsAFailure(t *testing.T) {
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.fail[updateRepo] = "fatal: could not read from remote"

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Err == "" {
		t.Fatalf("checks = %+v, want a failed check", rep.Checks)
	}
	if !rep.Failed() {
		t.Error("report is clean, want a failure exit")
	}
	if len(rep.Results) != 0 {
		t.Errorf("results = %+v, want no per-skill result for a failed group", rep.Results)
	}
}

func TestUpdateKeepsADerivedCopyInstall(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".claude", "skills", "alpha")
	writeProjectSkill(t, project, ".claude/skills/alpha", "alpha", "d")
	if err := os.WriteFile(filepath.Join(agentDir, "SKILL.md"),
		[]byte("---\nname: alpha\ndescription: d\n---\none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, agentDir, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "two\n")

	rep, err := Update(UpdateOptions{Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want a reinstall", rep.Results)
	}
	if got := readFile(t, filepath.Join(agentDir, "SKILL.md")); !strings.Contains(got, "two") {
		t.Errorf("agent copy = %q, want the new content", got)
	}
	if _, err := os.Lstat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("a canonical copy appeared for a copy-mode install: %v", err)
	}
}

func TestUpdateRedactsCredentialsInReportedSources(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {
			Source: "https://user:secret@example.com/repo.git", SourceType: "git",
			SkillPath: "skills/alpha/SKILL.md",
		},
	})
	rep, err := Update(UpdateOptions{Runner: newUpdateRunner()}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !rep.Failed() || len(rep.Checks) != 1 {
		t.Fatalf("report = %+v, want a failed check for the credential URL", rep)
	}
	if strings.Contains(rep.Checks[0].Source, "secret") {
		t.Errorf("reported source = %q, want the credentials removed", rep.Checks[0].Source)
	}
	if !strings.Contains(rep.Checks[0].Source, "example.com") {
		t.Errorf("reported source = %q, want the host kept", rep.Checks[0].Source)
	}
}

func TestUpdateFiltersByInstalledName(t *testing.T) {
	project := t.TempDir()
	src := updateSource(t, "one\n")
	writeSourceFile(t, src, "skills/gamma/SKILL.md", "---\nname: gamma\ndescription: d\n---\none\n")
	alpha := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	writeProjectSkill(t, project, ".agents/skills/gamma", "gamma", "d")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, alpha, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
		"gamma": updateEntry(t, filepath.Join(project, ".agents", "skills", "gamma"),
			lockfile.Entry{SkillPath: "skills/gamma/SKILL.md", Commit: strings.Repeat("c", 40)}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = src

	rep, err := Update(UpdateOptions{Skills: []string{"ALPHA"}, Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Name != "alpha" {
		t.Fatalf("results = %+v, want only alpha", rep.Results)
	}
	if len(runner.clones) != 1 {
		t.Errorf("clones = %v, want the selected group only", runner.clones)
	}
	if hasNote(rep.Notes, "global scope") {
		t.Errorf("notes = %v, want no global limitation", rep.Notes)
	}
}

func TestUpdateRefetchesAnEmptyHash(t *testing.T) {
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, "", lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "one\n")
	rep, err := Update(UpdateOptions{Project: true, Runner: runner}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateUpdated {
		t.Fatalf("results = %+v, want the empty hash re-fetched", rep.Results)
	}
	want, err := hash.Dir(installed)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Skills["alpha"].ComputedHash; got != want {
		t.Errorf("computedHash = %q, want %q", got, want)
	}
}

func TestUpdateUnmatchedNameIsAFailure(t *testing.T) {
	project := t.TempDir()
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "./src", SourceType: "local"},
	})
	rep, err := Update(UpdateOptions{Skills: []string{"nope"}}, updateTestEnv(t, project))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != UpdateFailed {
		t.Fatalf("results = %+v, want the unmatched name as a failure", rep.Results)
	}
	if !rep.Failed() {
		t.Error("report is clean, want a failure exit")
	}
}

func TestUpdateScopePrompt(t *testing.T) {
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	runner := newUpdateRunner()
	runner.trees[updateRepo] = updateSource(t, "one\n")
	env := updateTestEnv(t, project)
	env.IsTTY = true

	prompter := NewScripted(t, Answers{Scope: ScopeProject})
	if _, err := Update(UpdateOptions{Runner: runner}, env, prompter); err != nil {
		t.Fatalf("Update: %v", err)
	}
	asked := prompter.Asked()
	if len(asked) != 1 || len(asked[0].Steps) != 1 || asked[0].Steps[0] != StepUpdateScope {
		t.Fatalf("asked = %+v, want the update scope prompt", asked)
	}

	other := projectWithSkill(t)
	global := NewScripted(t, Answers{Scope: ScopeGlobal})
	rep, err := Update(UpdateOptions{Runner: runner}, envFor(t, other), global)
	if err != nil {
		t.Fatalf("global update: %v", err)
	}
	if rep.Scope != string(ScopeGlobal) {
		t.Errorf("scope = %q, want global", rep.Scope)
	}
	both := NewScripted(t, Answers{Scope: ScopeBoth})
	if _, err := Update(UpdateOptions{Runner: runner}, envFor(t, other), both); !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage for both scopes", err)
	}
}

func TestUpdateNothingToUpdate(t *testing.T) {
	env := updateTestEnv(t, t.TempDir())
	rep, err := Update(UpdateOptions{Yes: true}, env)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !rep.NothingToUpdate {
		t.Fatalf("report = %+v, want nothing to update", rep)
	}
	if !hasNote(rep.Notes, "No project skills to update.") {
		t.Errorf("notes = %v, want the empty message", rep.Notes)
	}
	if rep.Failed() || rep.Changed() || rep.Skipped() {
		t.Errorf("report = %+v, want a clean empty run", rep)
	}
}

// projectWithSkill creates a project with one installed skill and a matching lock.
func projectWithSkill(t *testing.T) string {
	t.Helper()
	project := t.TempDir()
	installed := installAlpha(t, project, ".agents/skills/alpha", "one\n")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": updateEntry(t, installed, lockfile.Entry{SkillPath: "skills/alpha/SKILL.md"}),
	})
	return project
}

func envFor(t *testing.T, project string) Env {
	t.Helper()
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: t.TempDir(), Cwd: project, Vars: map[string]string{},
	}
	env.IsTTY = true
	return env
}

// hasNote reports whether one of the report notes contains want.
func hasNote(notes []string, want string) bool {
	for _, note := range notes {
		if strings.Contains(note, want) {
			return true
		}
	}
	return false
}
