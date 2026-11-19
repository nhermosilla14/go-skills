package source

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeGitScript = `#!/bin/sh
printf 'git %s\n' "$*" >> "$FAKE_LOG"
while [ "$1" = "-c" ]; do shift 2; done
if [ -n "$FAKE_FAIL_ONCE" ] && [ ! -e "$FAKE_FAIL_FLAG" ]; then
  : > "$FAKE_FAIL_FLAG"
  printf '%s\n' "$FAKE_FAIL_MESSAGE" >&2
  exit 128
fi
case "$1" in
  clone)
    for last in "$@"; do :; done
    /bin/mkdir -p "$last"
    ;;
  rev-parse)
    printf '%s\n' "${FAKE_COMMIT:-1111111111111111111111111111111111111111}"
    ;;
esac
exit 0
`

const fakeGHScript = `#!/bin/sh
printf 'gh %s\n' "$*" >> "$FAKE_LOG"
if [ -n "$FAKE_GH_FAIL" ]; then
  printf 'gh: authentication failed\n' >&2
  exit 1
fi
/bin/mkdir -p "$4"
exit 0
`

// fakePath installs fake git and, when asked, gh executables and makes them the whole PATH, so
// no real gh can be found by accident.
func fakePath(t *testing.T, withGH bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executables are POSIX shell scripts")
	}
	dir := t.TempDir()
	scripts := map[string]string{"git": fakeGitScript}
	if withGH {
		scripts["gh"] = fakeGHScript
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_LOG", filepath.Join(t.TempDir(), "commands.log"))
	return dir
}

func commandLog(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("FAKE_LOG"))
	if err != nil {
		t.Fatalf("reading the fake command log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func failOnce(t *testing.T, message string) {
	t.Helper()
	t.Setenv("FAKE_FAIL_ONCE", "1")
	t.Setenv("FAKE_FAIL_FLAG", filepath.Join(t.TempDir(), "failed"))
	t.Setenv("FAKE_FAIL_MESSAGE", message)
}

func findCommand(log []string, prefix string) int {
	for i, line := range log {
		if strings.HasPrefix(line, prefix) {
			return i
		}
	}
	return -1
}

type runnerFunc func(context.Context, Command) (Output, error)

func (f runnerFunc) Run(ctx context.Context, cmd Command) (Output, error) {
	return f(ctx, cmd)
}

func TestAcquireWithFakeRunner(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var log []Command
	runner := runnerFunc(func(_ context.Context, cmd Command) (Output, error) {
		log = append(log, cmd)
		if strings.Contains(strings.Join(cmd.Args, " "), "rev-parse") {
			return Output{Stdout: testSHA + "\n"}, nil
		}
		return Output{}, nil
	})

	tree, err := AcquireWith(context.Background(),
		Source{Kind: KindGitHub, Source: "acme/repo"}, t.TempDir(), runner)
	if err != nil {
		t.Fatalf("AcquireWith: %v", err)
	}
	if tree.Commit != testSHA {
		t.Errorf("commit = %q, want %q", tree.Commit, testSHA)
	}
	if len(log) != 2 {
		t.Fatalf("commands = %v, want a clone and a rev-parse", log)
	}
	if log[0].Name != "git" || !strings.Contains(strings.Join(log[0].Args, " "), "clone --depth 1") {
		t.Errorf("first command = %+v, want a shallow git clone", log[0])
	}
}

func TestAcquireAuthFirstAttemptSucceeds(t *testing.T) {
	fakePath(t, true)
	src := Source{Kind: KindGitHub, Source: "acme/repo"}

	tree, err := Acquire(context.Background(), src, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit != "1111111111111111111111111111111111111111" {
		t.Errorf("commit = %q", tree.Commit)
	}
	log := commandLog(t)
	if findCommand(log, "gh ") >= 0 {
		t.Errorf("gh was tried although the first clone succeeded: %v", log)
	}
	for _, line := range log {
		if strings.Contains(line, "submodule") {
			t.Errorf("submodules were followed: %s", line)
		}
	}
	if !strings.Contains(log[0], "clone --depth 1 -- https://github.com/acme/repo.git") {
		t.Errorf("first command = %q, want a shallow clone", log[0])
	}
}

func TestAcquireAuth403FallsBackToGH(t *testing.T) {
	fakePath(t, true)
	failOnce(t, "fatal: unable to access 'https://github.com/acme/repo.git/': The requested URL returned error: 403")

	tree, err := Acquire(context.Background(), Source{Kind: KindGitHub, Source: "acme/repo"}, t.TempDir())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if tree.Commit == "" {
		t.Error("no commit recorded")
	}
	log := commandLog(t)
	gh := findCommand(log, "gh repo clone acme/repo")
	if gh < 0 {
		t.Fatalf("gh was not tried after a 403: %v", log)
	}
	if findCommand(log[gh:], "git ") < 0 {
		t.Errorf("the commit was not read after the gh clone: %v", log)
	}
}

func TestAcquireAuthGHMissingFallsBackToSSH(t *testing.T) {
	fakePath(t, false)
	failOnce(t, "fatal: unable to access 'https://github.com/acme/repo.git/': The requested URL returned error: 403")

	if _, err := Acquire(context.Background(), Source{Kind: KindGitHub, Source: "acme/repo"}, t.TempDir()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	log := strings.Join(commandLog(t), "\n")
	if !strings.Contains(log, "git@github.com:acme/repo.git") {
		t.Errorf("SSH was not tried after gh was unavailable: %v", commandLog(t))
	}
}

func TestAcquireAuthServerErrorDoesNotRetry(t *testing.T) {
	fakePath(t, true)
	failOnce(t, "fatal: unable to access 'https://github.com/acme/repo.git/': The requested URL returned error: 500")

	_, err := Acquire(context.Background(), Source{Kind: KindGitHub, Source: "acme/repo"}, t.TempDir())
	if err == nil {
		t.Fatal("Acquire succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want the server error detail", err)
	}
	if !strings.Contains(err.Error(), "after trying git:") {
		t.Errorf("error = %v, want it to name only the git attempt", err)
	}
	if strings.Contains(strings.Join(commandLog(t), "\n"), "gh ") {
		t.Errorf("gh was tried after a server error: %v", commandLog(t))
	}
}

func TestAcquireAuthRedactsCredentials(t *testing.T) {
	fakePath(t, true)
	failOnce(t, "fatal: unable to access 'https://user:s3cret@github.com/acme/repo.git/': "+
		"The requested URL returned error: 500")

	_, err := Acquire(context.Background(), Source{Kind: KindGitHub, Source: "acme/repo"}, t.TempDir())
	if err == nil {
		t.Fatal("Acquire succeeded, want an error")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Errorf("error echoes the credential: %v", err)
	}
	if !strings.Contains(err.Error(), "***@") {
		t.Errorf("error = %v, want the redacted userinfo", err)
	}
}

func TestAcquireCommitPinFetchesCommit(t *testing.T) {
	fakePath(t, false)
	src := Source{Kind: KindGitHub, Source: "acme/repo", Ref: testSHA}

	if _, err := Acquire(context.Background(), src, t.TempDir()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	log := strings.Join(commandLog(t), "\n")
	if !strings.Contains(log, "fetch --depth 1 origin "+testSHA) {
		t.Errorf("pinned commit was not fetched:\n%s", log)
	}
	if !strings.Contains(log, "checkout --detach FETCH_HEAD") {
		t.Errorf("FETCH_HEAD was not checked out:\n%s", log)
	}
	if strings.Contains(log, "--branch") {
		t.Errorf("a commit pin used --branch:\n%s", log)
	}
}

func TestAcquireGitMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH handling differs on Windows")
	}
	t.Setenv("PATH", t.TempDir())
	_, err := Acquire(context.Background(),
		Source{Kind: KindGit, Source: "https://example.com/o/r.git", SourceURL: "https://example.com/o/r.git"},
		t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "git is required") {
		t.Fatalf("error = %v, want a missing-git message", err)
	}
}
