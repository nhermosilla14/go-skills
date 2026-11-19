package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// installForRemoval installs the test source for the two agents with different skill directories.
func installForRemoval(t *testing.T, project string, copyMode bool) {
	t.Helper()
	_, err := Add(AddOptions{
		Source: newSource(t),
		Agents: []string{"opencode", "claude-code"},
		Yes:    true,
		Copy:   copyMode,
	}, addEnv(t, project))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func lockFile(t *testing.T, project string) *lockfile.File {
	t.Helper()
	lock, err := lockfile.Load(filepath.Join(project, lockfile.FileName))
	if err != nil {
		t.Fatalf("Load lock: %v", err)
	}
	return lock
}

func requireExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	exists := err == nil
	if exists != want {
		t.Fatalf("%s exists = %v, want %v (err %v)", path, exists, want, err)
	}
}

// TestRemoveHostModeIsNonInteractive proves a detected host agent removes without asking, while
// the same command keeps the documented interactive refusal without one.
func TestRemoveHostModeIsNonInteractive(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)

	plain := addEnv(t, project)
	if _, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"opencode"}}, plain); !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("err = %v, want the interactive refusal", err)
	}

	host := addEnv(t, project)
	host.Host = agents.Host{ID: "opencode", Name: "OpenCode"}
	rep, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"opencode"}}, host)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if rep.Failed() {
		t.Errorf("results = %+v, want a clean removal", rep.Results)
	}
}

// TestRemoveGuardKeepsAModifiedCopy covers the second half of the rule: a tracked directory in a
// non-dot agent directory is kept when it holds files the lock entry does not describe.
func TestRemoveGuardKeepsAModifiedCopy(t *testing.T) {
	project := t.TempDir()
	dir := writeProjectSkill(t, project, "data/skills/alpha", "alpha", "d")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "./src", SourceType: "local", ComputedHash: mustHash(t, dir)},
	})
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hand written\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Remove(RemoveOptions{Skills: []string{"alpha"}, Agents: []string{"astrbot"}, Yes: true},
		testEnv(project))
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if !rep.Failed() {
		t.Fatalf("results = %+v, want the guarded directory refused", rep.Results)
	}
	if !strings.Contains(rep.Results[0].Message, "not an unmodified copy") {
		t.Errorf("message = %q", rep.Results[0].Message)
	}
	requireExists(t, filepath.Join(dir, "notes.md"), true)
}

func TestRemoveKeepsCanonicalWhileAnotherAgentLinksIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are exercised on POSIX")
	}
	project := t.TempDir()
	installForRemoval(t, project, false)
	canonical := filepath.Join(project, ".agents", "skills", "flat")
	link := filepath.Join(project, ".claude", "skills", "flat")

	rep, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"claude-code"}, Yes: true},
		testEnv(project))
	if err != nil {
		t.Fatalf("Remove claude-code: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results failed: %+v", rep.Results)
	}
	requireExists(t, link, false)
	requireExists(t, filepath.Join(canonical, "SKILL.md"), true)
	if _, ok := lockFile(t, project).Get("flat"); !ok {
		t.Fatal("lock entry removed while another agent still uses the canonical copy")
	}

	rep, err = Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"opencode"}, Yes: true},
		testEnv(project))
	if err != nil {
		t.Fatalf("Remove opencode: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results failed: %+v", rep.Results)
	}
	requireExists(t, canonical, false)
	if _, ok := lockFile(t, project).Get("flat"); ok {
		t.Fatal("lock entry survived after the last copy was removed")
	}
}

func TestRemoveCopyModeDirectories(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, true)
	canonical := filepath.Join(project, ".agents", "skills", "flat")
	copied := filepath.Join(project, ".claude", "skills", "flat")
	requireExists(t, filepath.Join(canonical, "SKILL.md"), true)
	requireExists(t, filepath.Join(copied, "SKILL.md"), true)

	if _, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"claude-code"}, Yes: true},
		testEnv(project)); err != nil {
		t.Fatalf("Remove claude-code: %v", err)
	}
	requireExists(t, copied, false)
	requireExists(t, filepath.Join(canonical, "SKILL.md"), true)

	if _, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"opencode"}, Yes: true},
		testEnv(project)); err != nil {
		t.Fatalf("Remove opencode: %v", err)
	}
	requireExists(t, canonical, false)
}

func TestRemoveAllKeepsUntrackedDirectories(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	hand := writeProjectSkill(t, project, ".agents/skills/hand", "hand", "hand written")

	rep, err := Remove(RemoveOptions{All: true, Agents: []string{"*"}, Yes: true}, testEnv(project))
	if err != nil {
		t.Fatalf("Remove --all: %v", err)
	}
	requireExists(t, hand, true)
	if len(lockFile(t, project).Skills) != 0 {
		t.Fatalf("lock = %+v, want every tracked entry removed", lockFile(t, project).Skills)
	}
	found := false
	for _, note := range rep.Notes {
		if strings.Contains(note, "hand") && strings.Contains(note, "not recorded") {
			found = true
		}
	}
	if !found {
		t.Errorf("notes = %v, want the untracked hand directory reported as skipped", rep.Notes)
	}

	if _, err := Remove(RemoveOptions{Skills: []string{"hand"}, Yes: true}, testEnv(project)); err != nil {
		t.Fatalf("Remove hand by name: %v", err)
	}
	requireExists(t, hand, false)
}

func TestRemoveSkillWildcardSelectsLockTrackedSkills(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	hand := writeProjectSkill(t, project, ".agents/skills/hand", "hand", "hand written")

	if _, err := Remove(RemoveOptions{Skills: []string{"*"}, Agents: []string{"*"}, Yes: true},
		testEnv(project)); err != nil {
		t.Fatalf("Remove '*': %v", err)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "flat"), false)
	requireExists(t, hand, true)
	if len(lockFile(t, project).Skills) != 0 {
		t.Fatal("lock entries survived --skill '*'")
	}
}

func TestRemoveAgentWildcardRemovesEveryCopy(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	if _, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"*"}, Yes: true},
		testEnv(project)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "flat"), false)
	requireExists(t, filepath.Join(project, ".claude", "skills", "flat"), false)
	if _, ok := lockFile(t, project).Get("flat"); ok {
		t.Fatal("lock entry survived an all-agent removal")
	}
}

func TestRemoveSelectsByExactSource(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	writeProjectSkill(t, project, ".agents/skills/beta", "beta", "second")
	writeProjectSkill(t, project, ".agents/skills/gamma", "gamma", "other")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "owner/repo", SourceType: "github"},
		"beta":  {Source: "owner/repo", SourceType: "github"},
		"gamma": {Source: "owner/other", SourceType: "github"},
	})

	if _, err := Remove(RemoveOptions{Skills: []string{"owner/repo"}, Yes: true}, testEnv(project)); err != nil {
		t.Fatalf("Remove source: %v", err)
	}
	lock := lockFile(t, project)
	if _, ok := lock.Get("alpha"); ok {
		t.Error("alpha survived a source removal")
	}
	if _, ok := lock.Get("beta"); ok {
		t.Error("beta survived a source removal")
	}
	entry, ok := lock.Get("gamma")
	if !ok || entry.Source != "owner/other" {
		t.Errorf("gamma = %+v, want the unrelated entry preserved", entry)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "gamma", "SKILL.md"), true)
}

func TestRemoveMatchesSanitizedNameAndLockKey(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".agents/skills/ce-review", "ce-review", "review")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"ce:review": {Source: "owner/repo", SourceType: "github"},
	})

	rep, err := Remove(RemoveOptions{Skills: []string{"CE:Review"}, Yes: true}, testEnv(project))
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "ce-review"), false)
	if _, ok := lockFile(t, project).Get("ce:review"); ok {
		t.Error("lock key ce:review survived")
	}
}

func TestRemoveSymlinkTargetSurvives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are exercised on POSIX")
	}
	project := t.TempDir()
	external := t.TempDir()
	writeSourceFile(t, external, "SKILL.md", skillBody("linked", "external skill", false))
	link := filepath.Join(project, ".claude", "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}

	if _, err := Remove(RemoveOptions{Skills: []string{"linked"}, Agents: []string{"claude-code"}, Yes: true},
		testEnv(project)); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	requireExists(t, link, false)
	requireExists(t, filepath.Join(external, "SKILL.md"), true)
}

func TestRemoveDoesNotRewriteAnUnchangedLock(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".claude/skills/flat", "flat", "copied")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"flat": {Source: "owner/repo", SourceType: "github"},
	})
	lockPath := filepath.Join(project, lockfile.FileName)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	// Removing from a universal agent leaves the unrelated copy and the lock alone.
	rep, err := Remove(RemoveOptions{Skills: []string{"flat"}, Agents: []string{"cursor"}, Yes: true},
		testEnv(project))
	if err != nil {
		t.Fatalf("Remove cursor: %v", err)
	}
	if rep.LockPath != "" {
		t.Errorf("LockPath = %q, want no write", rep.LockPath)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("remove rewrote a lock that did not change")
	}
}

func TestRemoveNoMatch(t *testing.T) {
	project := t.TempDir()
	_, err := Remove(RemoveOptions{Skills: []string{"missing"}, Yes: true}, testEnv(project))
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("err = %v, want ErrNoMatch", err)
	}
	_, err = Remove(RemoveOptions{All: true, Yes: true}, testEnv(project))
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("--all err = %v, want ErrNoMatch", err)
	}
}

func TestRemoveRejectsAllWithNames(t *testing.T) {
	_, err := Remove(RemoveOptions{All: true, Skills: []string{"flat"}, Yes: true}, testEnv(t.TempDir()))
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestRemovePromptsForSelectionAndConfirmation(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	env := testEnv(project)
	env.IsTTY = true
	prompter := NewScripted(t, Answers{Skills: []string{"flat"}, Proceed: true})

	rep, err := Remove(RemoveOptions{}, env, prompter)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("results = %+v", rep.Results)
	}
	asked := prompter.Asked()
	if len(asked) != 1 {
		t.Fatalf("asked %d prompt sessions, want one", len(asked))
	}
	want := []StepID{StepRemoveSkills, StepRemoveConfirm}
	if len(asked[0].Steps) != len(want) || asked[0].Steps[0] != want[0] || asked[0].Steps[1] != want[1] {
		t.Errorf("steps = %v, want %v", asked[0].Steps, want)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "flat"), false)
	requireExists(t, filepath.Join(project, ".claude", "skills", "flat"), false)
}

func TestRemoveYesSkipsTheConfirmation(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	env := testEnv(project)
	env.IsTTY = true
	prompter := NewScripted(t, Answers{Skills: []string{"flat"}})

	if _, err := Remove(RemoveOptions{Yes: true}, env, prompter); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	asked := prompter.Asked()
	if len(asked) != 1 || len(asked[0].Steps) != 1 || asked[0].Steps[0] != StepRemoveSkills {
		t.Fatalf("steps = %+v, want only the skill selection", asked)
	}
}

type cancelPrompter struct{}

func (cancelPrompter) Ask(Questions) (Answers, error) { return Answers{}, ErrCancelled }

func TestRemoveCancelRemovesNothing(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	env := testEnv(project)
	env.IsTTY = true

	_, err := Remove(RemoveOptions{}, env, cancelPrompter{})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "flat", "SKILL.md"), true)
	requireExists(t, filepath.Join(project, ".claude", "skills", "flat"), true)
	if _, ok := lockFile(t, project).Get("flat"); !ok {
		t.Fatal("a cancelled prompt removed the lock entry")
	}
}

func TestRemoveNonTTYWithoutYesRequiresAPrompt(t *testing.T) {
	project := t.TempDir()
	installForRemoval(t, project, false)
	_, err := Remove(RemoveOptions{Skills: []string{"flat"}}, testEnv(project))
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("err = %v, want ErrPromptRequired", err)
	}
	requireExists(t, filepath.Join(project, ".agents", "skills", "flat", "SKILL.md"), true)
	if _, ok := lockFile(t, project).Get("flat"); !ok {
		t.Fatal("a refused prompt removed the lock entry")
	}

	_, err = Remove(RemoveOptions{}, testEnv(project))
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("no names err = %v, want ErrPromptRequired", err)
	}
}
