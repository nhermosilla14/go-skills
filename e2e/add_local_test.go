// Package e2e runs the built skills binary as a user would and checks the files it leaves behind.
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
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// skillsBin is the binary built once by TestMain.
var skillsBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "skills-e2e-bin-")
	if err != nil {
		panic(err)
	}
	// Every end-to-end run must be blind to the developer's real home and global skills. A test
	// that once ran `update -g` against the real home rewrote global skills; isolating HOME and
	// the XDG/Windows config roots here makes that impossible for the whole package.
	home, err := os.MkdirTemp("", "skills-e2e-home-")
	if err != nil {
		_ = os.RemoveAll(dir)
		panic(err)
	}
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		if err := os.Setenv(key, filepath.Join(home, key)); err != nil {
			_ = os.RemoveAll(dir)
			_ = os.RemoveAll(home)
			panic(err)
		}
	}
	name := "skills"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	skillsBin = filepath.Join(dir, name)
	build := exec.Command("go", "build", "-trimpath", "-o", skillsBin,
		"-ldflags", "-X main.version=0.0.0-test -X main.commit=0000000 -X main.date=2000-01-01T00:00:00Z",
		"../cmd/skills")
	if out, err := build.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		os.Stderr.Write(out)
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// result is the outcome of one run of the binary, with its streams kept apart.
type result struct {
	stdout, stderr string
	exit           int
}

func run(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runEnv(t, dir, nil, args...)
}

// runEnv runs the binary with extra environment entries appended to the process environment.
func runEnv(t *testing.T, dir string, extraEnv []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
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

// lockDoc is the subset of skills-lock.json the test asserts on.
type lockDoc struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

type lockEntry struct {
	Source       string `json:"source"`
	SourceURL    string `json:"sourceUrl"`
	SourceType   string `json:"sourceType"`
	Ref          string `json:"ref"`
	Commit       string `json:"commit"`
	SkillPath    string `json:"skillPath"`
	SkillName    string `json:"skillName"`
	ComputedHash string `json:"computedHash"`
	InstallMode  string `json:"installMode"`
}

// oracleHash is an independent implementation of the content hash contract, written from the
// published description and not shared with internal/hash: sorted relative paths, framing
// "path NUL decimal-length NUL content", SHA-256 in lowercase hex.
func oracleHash(t *testing.T, dir string) string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "metadata.json" {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "__pycache__" || d.Name() == "__pypackages__" {
				return filepath.SkipDir
			}
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
		content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(content))))
		h.Write([]byte{0})
		h.Write(content)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestE2EAddLocalInstall(t *testing.T) {
	project := t.TempDir()
	source, err := filepath.Abs("testdata/source")
	if err != nil {
		t.Fatal(err)
	}

	// Two agents with different skills directories: symlink mode, canonical copy plus links.
	res := run(t, project, "add", source, "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty for add", res.stdout)
	}
	if !strings.Contains(res.stderr, "warning: skipped skills/broken/SKILL.md") {
		t.Errorf("stderr misses the skipped-skill warning:\n%s", res.stderr)
	}

	installed := []string{"flat-skill", "nested-skill"}
	for _, name := range installed {
		canonical := filepath.Join(project, ".agents", "skills", name)
		if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
			t.Errorf("%s: canonical SKILL.md missing: %v", name, err)
		}
		link := filepath.Join(project, ".claude", "skills", name)
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("%s: %s is not a symlink: %v", name, link, err)
		}
		// Junctions, the Windows link, store absolute targets by design.
		if runtime.GOOS != "windows" && filepath.IsAbs(target) {
			t.Errorf("%s: link target %q is absolute", name, target)
		}
		if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
			t.Errorf("%s: link does not resolve: %v", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(project, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(installed) {
		t.Errorf("canonical dir has %d entries, want %d", len(entries), len(installed))
	}

	raw, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("lock missing: %v", err)
	}
	if !bytes.HasSuffix(raw, []byte("}\n")) {
		t.Errorf("lock lacks trailing newline")
	}
	var lock lockDoc
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("lock is not JSON: %v", err)
	}
	if lock.Version != 2 {
		t.Errorf("version = %d, want 2", lock.Version)
	}
	if len(lock.Skills) != len(installed) {
		t.Errorf("lock skills = %d, want %d", len(lock.Skills), len(installed))
	}
	for _, name := range installed {
		entry, ok := lock.Skills[name]
		if !ok {
			t.Errorf("lock has no %s entry", name)
			continue
		}
		if entry.SourceType != "local" {
			t.Errorf("%s sourceType = %q, want local", name, entry.SourceType)
		}
		if want := oracleHash(t, filepath.Join(project, ".agents", "skills", name)); entry.ComputedHash != want {
			t.Errorf("%s computedHash = %s, oracle says %s", name, entry.ComputedHash, want)
		}
	}
	flat := lock.Skills["flat-skill"]
	if flat.SkillPath != "" {
		t.Errorf("flat skillPath = %q, want omitted for local sources", flat.SkillPath)
	}
	// Hard-coded for flat-skill, computed with coreutils:
	//   { printf 'SKILL.md\0'; wc -c < SKILL.md | tr -d '\n'; printf '\0'; cat SKILL.md; } | sha256sum
	const flatHash = "201ab10af2c20a44b94d63f0a9592553dd45102240f496bd1ea37d81918bfbff"
	if flat.ComputedHash != flatHash {
		t.Errorf("flat computedHash = %s, want %s", flat.ComputedHash, flatHash)
	}
}

func TestE2EAddRootSource(t *testing.T) {
	project := t.TempDir()
	source, err := filepath.Abs("testdata/root-source")
	if err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "add", source, "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	canonical := filepath.Join(project, ".agents", "skills")
	entries, err := os.ReadDir(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "root-skill" {
		t.Fatalf("installed %v, want only root-skill: the root SKILL.md shadows the tree", entries)
	}
	if _, err := os.Stat(filepath.Join(canonical, "hidden-skill")); !os.IsNotExist(err) {
		t.Errorf("hidden-skill was installed despite the root SKILL.md shadowing it")
	}
}

func TestE2EDiscoveryMatrix(t *testing.T) {
	project := t.TempDir()
	source, err := filepath.Abs("testdata/matrix")
	if err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "add", source, "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "warning: skipped skills/shadow/SKILL.md") {
		t.Errorf("stderr misses the shadowed-skill warning:\n%s", res.stderr)
	}
	for _, hidden := range []string{"matrix-inner", "matrix-hidden", "matrix-too-deep"} {
		if strings.Contains(res.stderr, hidden) {
			t.Errorf("stderr mentions %s:\n%s", hidden, res.stderr)
		}
	}

	entries, err := os.ReadDir(filepath.Join(project, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	var installed []string
	for _, e := range entries {
		installed = append(installed, e.Name())
	}
	want := []string{"agent-skill", "config-skill", "matrix-cat", "matrix-flat", "matrix-nested", "plugin-one", "plugin-two"}
	if !reflect.DeepEqual(installed, want) {
		t.Errorf("installed = %v, want %v", installed, want)
	}

	raw, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("lock missing: %v", err)
	}
	var lock lockDoc
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("lock is not JSON: %v", err)
	}
	if len(lock.Skills) != len(want) {
		t.Errorf("lock skills = %d, want %d", len(lock.Skills), len(want))
	}
	for _, name := range want {
		if _, ok := lock.Skills[name]; !ok {
			t.Errorf("lock has no %s entry", name)
		}
	}
}

func TestE2ERename(t *testing.T) {
	project := t.TempDir()
	source, err := filepath.Abs("testdata/rename-source")
	if err != nil {
		t.Fatal(err)
	}
	sourceFile := filepath.Join(source, "skills", "alpha", "SKILL.md")
	before, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "add", source, "-a", "opencode", "-a", "claude-code", "-y", "--rename", "alpha-skill")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}

	installed := filepath.Join(project, ".agents", "skills", "alpha-skill", "SKILL.md")
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("installed SKILL.md missing: %v", err)
	}
	if !strings.HasPrefix(string(got), "\uFEFF---\r\nname: alpha-skill\r\n") {
		t.Errorf("installed frontmatter = %q, want the rewritten name with BOM and CRLF", got)
	}
	for _, keep := range []string{"description: alpha skill\r\n", "license: MIT\r\n", "  internal: false\r\n"} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("installed frontmatter lost %q:\n%s", keep, got)
		}
	}
	if strings.Contains(string(got), "Alpha Skill") {
		t.Errorf("installed frontmatter kept the upstream name:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "alpha")); !os.IsNotExist(err) {
		t.Errorf("upstream folder name was installed: %v", err)
	}

	after, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the source SKILL.md was modified")
	}

	raw, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("lock missing: %v", err)
	}
	var lock lockDoc
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("lock is not JSON: %v", err)
	}
	entry, ok := lock.Skills["alpha-skill"]
	if !ok {
		t.Fatalf("lock has no alpha-skill entry: %+v", lock.Skills)
	}
	if entry.SkillName != "Alpha Skill" {
		t.Errorf("skillName = %q, want the upstream name", entry.SkillName)
	}
}

func TestE2EListIgnoresRename(t *testing.T) {
	project := t.TempDir()
	source, err := filepath.Abs("testdata/rename-source")
	if err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "add", source, "--list", "--rename", "Not Safe")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "Alpha Skill\t") {
		t.Errorf("list output = %q, want the upstream name", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("--list wrote a lock: %v", err)
	}
}

func TestE2EFullDepth(t *testing.T) {
	project := t.TempDir()
	source, err := filepath.Abs("testdata/root-source")
	if err != nil {
		t.Fatal(err)
	}

	res := run(t, project, "add", source, "--full-depth", "-a", "opencode", "-a", "claude-code", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	entries, err := os.ReadDir(filepath.Join(project, ".agents", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	var installed []string
	for _, e := range entries {
		installed = append(installed, e.Name())
	}
	want := []string{"hidden-skill", "root-skill"}
	if !reflect.DeepEqual(installed, want) {
		t.Errorf("installed = %v, want %v", installed, want)
	}
}

func TestE2ERenameRejected(t *testing.T) {
	project := t.TempDir()
	one, err := filepath.Abs("testdata/rename-source")
	if err != nil {
		t.Fatal(err)
	}
	many, err := filepath.Abs("testdata/matrix")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unsafe name", []string{"add", one, "-y", "--rename", "Bad Name"}, "--rename must be a skill name"},
		{"several skills", []string{"add", many, "-y", "--rename", "renamed"}, "installs one skill under a new name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := run(t, project, tc.args...)
			if res.exit != 1 {
				t.Fatalf("exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
			}
			if !strings.Contains(res.stderr, tc.want) {
				t.Errorf("stderr = %q, want containing %q", res.stderr, tc.want)
			}
		})
	}
}

// TestE2EAgentMatrix installs one skill for a universal agent, a non-universal agent with its
// project directory present, one without it, and Eve, then repeats in copy mode.
func TestE2EAgentMatrix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("link types and Eve flat paths are POSIX-specific")
	}
	source, err := filepath.Abs("testdata/source")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("symlink mode", func(t *testing.T) {
		project := t.TempDir()
		if err := os.MkdirAll(filepath.Join(project, ".windsurf"), 0o755); err != nil {
			t.Fatal(err)
		}
		home := t.TempDir()
		res := runEnv(t, project, []string{"HOME=" + home},
			"add", source, "-s", "flat-skill", "-y",
			"-a", "codex", "-a", "windsurf", "-a", "kiro-cli", "-a", "eve")
		if res.exit != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
		}

		canonical := filepath.Join(project, ".agents", "skills", "flat-skill")
		if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
			t.Errorf("canonical copy missing: %v", err)
		}
		link := filepath.Join(project, ".windsurf", "skills", "flat-skill")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("windsurf destination is not a symlink: %v", err)
		}
		if filepath.IsAbs(target) {
			t.Errorf("link target %q is absolute", target)
		}
		if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
			t.Errorf("windsurf link does not resolve: %v", err)
		}
		if _, err := os.Stat(filepath.Join(project, ".kiro")); !os.IsNotExist(err) {
			t.Errorf("kiro project directory was created: %v", err)
		}
		flat := filepath.Join(project, "agent", "skills", "flat-skill.md")
		raw, err := os.ReadFile(flat)
		if err != nil {
			t.Fatalf("eve flat file missing: %v", err)
		}
		if want := "---\nname: flat-skill\ndescription: Flat skill fixture\n---\nFlat body.\n"; string(raw) != want {
			t.Errorf("eve flat file = %q, want %q", raw, want)
		}
		if _, err := os.Stat(filepath.Join(project, "agent", "skills", "flat-skill")); !os.IsNotExist(err) {
			t.Errorf("eve also wrote a directory: %v", err)
		}
		for _, want := range []string{
			"installed: flat-skill -> codex",
			"installed: flat-skill -> windsurf",
			"installed: flat-skill -> eve",
			"skipped: flat-skill -> kiro-cli",
			"project directory not found",
			"Installed 3, skipped 1, failed 0",
		} {
			if !strings.Contains(res.stderr, want) {
				t.Errorf("stderr misses %q:\n%s", want, res.stderr)
			}
		}

		lock := readLock(t, project)
		entry, ok := lock.Skills["flat-skill"]
		if !ok {
			t.Fatalf("lock has no flat-skill entry: %+v", lock.Skills)
		}
		if entry.InstallMode != "" {
			t.Errorf("installMode = %q, want omitted", entry.InstallMode)
		}
		if want := oracleHash(t, canonical); entry.ComputedHash != want {
			t.Errorf("computedHash = %s, oracle says %s", entry.ComputedHash, want)
		}
	})

	t.Run("copy mode", func(t *testing.T) {
		project := t.TempDir()
		if err := os.MkdirAll(filepath.Join(project, ".windsurf"), 0o755); err != nil {
			t.Fatal(err)
		}
		home := t.TempDir()
		res := runEnv(t, project, []string{"HOME=" + home},
			"add", source, "-s", "flat-skill", "-y", "--copy",
			"-a", "windsurf", "-a", "kiro-cli", "-a", "eve")
		if res.exit != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
		}
		for _, dir := range []string{
			filepath.Join(project, ".windsurf", "skills", "flat-skill"),
			filepath.Join(project, ".kiro", "skills", "flat-skill"),
		} {
			info, err := os.Lstat(dir)
			if err != nil {
				t.Fatalf("%s missing: %v", dir, err)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				t.Errorf("%s is not a plain directory", dir)
			}
			if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
				t.Errorf("%s is incomplete: %v", dir, err)
			}
		}
		if _, err := os.Stat(filepath.Join(project, "agent", "skills", "flat-skill.md")); err != nil {
			t.Errorf("eve flat file missing: %v", err)
		}
		lock := readLock(t, project)
		if entry, _ := lock.Skills["flat-skill"]; entry.InstallMode != "copy" {
			t.Errorf("installMode = %q, want copy", entry.InstallMode)
		}
	})
}

// readLock parses the project lock the same way the other tests do.
func readLock(t *testing.T, project string) lockDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(project, "skills-lock.json"))
	if err != nil {
		t.Fatalf("lock missing: %v", err)
	}
	var lock lockDoc
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatalf("lock is not JSON: %v", err)
	}
	return lock
}
