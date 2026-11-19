package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// skillBody is the minimal valid SKILL.md content for a name and description.
func skillBody(name, description string, internal bool) string {
	body := "---\nname: " + name + "\ndescription: " + description + "\n"
	if internal {
		body += "metadata:\n  internal: true\n"
	}
	return body + "---\nbody\n"
}

// writeProjectSkill creates one installed skill directory under the project.
func writeProjectSkill(t *testing.T, project, rel, name, description string) string {
	t.Helper()
	writeSourceFile(t, project, filepath.ToSlash(filepath.Join(rel, "SKILL.md")), skillBody(name, description, false))
	return filepath.Join(project, rel)
}

// saveProjectLock writes the given lock entries under the project root.
func saveProjectLock(t *testing.T, project string, entries map[string]lockfile.Entry) {
	t.Helper()
	lock := lockfile.New()
	for name, entry := range entries {
		lock.Put(name, entry)
	}
	if err := lock.Save(filepath.Join(project, lockfile.FileName)); err != nil {
		t.Fatalf("saving the lock: %v", err)
	}
}

// listByName indexes a list report by skill name.
func listByName(rep ListReport) map[string]SkillInfo {
	byName := map[string]SkillInfo{}
	for _, s := range rep.Skills {
		byName[s.Name] = s
	}
	return byName
}

// hasAgent reports whether names contains want.
func hasAgent(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestListStatusesAndMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are exercised on POSIX")
	}
	project := t.TempDir()
	alpha := writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	writeProjectSkill(t, project, ".agents/skills/beta", "beta", "second")
	writeProjectSkill(t, project, ".agents/skills/gamma", "gamma", "third")

	alphaHash, err := hash.Dir(alpha)
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "owner/repo", SourceType: "github", ComputedHash: alphaHash},
		"beta":  {Source: "owner/repo", SourceType: "github", ComputedHash: "not-the-hash"},
		"gone":  {Source: "owner/other", SourceType: "git", ComputedHash: "abc"},
	})
	// A symlink from an agent directory gives the skill a second link.
	link := filepath.Join(project, ".claude", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(alpha, link); err != nil {
		t.Fatal(err)
	}

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := listByName(rep)
	if len(rep.Skills) != 4 {
		t.Fatalf("skills = %+v, want alpha, beta, gamma and gone", rep.Skills)
	}
	if got := byName["alpha"]; got.Status != StatusTracked || got.Path != alpha {
		t.Errorf("alpha = %+v, want tracked at the canonical path", got)
	}
	if !hasAgent(byName["alpha"].Agents, "Claude Code") || !hasAgent(byName["alpha"].Agents, "OpenCode") {
		t.Errorf("alpha agents = %v, want the symlinked and universal agents", byName["alpha"].Agents)
	}
	if got := byName["beta"].Status; got != StatusModified {
		t.Errorf("beta status = %q, want modified", got)
	}
	if got := byName["gamma"]; got.Status != StatusUntracked || got.Source != "" {
		t.Errorf("gamma = %+v, want untracked without a source", got)
	}
	if got := byName["gone"]; got.Status != StatusMissing || got.Path != "" || got.Source != "owner/other" {
		t.Errorf("gone = %+v, want missing with the lock source", got)
	}
}

func TestListHashLessEntryIsModified(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"alpha": {Source: "owner/repo", SourceType: "github"},
	})

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := listByName(rep)["alpha"].Status; got != StatusModified {
		t.Errorf("status = %q, want modified for a hash-less entry", got)
	}
}

func TestListMatchesSanitizedLockKey(t *testing.T) {
	project := t.TempDir()
	dir := writeProjectSkill(t, project, ".agents/skills/ce-review", "ce-review", "review")
	digest, err := hash.Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"ce:review": {Source: "owner/repo", SourceType: "github", ComputedHash: digest},
	})

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := listByName(rep)["ce-review"].Status; got != StatusTracked {
		t.Errorf("status = %q, want tracked through the sanitized lock key", got)
	}
}

func TestListDeduplicatesLinkedDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are exercised on POSIX")
	}
	project := t.TempDir()
	alpha := writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	for _, rel := range []string{
		filepath.Join(".claude", "skills", "alpha"),
		filepath.Join(".windsurf", "skills", "alpha"),
	} {
		link := filepath.Join(project, rel)
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(alpha, link); err != nil {
			t.Fatal(err)
		}
	}

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rep.Skills) != 1 {
		t.Fatalf("skills = %+v, want one deduplicated alpha", rep.Skills)
	}
	got := rep.Skills[0]
	if got.Path != alpha {
		t.Errorf("path = %q, want the canonical directory", got.Path)
	}
	for _, want := range []string{"Claude Code", "Windsurf"} {
		if !hasAgent(got.Agents, want) {
			t.Errorf("agents = %v, want %s", got.Agents, want)
		}
	}
}

func TestListFilterHidesMissingAndScansSelectedAgents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks are exercised on POSIX")
	}
	project := t.TempDir()
	alpha := writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	writeProjectSkill(t, project, ".agents/skills/other", "other", "second")
	link := filepath.Join(project, ".claude", "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(alpha, link); err != nil {
		t.Fatal(err)
	}
	saveProjectLock(t, project, map[string]lockfile.Entry{
		"gone": {Source: "owner/repo", SourceType: "github", ComputedHash: "abc"},
	})

	rep, err := List(ListOptions{Agents: []string{"claude-code"}}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rep.Skills) != 1 || rep.Skills[0].Name != "alpha" {
		t.Fatalf("skills = %+v, want only the Claude Code link", rep.Skills)
	}
	if rep.Skills[0].Agents[0] != "Claude Code" {
		t.Errorf("agents = %v, want only Claude Code", rep.Skills[0].Agents)
	}
	if rep.Skills[0].Status != StatusUntracked {
		t.Errorf("status = %q, want untracked", rep.Skills[0].Status)
	}
}

func TestListExcludesInternalUnlessAllowed(t *testing.T) {
	project := t.TempDir()
	writeSourceFile(t, project, ".agents/skills/hidden/SKILL.md", skillBody("hidden", "internal", true))
	writeSourceFile(t, project, ".agents/skills/shown/SKILL.md", skillBody("shown", "public", false))

	env := testEnv(project)
	rep, err := List(ListOptions{}, env)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, ok := listByName(rep)["hidden"]; ok {
		t.Error("internal skill was listed")
	}
	if _, ok := listByName(rep)["shown"]; !ok {
		t.Error("public skill was not listed")
	}

	env.IncludeInternal = true
	rep, err = List(ListOptions{}, env)
	if err != nil {
		t.Fatalf("List with internal skills: %v", err)
	}
	if _, ok := listByName(rep)["hidden"]; !ok {
		t.Error("internal skill was not listed when allowed")
	}
}

func TestListSkipsUnparseableSkillDirectories(t *testing.T) {
	project := t.TempDir()
	writeSourceFile(t, project, ".agents/skills/broken/SKILL.md", "---\ndescription: no name\n---\n")
	writeSourceFile(t, project, ".agents/skills/empty/SKILL.md", "")
	writeProjectSkill(t, project, ".agents/skills/good", "good", "fine")

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rep.Skills) != 1 || rep.Skills[0].Name != "good" {
		t.Errorf("skills = %+v, want only the parseable skill", rep.Skills)
	}
}

func TestListSortsSkillsAndAgentsDeterministically(t *testing.T) {
	project := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "mu"} {
		writeProjectSkill(t, project, filepath.Join(".agents", "skills", name), name, "d")
	}
	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var names []string
	for _, s := range rep.Skills {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "alpha,mu,zeta" {
		t.Errorf("order = %v, want sorted by name", names)
	}
	if len(rep.Skills[0].Agents) == 0 || rep.Skills[0].Agents[0] != "Amp" {
		t.Errorf("agents = %v, want registry order", rep.Skills[0].Agents)
	}
}

func TestListLegacyDirectoryHasNoAgent(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".cline/skills/legacy", "legacy", "old")

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got, ok := listByName(rep)["legacy"]
	if !ok {
		t.Fatalf("legacy skill was not listed: %+v", rep.Skills)
	}
	if len(got.Agents) != 0 || got.Status != StatusUntracked {
		t.Errorf("legacy = %+v, want an untracked skill with no agent", got)
	}
}

func TestListScansTheBareSkillsDirectory(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, "skills/openclaw-skill", "openclaw-skill", "d")

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got, ok := listByName(rep)["openclaw-skill"]
	if !ok {
		t.Fatalf("skills = %+v, want the OpenClaw directory scanned", rep.Skills)
	}
	if !hasAgent(got.Agents, "OpenClaw") {
		t.Errorf("agents = %v, want OpenClaw", got.Agents)
	}
}

func TestListCorruptLockIsAWarning(t *testing.T) {
	project := t.TempDir()
	writeProjectSkill(t, project, ".agents/skills/alpha", "alpha", "first")
	if err := os.WriteFile(filepath.Join(project, lockfile.FileName), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := List(ListOptions{}, testEnv(project))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rep.Warnings) == 0 {
		t.Error("no warning for the corrupt lock")
	}
	if got := listByName(rep)["alpha"].Status; got != StatusUntracked {
		t.Errorf("status = %q, want untracked with an unreadable lock", got)
	}
}
