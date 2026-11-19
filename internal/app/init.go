package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// InitOptions are the inputs of the init use case. An empty Name writes the template to the working
// directory itself, named after it.
type InitOptions struct {
	Name string
}

// InitReport is the outcome of an init run.
type InitReport struct {
	Name        string // skill name in the template
	Path        string // absolute path of the SKILL.md file
	DisplayPath string // slash-separated path of SKILL.md relative to the working directory
	Exists      bool   // SKILL.md was already there; nothing was written
}

// Init writes the documented SKILL.md template to ./SKILL.md, or to ./<name>/SKILL.md when a name
// is given, creating the directory and its parents. An existing file is never overwritten. Names
// that would leave the working directory are usage errors, so a typo cannot write elsewhere.
func Init(opts InitOptions, env Env) (InitReport, error) {
	workDir, err := filepath.Abs(env.WorkDir)
	if err != nil {
		return InitReport{}, err
	}
	dir, name := workDir, filepath.Base(workDir)
	if opts.Name != "" {
		if name, err = initName(opts.Name); err != nil {
			return InitReport{}, err
		}
		dir = filepath.Join(workDir, name)
	}
	if !fsutil.Within(workDir, dir) {
		return InitReport{}, fmt.Errorf("%w: invalid skill name %q: it must stay inside the working directory",
			ErrUsage, opts.Name)
	}
	path := filepath.Join(dir, "SKILL.md")
	rel, err := filepath.Rel(workDir, path)
	if err != nil {
		return InitReport{}, err
	}
	report := InitReport{Name: name, Path: path, DisplayPath: filepath.ToSlash(rel)}

	if err := fsutil.EnsureDir(dir); err != nil {
		return InitReport{}, fmt.Errorf("creating the skill directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		report.Exists = true
		return report, nil
	}
	if err != nil {
		return InitReport{}, fmt.Errorf("creating %s: %w", report.DisplayPath, err)
	}
	if _, err := file.Write(initTemplate(name)); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return InitReport{}, fmt.Errorf("writing %s: %w", report.DisplayPath, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return InitReport{}, fmt.Errorf("writing %s: %w", report.DisplayPath, err)
	}
	return report, nil
}

// initName validates the positional name of init and returns the directory name it selects.
// Trailing separators are normalized away; anything else that names a path outside the working
// directory (separators, absolute paths, drive letters, the home shortcut) is a usage error.
func initName(raw string) (string, error) {
	name := strings.TrimRight(raw, `/\`)
	switch {
	case name == "":
		return "", fmt.Errorf("%w: skill name must not be empty", ErrUsage)
	case name == "." || name == "..":
		return "", fmt.Errorf("%w: invalid skill name %q: it must stay inside the working directory",
			ErrUsage, name)
	case filepath.IsAbs(name) || isDriveName(name):
		return "", fmt.Errorf("%w: invalid skill name %q: absolute paths are not allowed",
			ErrUsage, name)
	case strings.ContainsAny(name, `/\`):
		return "", fmt.Errorf("%w: invalid skill name %q: separators are not allowed",
			ErrUsage, name)
	case strings.HasPrefix(name, "~"):
		return "", fmt.Errorf("%w: invalid skill name %q: the home directory shortcut is not allowed",
			ErrUsage, name)
	}
	return name, nil
}

// isDriveName reports a Windows drive prefix such as C: or D:path, whatever the host OS is.
func isDriveName(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	first := name[0]
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

// initTemplate is the documented SKILL.md skeleton. The name is YAML-escaped in the frontmatter,
// so values like `true` or `demo: x` round-trip as strings, and printed as written in the heading.
func initTemplate(name string) []byte {
	return []byte("---\nname: " + skill.YAMLScalar(name) + "\n" +
		"description: A brief description of what this skill does\n---\n\n" +
		"# " + name + "\n\n" +
		"Instructions for the agent to follow when this skill is activated.\n\n" +
		"## When to use\n\nDescribe when this skill should be used.\n\n" +
		"## Instructions\n\n1. First step\n2. Second step\n3. Additional steps as needed\n")
}
