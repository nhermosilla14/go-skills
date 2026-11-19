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
	os.Exit(code)
}

// result is the outcome of one run of the binary, with its streams kept apart.
type result struct {
	stdout, stderr string
	exit           int
}

func run(t *testing.T, dir string, args ...string) result {
	t.Helper()
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = dir
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
	SourceType   string `json:"sourceType"`
	SkillPath    string `json:"skillPath"`
	ComputedHash string `json:"computedHash"`
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
		if filepath.IsAbs(target) {
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
