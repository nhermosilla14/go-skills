package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
)

// runGlobal runs the binary with HOME and XDG_STATE_HOME pointing at an isolated directory, so a
// global command can never read or write the real user's home.
func runGlobal(t *testing.T, dir, home string, args ...string) result {
	t.Helper()
	var env []string
	host := hostEnvNames()
	for _, kv := range os.Environ() {
		key, _, _ := splitEnv(kv)
		if globalTestVariables[key] || host[key] {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home, "XDG_STATE_HOME="+filepath.Join(home, "state"))
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), exit: exit}
}

// globalTestVariables can move a global directory or lock outside the test home.
var globalTestVariables = map[string]bool{
	"HOME": true, "USERPROFILE": true, "XDG_STATE_HOME": true, "XDG_CONFIG_HOME": true,
	"APPDATA": true, "FLATPAK_XDG_CONFIG_HOME": true, "CODEX_HOME": true,
	"CLAUDE_CONFIG_DIR": true, "VIBE_HOME": true, "HERMES_HOME": true,
	"AUTOHAND_HOME": true, "GROK_HOME": true, "SARVAM_HOME": true, "REASONIX_HOME": true,
}

// splitEnv splits KEY=VALUE at the first equals sign.
func splitEnv(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return kv, "", false
}

// hostEnvNames are the host-agent markers host detection reads. A test process started by a coding
// agent inherits them, so every run drops them unless the test adds one back.
func hostEnvNames() map[string]bool {
	out := map[string]bool{}
	for _, name := range agents.HostVariables() {
		out[name] = true
	}
	return out
}

// withoutHost drops the host-agent markers from an environment.
func withoutHost(env []string) []string {
	host := hostEnvNames()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if key, _, _ := splitEnv(kv); host[key] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// TestE2EGlobalLegacyDirs walks add, list, update and remove through OpenClaw's legacy home, which
// must be used by every command and not only by path resolution.
func TestE2EGlobalLegacyDirs(t *testing.T) {
	fx := newUpdateFixture(t)
	project := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".clawdbot"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(home, ".clawdbot", "skills", "alpha")

	res := runGlobal(t, project, home, "add", fx.url+"#v1", "-a", "openclaw", "-g", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(legacy, "SKILL.md")); err != nil {
		t.Fatalf("the skill was not installed in the legacy home: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".openclaw")); !os.IsNotExist(err) {
		t.Errorf("the add created .openclaw next to the legacy home: %v", err)
	}

	res = runGlobal(t, project, home, "list", "-g")
	if res.exit != 0 || !strings.Contains(res.stdout, legacy) {
		t.Fatalf("list exit = %d, stdout:\n%s", res.exit, res.stdout)
	}

	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)
	res = runGlobal(t, project, home, "update", "-g")
	if res.exit != 0 {
		t.Fatalf("update exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err := os.ReadFile(filepath.Join(legacy, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading the updated copy: %v", err)
	}
	if !strings.Contains(string(raw), "version two") {
		t.Errorf("legacy copy = %q, want the advanced content", raw)
	}
	if _, err := os.Stat(filepath.Join(home, ".openclaw", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("update wrote into .openclaw: %v", err)
	}

	res = runGlobal(t, project, home, "remove", "alpha", "-g", "-y")
	if res.exit != 0 {
		t.Fatalf("remove exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("the legacy copy survived removal: %v", err)
	}
}

// globalLockPath is where the injected XDG_STATE_HOME places the global lock.
func globalLockPath(home string) string {
	return filepath.Join(home, "state", "skills", ".skill-lock.json")
}

// globalLockDoc is the subset of .skill-lock.json the tests assert on.
type globalLockDoc struct {
	Version            int                        `json:"version"`
	Skills             map[string]globalLockEntry `json:"skills"`
	LastSelectedAgents []string                   `json:"lastSelectedAgents"`
}

// globalLockEntry is one entry of the global lock.
type globalLockEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	Ref             string `json:"ref"`
	Commit          string `json:"commit"`
	SkillPath       string `json:"skillPath"`
	SkillName       string `json:"skillName"`
	SkillFolderHash string `json:"skillFolderHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	InstallMode     string `json:"installMode"`
}

// readGlobalLock decodes the global lock.
func readGlobalLock(t *testing.T, home string) globalLockDoc {
	t.Helper()
	raw, err := os.ReadFile(globalLockPath(home))
	if err != nil {
		t.Fatalf("reading the global lock: %v", err)
	}
	var doc globalLockDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding the global lock: %v\n%s", err, raw)
	}
	return doc
}

var (
	contentHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	timestampRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)
)

// TestE2EGlobalScopeRoundTrip installs, lists, checks, updates and removes a global skill with an
// isolated home, and proves the project lock is never touched.
func TestE2EGlobalScopeRoundTrip(t *testing.T) {
	fx := newGitFixture(t)
	home := t.TempDir()
	project := t.TempDir()

	res := runGlobal(t, project, home, "add", fx.url+"#v1", "-g", "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("add -g exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Fatalf("global add wrote the project lock: %v", err)
	}
	doc := readGlobalLock(t, home)
	if doc.Version != 3 {
		t.Errorf("version = %d, want 3", doc.Version)
	}
	for _, name := range []string{"alpha", "beta"} {
		entry, ok := doc.Skills[name]
		if !ok {
			t.Fatalf("global lock has no %q entry: %+v", name, doc.Skills)
		}
		if entry.SourceType != "git" || entry.SourceURL != fx.url || entry.Ref != "v1" {
			t.Errorf("%s entry = %+v, want the recorded git source and ref", name, entry)
		}
		if entry.Commit != fx.first {
			t.Errorf("%s commit = %q, want %s", name, entry.Commit, fx.first)
		}
		if !contentHashRE.MatchString(entry.SkillFolderHash) {
			t.Errorf("%s skillFolderHash = %q, want a 64-hex content hash", name, entry.SkillFolderHash)
		}
		if !timestampRE.MatchString(entry.InstalledAt) || !timestampRE.MatchString(entry.UpdatedAt) {
			t.Errorf("%s timestamps = %q/%q, want RFC 3339 with milliseconds",
				name, entry.InstalledAt, entry.UpdatedAt)
		}
	}
	alphaPath := filepath.Join(home, ".agents", "skills", "alpha")
	if doc.Skills["alpha"].SkillFolderHash != oracleHash(t, alphaPath) {
		t.Errorf("alpha hash = %q, want the installed content hash", doc.Skills["alpha"].SkillFolderHash)
	}
	installedAt := doc.Skills["alpha"].InstalledAt

	res = runGlobal(t, project, home, "list", "-g")
	if res.exit != 0 {
		t.Fatalf("list -g exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	for _, want := range []string{"Global Skills", "alpha", "beta", "Source: " + fx.url} {
		if !contains(res.stdout, want) {
			t.Errorf("list -g stdout misses %q:\n%s", want, res.stdout)
		}
	}

	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)
	res = runGlobal(t, project, home, "check", "-g")
	if res.exit != 1 {
		t.Fatalf("check -g exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	if !contains(res.stdout, "↑ alpha has an update") {
		t.Errorf("check -g stdout misses the update:\n%s", res.stdout)
	}

	res = runGlobal(t, project, home, "update", "-g")
	if res.exit != 0 {
		t.Fatalf("update -g exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err := os.ReadFile(filepath.Join(alphaPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("alpha SKILL.md missing: %v", err)
	}
	if !contains(string(raw), "version two") {
		t.Errorf("alpha content = %q, want the advanced commit", raw)
	}
	doc = readGlobalLock(t, home)
	alpha := doc.Skills["alpha"]
	if alpha.Commit != fx.second {
		t.Errorf("alpha commit = %q, want %s", alpha.Commit, fx.second)
	}
	if alpha.SkillFolderHash != oracleHash(t, alphaPath) {
		t.Errorf("alpha skillFolderHash = %q, want the reinstalled content hash", alpha.SkillFolderHash)
	}
	if alpha.InstalledAt != installedAt {
		t.Errorf("installedAt = %q, want the first install time %q", alpha.InstalledAt, installedAt)
	}
	if alpha.UpdatedAt < installedAt {
		t.Errorf("updatedAt = %q, want it refreshed", alpha.UpdatedAt)
	}

	untracked := filepath.Join(home, ".agents", "skills", "untracked")
	if err := os.MkdirAll(untracked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(untracked, "SKILL.md"),
		[]byte("---\nname: untracked\ndescription: kept\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res = runGlobal(t, project, home, "remove", "-g", "--all")
	if res.exit != 0 {
		t.Fatalf("remove -g --all exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	raw, err = os.ReadFile(globalLockPath(home))
	if err != nil {
		t.Fatalf("the global lock was deleted: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"skills": {}`)) {
		t.Errorf("global lock after the last removal =\n%s\nwant an empty skills map", raw)
	}
	doc = readGlobalLock(t, home)
	if len(doc.Skills) != 0 {
		t.Errorf("global lock still tracks %v", doc.Skills)
	}
	if _, err := os.Stat(untracked); err != nil {
		t.Errorf("remove --all deleted an untracked folder: %v", err)
	}
}

// TestE2EProjectLockV1Migration feeds a version 1 project lock and asserts the migrated hashes,
// the empty-hash entry and the check exit code.
func TestE2EProjectLockV1Migration(t *testing.T) {
	project := t.TempDir()
	alpha := filepath.Join(project, ".agents", "skills", "alpha")
	beta := filepath.Join(project, ".agents", "skills", "beta")
	writeFixtureFile(t, alpha, "SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	writeFixtureFile(t, alpha, "notes.txt", "keep\n")
	writeFixtureFile(t, beta, "SKILL.md", "---\nname: beta\ndescription: b\n---\n")

	legacy := `{
  "version": 1,
  "skills": {
    "alpha": {"source": "./src-alpha", "sourceType": "local", "computedHash": "` +
		legacyHash(t, alpha) + `"},
    "beta": {"source": "./src-beta", "sourceType": "local", "computedHash": "` +
		legacyHash(t, beta) + `"}
  }
}
`
	// beta was edited after the lock was written, so its legacy hash no longer matches.
	if err := os.WriteFile(filepath.Join(beta, "SKILL.md"),
		[]byte("---\nname: beta\ndescription: b\n---\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(project, "skills-lock.json")
	if err := os.WriteFile(lockPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "list")
	if res.exit != 0 {
		t.Fatalf("list exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if !contains(res.stdout, "alpha") || !contains(res.stdout, "beta") {
		t.Errorf("list stdout misses the v1 skills:\n%s", res.stdout)
	}
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte(`"version": 1`)) {
		t.Errorf("list rewrote the lock:\n%s", before)
	}

	res = run(t, project, "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	if !contains(res.stdout, "✎ beta has local changes") {
		t.Errorf("check stdout misses the empty-hash entry:\n%s", res.stdout)
	}
	if contains(res.stdout, "✎ alpha has local changes") {
		t.Error("alpha matched its legacy hash, so it must not be reported as changed")
	}

	// A local add of another skill forces the in-memory migration to be written.
	src := t.TempDir()
	writeFixtureFile(t, filepath.Join(src, "gamma"), "SKILL.md", "---\nname: gamma\ndescription: g\n---\n")
	res = run(t, project, "add", src, "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	doc := readLock(t, project)
	if doc.Version != 2 {
		t.Errorf("lock version = %d, want 2", doc.Version)
	}
	if got := doc.Skills["alpha"].ComputedHash; got != oracleHash(t, alpha) {
		t.Errorf("alpha computedHash = %q, want the migrated v2 hash", got)
	}
	if got := doc.Skills["beta"].ComputedHash; got != "" {
		t.Errorf("beta computedHash = %q, want an empty hash", got)
	}

	res = run(t, project, "check")
	if res.exit != 1 || !contains(res.stdout, "✎ beta has local changes") {
		t.Errorf("check after migration exit = %d, stdout = %q", res.exit, res.stdout)
	}
}

// TestE2EGlobalProjectOnlyAgentsFails checks the documented error for a named project-only agent
// and that no global lock is written.
func TestE2EGlobalProjectOnlyAgentsFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture uses POSIX paths")
	}
	source := sourceDir(t)
	for _, agent := range []string{"eve", "promptscript"} {
		t.Run(agent, func(t *testing.T) {
			home := t.TempDir()
			res := runGlobal(t, t.TempDir(), home, "add", source, "-g", "-a", agent, "-y")
			if res.exit != 1 {
				t.Fatalf("add -g -a %s exit = %d, want 1\nstderr:\n%s", agent, res.exit, res.stderr)
			}
			if !contains(res.stderr, "does not support global skill installation") {
				t.Errorf("stderr misses the documented error:\n%s", res.stderr)
			}
			if _, err := os.Stat(globalLockPath(home)); !os.IsNotExist(err) {
				t.Errorf("a failed global add wrote the lock: %v", err)
			}
		})
	}
}

// contains reports whether s holds want.
func contains(s, want string) bool { return strings.Contains(s, want) }

// legacyHash is an independent implementation of the version 1 content hash: regular files, the
// .git and node_modules directories skipped, slash paths ordered by code units, and path then
// content bytes with no framing.
func legacyHash(t *testing.T, dir string) string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(files)
	h := sha256.New()
	for _, rel := range files {
		content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(rel))
		h.Write(content)
	}
	return hex.EncodeToString(h.Sum(nil))
}
