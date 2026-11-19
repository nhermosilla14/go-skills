package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// globalTestEnv builds an environment with an isolated home and XDG state directory.
func globalTestEnv(t *testing.T, project string) Env {
	t.Helper()
	home := t.TempDir()
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: home, Cwd: project,
		Vars: map[string]string{"XDG_STATE_HOME": filepath.Join(home, "state")},
	}
	return env
}

// useGitHubAPI points the Trees API at a test server for the duration of a test.
func useGitHubAPI(t *testing.T, url string) {
	t.Helper()
	old := githubAPIBase
	githubAPIBase = url
	t.Cleanup(func() { githubAPIBase = old })
}

func TestGlobalFolderHashUsesGitHubTreeSHA(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	sha := strings.Repeat("b", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/git/trees/"+strings.Repeat("a", 40) {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sha": strings.Repeat("c", 40),
			"tree": []map[string]string{
				{"path": "skills/alpha", "type": "tree", "sha": sha},
			},
		})
	}))
	defer server.Close()
	useGitHubAPI(t, server.URL)

	installed := t.TempDir()
	writeSourceFile(t, installed, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	got, warning, err := globalFolderHash(lockfile.GlobalEntry{
		SourceType: "github", Source: "owner/repo", Commit: strings.Repeat("a", 40),
		SkillPath: "skills/alpha/SKILL.md",
	}, installed)
	if err != nil {
		t.Fatalf("globalFolderHash: %v", err)
	}
	if got != sha {
		t.Errorf("hash = %q, want the tree SHA %q", got, sha)
	}
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
}

func TestGlobalFolderHashFallsBackToContentHash(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	useGitHubAPI(t, server.URL)

	installed := t.TempDir()
	writeSourceFile(t, installed, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	want, err := hash.Dir(installed)
	if err != nil {
		t.Fatal(err)
	}
	got, warning, err := globalFolderHash(lockfile.GlobalEntry{
		SourceType: "github", Source: "owner/repo", Commit: strings.Repeat("a", 40),
		SkillPath: "skills/alpha/SKILL.md",
	}, installed)
	if err != nil {
		t.Fatalf("globalFolderHash: %v", err)
	}
	if got != want || !contentHashRE.MatchString(got) {
		t.Errorf("hash = %q, want the 64-hex content hash %q", got, want)
	}
	if warning == "" {
		t.Error("no warning for the API failure")
	}

	got, warning, err = globalFolderHash(lockfile.GlobalEntry{
		SourceType: "git", Source: "file:///repo", Commit: strings.Repeat("a", 40),
	}, installed)
	if err != nil || got != want || warning != "" {
		t.Errorf("non-GitHub hash = %q, warning = %q, err = %v; want the content hash", got, warning, err)
	}
}

func TestGlobalFolderHashNeverPrintsTheToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "super-secret-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer super-secret-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	useGitHubAPI(t, server.URL)

	installed := t.TempDir()
	writeSourceFile(t, installed, "SKILL.md", "x")
	_, warning, err := globalFolderHash(lockfile.GlobalEntry{
		SourceType: "github", Source: "owner/repo", Commit: strings.Repeat("a", 40),
		SkillPath: "SKILL.md",
	}, installed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(warning, "super-secret-token") {
		t.Errorf("warning leaks the token: %q", warning)
	}
}

// contentHashRE matches a 64-hex content hash.
var contentHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestGlobalAddWritesOnlyTheGlobalLock(t *testing.T) {
	project := t.TempDir()
	env := interactiveEnv(t, project)
	script := NewScripted(t, Answers{
		Agents: []string{"opencode"}, Mode: install.ModeSymlink, Proceed: true,
	})
	rep, err := Add(AddOptions{Source: newSource(t), Global: true}, env, script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, lockfile.FileName)); !os.IsNotExist(err) {
		t.Errorf("global add wrote the project lock: %v", err)
	}
	path := agents.GlobalLockPath(env.AgentEnv)
	lock, err := lockfile.LoadGlobal(path)
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	entry, ok := lock.Get("flat")
	if !ok {
		t.Fatalf("global lock has no flat entry: %+v", lock.Skills)
	}
	if entry.SourceType != "local" || entry.InstalledAt != "2026-01-02T03:04:05.000Z" {
		t.Errorf("entry = %+v", entry)
	}
	if len(lock.LastSelectedAgents) != 1 || lock.LastSelectedAgents[0] != "opencode" {
		t.Errorf("lastSelectedAgents = %v, want the prompted selection", lock.LastSelectedAgents)
	}
	if rep.LockPath != path {
		t.Errorf("reported lock = %q, want %q", rep.LockPath, path)
	}
}

func TestGlobalRemoveDeletesUniversalAgentOwnDir(t *testing.T) {
	project := t.TempDir()
	env := globalTestEnv(t, project)
	path := filepath.Join(env.AgentEnv.Home, ".gemini", "config", "skills", "alpha")
	writeSourceFile(t, path, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	lock := lockfile.NewGlobal()
	lock.Put("alpha", lockfile.GlobalEntry{Source: "./src", SourceType: "local"})
	if err := lock.Save(agents.GlobalLockPath(env.AgentEnv)); err != nil {
		t.Fatal(err)
	}
	rep, err := Remove(RemoveOptions{Skills: []string{"alpha"}, Agents: []string{"antigravity"},
		Global: true, Yes: true}, env)
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if rep.Failed() {
		t.Errorf("report = %+v, want no failure", rep)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the agent's own global directory survived: %v", err)
	}
	saved, err := lockfile.LoadGlobal(agents.GlobalLockPath(env.AgentEnv))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Skills) != 0 {
		t.Errorf("lock still tracks %v", saved.Skills)
	}
}

func TestProjectAddNeverWritesTheGlobalLock(t *testing.T) {
	project := t.TempDir()
	env := interactiveEnv(t, project)
	if _, err := Add(AddOptions{Source: newSource(t), Yes: true, Agents: []string{"opencode"}},
		env); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(agents.GlobalLockPath(env.AgentEnv)); !os.IsNotExist(err) {
		t.Errorf("project add wrote the global lock: %v", err)
	}
}

func TestGlobalListAndCheckReadTheGlobalScope(t *testing.T) {
	project := t.TempDir()
	env := globalTestEnv(t, project)
	home := env.AgentEnv.Home
	installed := filepath.Join(home, ".agents", "skills", "alpha")
	writeSourceFile(t, installed, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	writeSourceFile(t, filepath.Join(home, ".agents", "skills", "beta"), "SKILL.md",
		"---\nname: beta\ndescription: b\n---\n")
	digest, err := hash.Dir(installed)
	if err != nil {
		t.Fatal(err)
	}
	lock := lockfile.NewGlobal()
	lock.Put("alpha", lockfile.GlobalEntry{
		Source: "./src", SourceType: "local", SkillFolderHash: digest,
		InstalledAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-01T00:00:00.000Z",
	})
	lock.Put("beta", lockfile.GlobalEntry{
		Source: "./src", SourceType: "local", SkillFolderHash: "",
	})
	lock.Put("gone", lockfile.GlobalEntry{Source: "./src", SourceType: "local"})
	if err := lock.Save(agents.GlobalLockPath(env.AgentEnv)); err != nil {
		t.Fatal(err)
	}

	rep, err := List(ListOptions{Global: true}, env)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if rep.Scope != string(ScopeGlobal) {
		t.Errorf("scope = %q, want global", rep.Scope)
	}
	byName := listByName(rep)
	if got := byName["alpha"].Status; got != StatusTracked {
		t.Errorf("alpha = %q, want tracked", got)
	}
	if got := byName["beta"].Status; got != StatusModified {
		t.Errorf("beta = %q, want modified for an empty hash", got)
	}
	if got := byName["gone"].Status; got != StatusMissing {
		t.Errorf("gone = %q, want missing", got)
	}

	check, err := Check(context.Background(), CheckOptions{Global: true}, env)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if check.Scope != string(ScopeGlobal) {
		t.Errorf("check scope = %q", check.Scope)
	}
	if len(check.Modified) != 1 || check.Modified[0] != "beta" {
		t.Errorf("modified = %v, want beta", check.Modified)
	}
	if !check.Failed() {
		t.Error("check is clean, want the empty hash to fail it")
	}
}
