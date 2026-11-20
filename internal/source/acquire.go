package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Tree is the result of acquiring a source: the directory discovery starts from and, for git
// sources, the full commit that was checked out.
type Tree struct {
	Dir    string
	Commit string
	// SkillPath is the SKILL.md path relative to Dir when the acquisition is one known file, which
	// only a direct download of a single SKILL.md is. It is empty otherwise, and discovery then
	// derives the path.
	SkillPath string
	// WellKnown is set when the acquisition came from a host's well-known provider. It carries the
	// per-skill digest and base URL the lock records.
	WellKnown *WellKnownContent
}

// Command is one external command for a Runner.
type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

// Output is the captured output of a command.
type Output struct {
	Stdout string
	Stderr string
}

// Runner executes the external commands of a git acquisition. Tests replace it to observe or fake
// the git and gh invocations.
type Runner interface {
	Run(ctx context.Context, cmd Command) (Output, error)
}

// ExecRunner runs commands from PATH, which is what production uses.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, cmd Command) (Output, error) {
	c := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	c.Dir = cmd.Dir
	if cmd.Env != nil {
		c.Env = cmd.Env
	}
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	return Output{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

// Acquire resolves a source: a local directory is returned as is, a download is materialized into
// dir, and a git source is checked out into dir, which the caller owns and removes.
func Acquire(ctx context.Context, src Source, dir string) (Tree, error) {
	if src.needsGit() {
		if _, err := exec.LookPath("git"); err != nil {
			return Tree{}, errMissingGit
		}
	}
	return AcquireWith(ctx, src, dir, ExecRunner{})
}

// needsGit reports whether acquiring the source clones a repository.
func (s Source) needsGit() bool {
	switch s.Kind {
	case KindGitHub, KindGitLab, KindAzure, KindGit:
		return true
	}
	return false
}

// errMissingGit is reported before any clone is attempted.
var errMissingGit = errors.New("git is required to install from a git source but was not found on PATH; install git and try again")

// AcquireWith is Acquire with an explicit runner, for tests. It does not require git on PATH, so a
// fake runner can be used without the real binary.
func AcquireWith(ctx context.Context, src Source, dir string, runner Runner) (Tree, error) {
	if src.Kind == KindLocal {
		info, err := os.Stat(src.SourceURL)
		if err != nil {
			return Tree{}, fmt.Errorf("source not found: %s", src.SourceURL)
		}
		if !info.IsDir() {
			return Tree{}, fmt.Errorf("source is not a directory: %s", src.SourceURL)
		}
		return Tree{Dir: filepath.Clean(src.SourceURL)}, nil
	}
	if src.Kind == KindDownload {
		if dir == "" {
			return Tree{}, errors.New("no temporary directory was provided for the download")
		}
		if src.IsNotion() {
			return acquireNotion(ctx, src, dir)
		}
		dl, err := FetchDownload(ctx, src.CloneURL(), dir)
		if err != nil {
			return Tree{}, err
		}
		return Tree{Dir: dl.Root, SkillPath: dl.SkillPath}, nil
	}
	if src.Kind == KindNotion {
		return Tree{}, errors.New("the Notion pack browser must select packs before acquisition")
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	if dir == "" {
		return Tree{}, errors.New("no temporary directory was provided for the git checkout")
	}
	url := src.CloneURL()
	if strings.HasPrefix(strings.ToLower(url), "ext::") {
		return Tree{}, errors.New("unsupported git transport: ext")
	}
	a := &gitAcquirer{runner: runner, env: gitEnv()}
	return a.acquire(ctx, src, dir)
}

// gitConfig is passed to every git invocation. The empty LFS filters keep clones working without
// git-lfs installed; long paths keep Windows checkouts under MAX_PATH working.
var gitConfig = []string{
	"-c", "filter.lfs.required=false",
	"-c", "filter.lfs.smudge=",
	"-c", "filter.lfs.clean=",
	"-c", "filter.lfs.process=",
	"-c", "core.longpaths=true",
}

// gitAcquirer clones one source, trying the authentication fallbacks in upstream's order.
type gitAcquirer struct {
	runner Runner
	env    []string
}

func (a *gitAcquirer) acquire(ctx context.Context, src Source, dir string) (Tree, error) {
	url := src.CloneURL()
	attempts := []string{"git"}
	last, err := a.cloneWithGit(ctx, url, dir, src.Ref, "")
	if err == nil {
		return a.finish(ctx, src, dir)
	}
	if !isGitHubHTTPS(url) || !authFailure(last) {
		return Tree{}, cloneError(url, attempts, last, err)
	}

	if _, lookErr := exec.LookPath("gh"); lookErr == nil {
		attempts = append(attempts, "gh repo clone")
		if resetErr := resetDir(dir); resetErr != nil {
			return Tree{}, resetErr
		}
		if _, ghErr := a.cloneWithGH(ctx, url, dir, src.Ref); ghErr == nil {
			return a.finish(ctx, src, dir)
		}
	}

	attempts = append(attempts, "ssh")
	if resetErr := resetDir(dir); resetErr != nil {
		return Tree{}, resetErr
	}
	last, err = a.cloneWithGit(ctx, sshCloneURL(url), dir, src.Ref, "ssh -o BatchMode=yes")
	if err == nil {
		return a.finish(ctx, src, dir)
	}
	return Tree{}, cloneError(url, attempts, last, err)
}

// cloneWithGit clones url into dir. A branch or tag is cloned shallowly; a full commit SHA is
// fetched directly, because git clone --branch cannot check out a commit.
func (a *gitAcquirer) cloneWithGit(ctx context.Context, url, dir, ref, sshCommand string) (Output, error) {
	env := a.env
	if sshCommand != "" {
		env = append(append([]string{}, env...), "GIT_SSH_COMMAND="+sshCommand)
	}
	if isCommitSHA(ref) {
		if out, err := a.git(ctx, dir, env, "init"); err != nil {
			return out, err
		}
		if out, err := a.git(ctx, dir, env, "remote", "add", "origin", url); err != nil {
			return out, err
		}
		return a.fetchCommit(ctx, dir, env, ref)
	}
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dir)
	return a.git(ctx, "", env, args...)
}

// cloneWithGH clones a GitHub repository through the gh CLI, which can use its own stored
// credential without the CLI ever reading it.
func (a *gitAcquirer) cloneWithGH(ctx context.Context, url, dir, ref string) (Output, error) {
	slug := githubSlug(url)
	if slug == "" {
		return Output{}, errors.New("not a GitHub repository")
	}
	args := []string{"repo", "clone", slug, dir, "--", "--depth=1"}
	if ref != "" && !isCommitSHA(ref) {
		args = append(args, "--branch", ref)
	}
	out, err := a.runner.Run(ctx, Command{Name: "gh", Args: args, Env: a.env})
	if err != nil {
		return out, err
	}
	if isCommitSHA(ref) {
		return a.fetchCommit(ctx, dir, a.env, ref)
	}
	return out, nil
}

func (a *gitAcquirer) fetchCommit(ctx context.Context, dir string, env []string, sha string) (Output, error) {
	out, err := a.git(ctx, dir, env, "fetch", "--depth", "1", "origin", sha)
	if err != nil {
		return out, err
	}
	return a.git(ctx, dir, env, "checkout", "--detach", "FETCH_HEAD")
}

// git runs one git command with the forced configuration and environment.
func (a *gitAcquirer) git(ctx context.Context, dir string, env []string, args ...string) (Output, error) {
	full := append(append([]string{}, gitConfig...), args...)
	return a.runner.Run(ctx, Command{Name: "git", Args: full, Dir: dir, Env: env})
}

// finish records the commit and validates the subpath against the real checked-out tree.
func (a *gitAcquirer) finish(ctx context.Context, src Source, dir string) (Tree, error) {
	out, err := a.git(ctx, dir, a.env, "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}")
	if err != nil {
		if text := detail(out); text != "" {
			return Tree{}, fmt.Errorf("reading the checked out commit: %s", text)
		}
		return Tree{}, fmt.Errorf("reading the checked out commit: %w", err)
	}
	commit := strings.ToLower(strings.TrimSpace(out.Stdout))
	if !commitRE.MatchString(commit) {
		return Tree{}, fmt.Errorf("could not read the checked out commit of %s", src.CloneURL())
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Tree{}, fmt.Errorf("resolving the checkout: %w", err)
	}
	target := root
	if src.Subpath != "" {
		sub, err := containedPath(root, src.Subpath)
		if err != nil {
			return Tree{}, err
		}
		target = sub
	}
	return Tree{Dir: target, Commit: commit}, nil
}

// containedPath resolves subpath under root and rejects anything that escapes it, including
// through a symlink.
func containedPath(root, subpath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(subpath)))
	if err != nil {
		return "", fmt.Errorf("subpath %q does not exist in the repository", subpath)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid subpath %q: it resolves outside the repository", subpath)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid subpath %q: it is not a directory", subpath)
	}
	return resolved, nil
}

// resetDir empties dir between authentication attempts.
func resetDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("cleaning the temporary directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("recreating the temporary directory: %w", err)
	}
	return nil
}

// cloneError turns the last attempt into a user-facing error that names each attempt and never
// carries credentials.
func cloneError(url string, attempts []string, out Output, err error) error {
	if authFailure(out) {
		return fmt.Errorf("authentication failed for %s after trying %s; check your git credentials, "+
			"run 'gh auth status', or load your SSH key", url, strings.Join(attempts, ", "))
	}
	if text := detail(out); text != "" {
		return fmt.Errorf("failed to clone %s after trying %s: %s", url, strings.Join(attempts, ", "), text)
	}
	if err != nil {
		return fmt.Errorf("failed to clone %s after trying %s: %s", url, strings.Join(attempts, ", "), RedactCredentials(err.Error()))
	}
	return fmt.Errorf("failed to clone %s after trying %s", url, strings.Join(attempts, ", "))
}

// authPatterns are the stderr fragments that mean a different credential may help. Anything else,
// including a 5xx, is not retried.
var authPatterns = []string{
	"authentication failed",
	"could not read username",
	"terminal prompts disabled",
	"permission denied",
	"repository not found",
	"access denied",
	"returned error: 401",
	"returned error: 403",
	"returned error: 404",
}

func authFailure(out Output) bool {
	text := strings.ToLower(out.Stdout + "\n" + out.Stderr)
	for _, pattern := range authPatterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

// credentialsRE matches the userinfo of a URL so error text cannot echo it.
var credentialsRE = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)

// RedactCredentials removes the userinfo of every URL in s, so a source read from a lock file can
// never print a password. It is exported for the callers that render recorded sources.
func RedactCredentials(s string) string {
	return credentialsRE.ReplaceAllString(s, "${1}***@")
}

// detail renders a command result for an error message.
func detail(out Output) string {
	text := strings.TrimSpace(RedactCredentials(strings.TrimSpace(out.Stdout + "\n" + out.Stderr)))
	if runes := []rune(text); len(runes) > 800 {
		text = string(runes[:800]) + "…"
	}
	return text
}

func isCommitSHA(ref string) bool {
	return commitRE.MatchString(ref)
}

// isGitHubHTTPS reports whether url is an https GitHub URL, the only case that gets the gh and
// SSH fallbacks.
func isGitHubHTTPS(raw string) bool {
	u, err := parseCloneURL(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https") && isGitHubHost(u.Hostname())
}

// githubSlug returns owner/repo for a GitHub HTTPS URL.
func githubSlug(raw string) string {
	u, err := parseCloneURL(raw)
	if err != nil || !isGitHubHost(u.Hostname()) {
		return ""
	}
	segs := pathSegments(u)
	if len(segs) != 2 {
		return ""
	}
	repo := strings.TrimSuffix(segs[1], ".git")
	if repo == "" {
		return ""
	}
	return segs[0] + "/" + repo
}

// sshCloneURL returns the scp-like SSH form of a GitHub HTTPS URL.
func sshCloneURL(raw string) string {
	u, err := parseCloneURL(raw)
	if err != nil {
		return raw
	}
	p := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(strings.ToLower(p), ".git") {
		p += ".git"
	}
	return "git@" + u.Hostname() + ":" + p
}

func parseCloneURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

// gitEnv is the process environment with git's required variables forced. The user's credential
// helpers, SSH agent and proxies stay inherited; no credential is read or added here.
func gitEnv() []string {
	overrides := map[string]string{
		"LC_ALL":              "C",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_ALLOW_PROTOCOL":  "https:http:ssh:git:file",
		"GIT_LFS_SKIP_SMUDGE": "1",
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if _, replaced := overrides[key]; replaced {
			continue
		}
		env = append(env, kv)
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}
