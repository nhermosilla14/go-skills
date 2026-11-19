package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RemoteHead returns the commit the source's ref points at, or HEAD when no ref is recorded, by
// asking the remote with git ls-remote. It never clones the repository.
func RemoteHead(ctx context.Context, src Source, runner Runner) (string, error) {
	if src.Kind == KindLocal {
		return "", errors.New("a local source has no remote head")
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	args := append(append([]string{}, gitConfig...), "ls-remote", "--", src.CloneURL(), ref)
	out, err := runner.Run(ctx, Command{Name: "git", Args: args, Env: gitEnv()})
	if err != nil {
		if text := detail(out); text != "" {
			return "", fmt.Errorf("git ls-remote failed: %s", text)
		}
		return "", fmt.Errorf("git ls-remote failed: %w", err)
	}
	commit := pickRemoteCommit(out.Stdout, src.Ref)
	if commit == "" {
		return "", fmt.Errorf("the remote has no ref matching %s", ref)
	}
	return commit, nil
}

// pickRemoteCommit chooses the commit for ref from git ls-remote output. A tag is preferred in its
// peeled form, because the lock records the commit rather than the tag object. Without a ref, HEAD
// is used, falling back to the first line.
func pickRemoteCommit(stdout, ref string) string {
	shas := map[string]string{}
	first := ""
	for _, line := range strings.Split(stdout, "\n") {
		sha, name, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		sha = strings.ToLower(strings.TrimSpace(sha))
		if !commitRE.MatchString(sha) {
			continue
		}
		if first == "" {
			first = sha
		}
		name = strings.TrimSpace(name)
		if _, seen := shas[name]; !seen {
			shas[name] = sha
		}
	}
	if ref == "" {
		if sha := shas["HEAD"]; sha != "" {
			return sha
		}
		return first
	}
	for _, name := range []string{
		"refs/heads/" + ref,
		"refs/tags/" + ref + "^{}",
		"refs/tags/" + ref,
		ref,
	} {
		if sha := shas[name]; sha != "" {
			return sha
		}
	}
	return first
}
