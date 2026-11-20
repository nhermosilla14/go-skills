package source

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// envGitHubAPIURL overrides the GitHub API base. Tests point the fast path at a local stub; an
// empty or invalid value keeps the public API.
const envGitHubAPIURL = "SKILLS_GITHUB_API_URL"

// blobFallbackNotice is the single informational line a failed fast path logs before cloning.
const blobFallbackNotice = "GitHub API unavailable; falling back to git clone"

// githubAPIBase overrides the API host in tests. The environment wins over it, so an end-to-end
// run can direct the built binary at a stub without a special build.
var githubAPIBase string

// maxBlobResponse bounds one GitHub API response, including a base64 blob.
var maxBlobResponse int64 = 32 << 20

// noticeWriter receives the fallback line. Tests replace it to observe the message.
var noticeWriter io.Writer = os.Stderr

// blobAcquire reads a GitHub source through the Trees and blobs API without git. ok is false when
// the caller must clone; a failure logs one line and leaves dir empty for the clone. A reset error,
// which would make the clone unsafe, is returned.
func blobAcquire(ctx context.Context, src Source, dir string) (Tree, bool, error) {
	slug := blobSlug(src)
	if slug == "" || dir == "" || ctx.Err() != nil {
		return Tree{}, false, nil
	}
	tree, err := fetchBlobTree(ctx, slug, src, dir)
	if err == nil {
		return tree, true, nil
	}
	fmt.Fprintln(noticeWriter, blobFallbackNotice)
	if resetErr := resetDir(dir); resetErr != nil {
		return Tree{}, false, resetErr
	}
	return Tree{}, false, nil
}

// blobSlug reports the owner/repo of a source the fast path may read: GitHub only, with no ref or
// with a full commit SHA, which the API can express exactly.
func blobSlug(src Source) string {
	if src.Kind != KindGitHub {
		return ""
	}
	if src.Ref != "" && !isCommitSHA(src.Ref) {
		return ""
	}
	return githubSlug(src.CloneURL())
}

// fetchBlobTree resolves the commit, reads its recursive tree, writes the selected subtree, and
// verifies every file against its tree entry.
func fetchBlobTree(ctx context.Context, slug string, src Source, dir string) (Tree, error) {
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return Tree{}, fmt.Errorf("invalid GitHub repository %q", slug)
	}
	api := &blobAPI{base: blobAPIBase(), owner: owner, name: name, runner: ExecRunner{}}
	commit := strings.ToLower(src.Ref)
	if commit == "" {
		var err error
		commit, err = api.defaultCommit(ctx)
		if err != nil {
			return Tree{}, err
		}
	}
	doc, err := api.tree(ctx, commit)
	if err != nil {
		return Tree{}, err
	}
	root := strings.Trim(strings.ReplaceAll(src.Subpath, `\`, "/"), "/")
	blobs, err := selectBlobs(doc, root)
	if err != nil {
		return Tree{}, err
	}
	if err := api.fetchBlobs(ctx, blobs, dir); err != nil {
		return Tree{}, err
	}
	target := dir
	if root != "" {
		target = filepath.Join(dir, filepath.FromSlash(root))
		if err := os.MkdirAll(target, 0o755); err != nil {
			return Tree{}, fmt.Errorf("writing the GitHub tree: %w", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return Tree{}, fmt.Errorf("resolving the GitHub tree: %w", err)
	}
	return Tree{Dir: resolved, Commit: commit}, nil
}

// blobAPIBase is the API base the fast path calls.
func blobAPIBase() string {
	if base := strings.TrimSpace(os.Getenv(envGitHubAPIURL)); base != "" {
		if u, err := url.Parse(base); err == nil &&
			(u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return strings.TrimSuffix(base, "/")
		}
	}
	if githubAPIBase != "" {
		return strings.TrimSuffix(githubAPIBase, "/")
	}
	return DefaultGitHubAPI
}

// blobToken is the first explicit API token configured in the environment. It is never printed.
func blobToken() string {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}

// blobWorkers bounds the concurrent blob downloads, matching upstream's fetch concurrency.
const blobWorkers = 4

// fetchBlobs downloads, verifies and writes every selected blob. Any failure cancels the rest; the
// caller resets the directory before cloning, so a partial snapshot is never installed.
func (a *blobAPI) fetchBlobs(ctx context.Context, entries []blobTreeEntry, dir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, blobWorkers)
	failures := make(chan error, 1)
	var wg sync.WaitGroup
	for _, entry := range entries {
		wg.Add(1)
		go func(entry blobTreeEntry) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			content, err := a.blob(ctx, entry.SHA)
			if err == nil {
				err = verifyBlob(entry, content)
			}
			if err == nil {
				err = writeBlob(dir, entry.Path, content)
			}
			if err != nil {
				select {
				case failures <- err:
				default:
				}
				cancel()
			}
		}(entry)
	}
	wg.Wait()
	select {
	case err := <-failures:
		return err
	default:
		return ctx.Err()
	}
}

// blobAPI calls the read endpoints of the GitHub REST API with the documented authentication
// ladder: anonymous, then an explicit token, then the gh CLI.
type blobAPI struct {
	base   string
	owner  string
	name   string
	runner Runner
}

// defaultCommit is the commit at the tip of the default branch.
func (a *blobAPI) defaultCommit(ctx context.Context) (string, error) {
	data, err := a.get(ctx, "repos/"+url.PathEscape(a.owner)+"/"+url.PathEscape(a.name)+"/commits?per_page=1")
	if err != nil {
		return "", err
	}
	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(data, &commits); err != nil || len(commits) == 0 {
		return "", errors.New("the GitHub API returned no commit for the default branch")
	}
	commit := strings.ToLower(commits[0].SHA)
	if !commitRE.MatchString(commit) {
		return "", errors.New("the GitHub API returned an invalid commit")
	}
	return commit, nil
}

// tree is the recursive tree of commit.
func (a *blobAPI) tree(ctx context.Context, commit string) (blobTree, error) {
	if !commitRE.MatchString(commit) {
		return blobTree{}, errors.New("the GitHub tree needs a full commit")
	}
	data, err := a.get(ctx, "repos/"+url.PathEscape(a.owner)+"/"+url.PathEscape(a.name)+
		"/git/trees/"+commit+"?recursive=1")
	if err != nil {
		return blobTree{}, err
	}
	var doc blobTree
	if err := json.Unmarshal(data, &doc); err != nil {
		return blobTree{}, errors.New("the GitHub API returned an invalid tree")
	}
	if doc.Truncated {
		return blobTree{}, errors.New("the GitHub tree is truncated")
	}
	return doc, nil
}

// blob reads one blob and decodes its base64 content.
func (a *blobAPI) blob(ctx context.Context, sha string) ([]byte, error) {
	sha = strings.ToLower(sha)
	if !commitRE.MatchString(sha) {
		return nil, errors.New("the GitHub tree has an invalid blob SHA")
	}
	data, err := a.get(ctx, "repos/"+url.PathEscape(a.owner)+"/"+url.PathEscape(a.name)+
		"/git/blobs/"+sha)
	if err != nil {
		return nil, err
	}
	var doc struct {
		SHA      string `json:"sha"`
		Size     int64  `json:"size"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.New("the GitHub API returned an invalid blob")
	}
	if doc.Encoding != "base64" {
		return nil, errors.New("the GitHub API returned an unsupported blob encoding")
	}
	if doc.SHA != "" && strings.ToLower(doc.SHA) != sha {
		return nil, errors.New("the GitHub API returned a different blob")
	}
	content, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding,
		strings.NewReader(strings.NewReplacer("\r", "", "\n", "").Replace(doc.Content))))
	if err != nil {
		return nil, errors.New("the GitHub API returned invalid base64")
	}
	if doc.Size != 0 && int64(len(content)) != doc.Size {
		return nil, errors.New("the GitHub API returned a blob of the wrong size")
	}
	return content, nil
}

// get reads one API endpoint, trying the next authentication step only when the last one was
// rejected for authentication or rate limiting.
func (a *blobAPI) get(ctx context.Context, endpoint string) ([]byte, error) {
	data, retryable, err := a.direct(ctx, endpoint, "")
	if err == nil {
		return data, nil
	}
	if !retryable {
		return nil, err
	}
	if token := blobToken(); token != "" {
		data, retryable, err = a.direct(ctx, endpoint, token)
		if err == nil {
			return data, nil
		}
		if !retryable {
			return nil, err
		}
	}
	if _, lookErr := exec.LookPath("gh"); lookErr != nil {
		return nil, err
	}
	return a.viaGH(ctx, endpoint)
}

// direct sends one unauthenticated or bearer-authenticated request. retryable reports whether a
// credential could change the answer.
func (a *blobAPI) direct(ctx context.Context, endpoint, token string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/"+endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "skills")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := githubClient.Do(req)
	if err != nil {
		return nil, false, errors.New("the GitHub API request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		retryable := credentialRetry(resp)
		return nil, retryable, fmt.Errorf("the GitHub API answered %s", resp.Status)
	}
	data, readErr := readResponse(resp.Body, maxBlobResponse)
	if readErr != nil {
		return nil, false, readErr
	}
	return data, false, nil
}

// credentialRetry reports whether an HTTP status means another credential may help: an
// authentication failure or a rate limit. A plain permission denial is not retried.
func credentialRetry(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		return resp.Header.Get("Retry-After") != "" ||
			strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")) == "0"
	}
	return false
}

// viaGH reads an endpoint through the gh CLI, which uses its own stored credential without the
// CLI ever reading it. The output is never included in an error, so no credential can leak.
func (a *blobAPI) viaGH(ctx context.Context, endpoint string) ([]byte, error) {
	out, err := a.runner.Run(ctx, Command{
		Name: "gh",
		Args: []string{"api", endpoint, "--method", "GET", "--hostname", "github.com"},
		Env:  append(os.Environ(), "GH_PROMPT_DISABLED=1"),
	})
	if err != nil {
		return nil, errors.New("the GitHub CLI could not read the API")
	}
	data := []byte(out.Stdout)
	if int64(len(data)) > maxBlobResponse {
		return nil, errors.New("the GitHub CLI response is too large")
	}
	return data, nil
}

// readResponse reads a bounded response body.
func readResponse(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, errors.New("reading the GitHub API response failed")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the GitHub API response is too large")
	}
	return data, nil
}

// blobTree is the subset of the Trees API document the fast path needs.
type blobTree struct {
	SHA       string          `json:"sha"`
	Truncated bool            `json:"truncated"`
	Tree      []blobTreeEntry `json:"tree"`
}

// blobTreeEntry is one entry of a recursive tree.
type blobTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size *int64 `json:"size"`
}

// selectBlobs chooses the blob entries to install. root is the repository-relative search root.
// A root-level SKILL.md is the documented exception: the blob path installs only that file.
func selectBlobs(doc blobTree, root string) ([]blobTreeEntry, error) {
	found := false
	var blobs []blobTreeEntry
	for _, entry := range doc.Tree {
		if !withinRoot(entry.Path, root) {
			continue
		}
		found = true
		if err := checkBlobPath(entry.Path); err != nil {
			return nil, err
		}
		switch entry.Type {
		case "tree":
			continue
		case "blob":
		default:
			return nil, errors.New("the GitHub tree contains an entry the API cannot install")
		}
		// A symlink or a submodule does not materialize as a regular file, and the clone path
		// handles both; let it.
		if entry.Mode == "120000" || entry.Mode == "160000" {
			return nil, errors.New("the GitHub tree contains a link or submodule")
		}
		if entry.Size == nil {
			return nil, errors.New("the GitHub tree has a blob without a size")
		}
		if !commitRE.MatchString(entry.SHA) {
			return nil, errors.New("the GitHub tree has a blob without a SHA")
		}
		blobs = append(blobs, entry)
	}
	if root != "" && !found {
		return nil, errors.New("the subpath is not in the GitHub tree")
	}
	if root == "" {
		for _, entry := range blobs {
			if entry.Path == "SKILL.md" {
				return []blobTreeEntry{entry}, nil
			}
		}
	}
	return blobs, nil
}

// withinRoot reports whether path is the root itself or one of its descendants.
func withinRoot(path, root string) bool {
	if root == "" {
		return path != ""
	}
	return path == root || strings.HasPrefix(path, root+"/")
}

// checkBlobPath rejects a tree path that could write outside dir.
func checkBlobPath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, `\`) ||
		filepath.IsAbs(path) || isWindowsAbs(path) {
		return fmt.Errorf("unsafe path %q in the GitHub tree", path)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("unsafe path %q in the GitHub tree", path)
		}
	}
	return nil
}

// verifyBlob checks decoded content against its tree entry: the Git blob SHA covers content and
// length, which is what makes a snapshot complete.
func verifyBlob(entry blobTreeEntry, content []byte) error {
	if entry.Size != nil && int64(len(content)) != *entry.Size {
		return errors.New("a GitHub blob has the wrong size")
	}
	if gitBlobSHA(content) != strings.ToLower(entry.SHA) {
		return errors.New("a GitHub blob has the wrong SHA")
	}
	return nil
}

// gitBlobSHA is the Git object id of a blob: sha1("blob <len>\0" + content).
func gitBlobSHA(content []byte) string {
	h := sha1.New()
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// writeBlob writes one verified file below dir at its repository-relative path.
func writeBlob(dir, path string, content []byte) error {
	target := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("writing the GitHub tree: %w", err)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return fmt.Errorf("writing the GitHub tree: %w", err)
	}
	return nil
}
