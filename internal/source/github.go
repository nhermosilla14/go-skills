package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultGitHubAPI is the GitHub REST base URL used when a caller passes none.
const DefaultGitHubAPI = "https://api.github.com"

// maxTreeResponse bounds the tree document a hostile or huge repository can make us read.
const maxTreeResponse = 32 << 20

// githubClient is shared by every Trees API call; the timeout keeps a stuck request from hanging a
// command.
var githubClient = &http.Client{Timeout: 30 * time.Second}

// treeResponse is the subset of the Trees API document the hash needs.
type treeResponse struct {
	SHA       string `json:"sha"`
	Truncated bool   `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

// GitHubTreeHash returns the 40-hex Git tree SHA of folder at commit in owner/repo, as recorded in
// the global lock's skillFolderHash. An empty folder names the repository root. The optional
// token, when non-empty, is sent as a bearer credential and is never part of an error. apiBase
// overrides the API host, which tests use with a fake server.
func GitHubTreeHash(ctx context.Context, repo, commit, folder, apiBase, token string) (string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("invalid GitHub repository %q: expected owner/repo", repo)
	}
	commit = strings.ToLower(strings.TrimSpace(commit))
	if !commitRE.MatchString(commit) {
		return "", fmt.Errorf("invalid commit %q: expected a 40-character SHA", commit)
	}
	folder = strings.Trim(strings.ReplaceAll(folder, `\`, "/"), "/")
	if folder == "." {
		folder = ""
	}
	if err := checkTreeFolder(folder); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(apiBase, "/")
	if base == "" {
		base = DefaultGitHubAPI
	}
	endpoint := base + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) +
		"/git/trees/" + commit + "?recursive=1"
	doc, err := fetchTree(ctx, endpoint, token)
	if err != nil {
		return "", err
	}
	if doc.Truncated {
		return "", fmt.Errorf("the GitHub tree for %s at %s is truncated", repo, commit)
	}
	if folder == "" {
		if !commitRE.MatchString(strings.ToLower(doc.SHA)) {
			return "", fmt.Errorf("the GitHub tree for %s at %s has no root SHA", repo, commit)
		}
		return strings.ToLower(doc.SHA), nil
	}
	for _, entry := range doc.Tree {
		if entry.Path != folder {
			continue
		}
		if entry.Type != "tree" {
			return "", fmt.Errorf("%q is a %s, not a folder, in %s at %s", folder, entry.Type, repo, commit)
		}
		if !commitRE.MatchString(strings.ToLower(entry.SHA)) {
			return "", fmt.Errorf("the GitHub tree for %q in %s has no SHA", folder, repo)
		}
		return strings.ToLower(entry.SHA), nil
	}
	return "", fmt.Errorf("folder %q was not found in %s at %s", folder, repo, commit)
}

// checkTreeFolder rejects folder values that could redirect the lookup.
func checkTreeFolder(folder string) error {
	for _, segment := range strings.Split(folder, "/") {
		if segment == ".." {
			return fmt.Errorf("invalid folder %q: must not contain %q", folder, "..")
		}
	}
	return nil
}

// fetchTree reads and decodes one Trees API document.
func fetchTree(ctx context.Context, endpoint, token string) (treeResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return treeResponse{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "skills")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := githubClient.Do(req)
	if err != nil {
		return treeResponse{}, fmt.Errorf("the GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return treeResponse{}, fmt.Errorf("the GitHub API answered %s", resp.Status)
	}
	var doc treeResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxTreeResponse))
	if err := decoder.Decode(&doc); err != nil {
		return treeResponse{}, fmt.Errorf("invalid GitHub API response: %w", err)
	}
	return doc, nil
}
