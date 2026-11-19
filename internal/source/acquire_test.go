package source

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// requireGit skips a test when git is not installed or the platform lacks POSIX file URLs.
func requireGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local repository fixtures use POSIX paths")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// runGit runs a git command in dir, or in the current directory when dir is empty.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitGit commits the working tree of dir with a throwaway identity.
func commitGit(t *testing.T, dir, message string) string {
	t.Helper()
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com",
		"commit", "-q", "-m", message)
	return runGit(t, dir, "rev-parse", "HEAD")
}

func writeRepoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRepoFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// repoFixture is a bare repository served through a file:// URL, with two commits and a tag on
// the first.
type repoFixture struct {
	url    string
	bare   string
	first  string
	second string
}

func newRepoFixture(t *testing.T) repoFixture {
	t.Helper()
	work := t.TempDir()
	runGit(t, work, "init", "-q", "-b", "main")
	writeRepoFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nversion one\n")
	first := commitGit(t, work, "one")
	runGit(t, work, "tag", "v1")

	writeRepoFile(t, work, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nversion two\n")
	second := commitGit(t, work, "two")

	bare := filepath.Join(t.TempDir(), "repo.git")
	runGit(t, "", "clone", "-q", "--bare", work, bare)
	// Let the fixture server hand out a commit by SHA, which is what a commit pin fetches.
	runGit(t, bare, "config", "uploadpack.allowAnySHA1InWant", "true")

	u := url.URL{Scheme: "file", Path: filepath.ToSlash(bare)}
	return repoFixture{url: u.String(), bare: bare, first: first, second: second}
}

func TestAcquireLocal(t *testing.T) {
	dir := t.TempDir()
	src, err := Parse(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := Acquire(context.Background(), src, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Dir != dir || tree.Commit != "" {
		t.Errorf("Tree = %+v, want the directory and no commit", tree)
	}

	missing, err := Parse(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(context.Background(), missing, ""); err == nil ||
		!strings.Contains(err.Error(), "source not found") {
		t.Errorf("missing local source error = %v", err)
	}
}

func TestAcquireClonesDefaultBranch(t *testing.T) {
	requireGit(t)
	fx := newRepoFixture(t)
	dir := t.TempDir()

	tree, err := Acquire(context.Background(), Source{Kind: KindGit, Source: fx.url, SourceURL: fx.url}, dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit != fx.second {
		t.Errorf("commit = %s, want %s", tree.Commit, fx.second)
	}
	if got := readRepoFile(t, tree.Dir, "skills/alpha/SKILL.md"); !strings.Contains(got, "version two") {
		t.Errorf("checked out %q, want the default branch", got)
	}
}

func TestAcquireTagRef(t *testing.T) {
	requireGit(t)
	fx := newRepoFixture(t)
	tree, err := Acquire(context.Background(),
		Source{Kind: KindGit, Source: fx.url, SourceURL: fx.url, Ref: "v1"}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit != fx.first {
		t.Errorf("commit = %s, want the tagged %s", tree.Commit, fx.first)
	}
	if got := readRepoFile(t, tree.Dir, "skills/alpha/SKILL.md"); !strings.Contains(got, "version one") {
		t.Errorf("checked out %q, want the tag", got)
	}
}

func TestAcquireCommitPinSurvivesMovedTag(t *testing.T) {
	requireGit(t)
	fx := newRepoFixture(t)
	// Move the tag to the second commit: a pin must still land on the recorded one.
	runGit(t, fx.bare, "tag", "-f", "v1", fx.second)

	tree, err := Acquire(context.Background(),
		Source{Kind: KindGit, Source: fx.url, SourceURL: fx.url, Ref: fx.first}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit != fx.first {
		t.Errorf("commit = %s, want the pinned %s", tree.Commit, fx.first)
	}
	if got := readRepoFile(t, tree.Dir, "skills/alpha/SKILL.md"); !strings.Contains(got, "version one") {
		t.Errorf("checked out %q, want the pinned commit", got)
	}
}

func TestAcquireSubpath(t *testing.T) {
	requireGit(t)
	fx := newRepoFixture(t)

	tree, err := Acquire(context.Background(),
		Source{Kind: KindGit, Source: fx.url, SourceURL: fx.url, Subpath: "skills"}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if filepath.Base(tree.Dir) != "skills" {
		t.Errorf("Dir = %s, want the subpath directory", tree.Dir)
	}
	if _, err := os.Stat(filepath.Join(tree.Dir, "alpha", "SKILL.md")); err != nil {
		t.Errorf("subpath tree is incomplete: %v", err)
	}
}

func TestAcquireRejectsEscapingSubpath(t *testing.T) {
	requireGit(t)
	fx := newRepoFixture(t)

	tests := map[string]string{
		"outside":        "../outside",
		"missing":        "does/not/exist",
		"traversal deep": "skills/../../outside",
	}
	for name, subpath := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Acquire(context.Background(),
				Source{Kind: KindGit, Source: fx.url, SourceURL: fx.url, Subpath: subpath}, t.TempDir())
			if err == nil {
				t.Fatal("Acquire succeeded, want an error")
			}
			if !strings.Contains(err.Error(), "subpath") {
				t.Errorf("error = %v, want it to name the subpath", err)
			}
		})
	}
}
