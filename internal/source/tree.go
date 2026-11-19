package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// TreeSHA returns the 40-hex Git tree SHA of folder at rev in the repository checked out at dir.
// An empty folder names the root tree. This is the value the global lock records for GitHub
// sources, computed from the clone instead of the Trees API.
func TreeSHA(ctx context.Context, dir, rev, folder string, runner Runner) (string, error) {
	spec := strings.TrimSpace(rev)
	if !commitRE.MatchString(spec) {
		return "", errors.New("a tree SHA needs a full 40-character commit")
	}
	folder = strings.Trim(strings.ReplaceAll(folder, `\`, "/"), "/")
	if folder == "" {
		spec += "^{tree}"
	} else {
		if err := checkTreeFolder(folder); err != nil {
			return "", err
		}
		spec += ":" + folder
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	args := append(append([]string{}, gitConfig...), "rev-parse", "--verify", spec)
	out, err := runner.Run(ctx, Command{Name: "git", Args: args, Dir: dir, Env: gitEnv()})
	if err != nil {
		if text := detail(out); text != "" {
			return "", fmt.Errorf("git rev-parse failed: %s", text)
		}
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}
	sha := strings.ToLower(strings.TrimSpace(out.Stdout))
	if !commitRE.MatchString(sha) {
		return "", fmt.Errorf("could not resolve a tree SHA for %q", folder)
	}
	return sha, nil
}
