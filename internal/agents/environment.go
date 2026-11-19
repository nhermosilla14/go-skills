package agents

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Environment is everything path resolution and detection read from the machine. Tests inject a
// fake one, so Windows and macOS behavior can be exercised on any host.
//
// Home is resolved by the caller: FromOS uses os.UserHomeDir, which honours HOME on POSIX and
// USERPROFILE on Windows.
type Environment struct {
	GOOS string            // target platform: linux, darwin or windows
	Home string            // user home directory
	Cwd  string            // project directory
	Vars map[string]string // environment variables read by the registry and by the CI checks

	// ApplicationsDir and EtcDir are the roots of the fixed probes outside home, so tests can
	// exercise them without touching the real system. Empty means the platform default.
	ApplicationsDir string
	EtcDir          string
}

// FromOS reads the current process environment. Vars holds only the variables the registry and the
// CI checks read.
func FromOS(cwd string) Environment {
	names := []string{
		"XDG_CONFIG_HOME", "XDG_STATE_HOME", "APPDATA", "FLATPAK_XDG_CONFIG_HOME", "HOME", "USERPROFILE",
		"CODEX_HOME", "CLAUDE_CONFIG_DIR", "VIBE_HOME", "HERMES_HOME", "AUTOHAND_HOME",
		"GROK_HOME", "SARVAM_HOME", "REASONIX_HOME",
		"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "TRAVIS", "TF_BUILD", "JENKINS_URL",
	}
	names = append(names, HostVariables()...)
	vars := map[string]string{}
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			vars[name] = v
		}
	}
	home, _ := os.UserHomeDir()
	return Environment{GOOS: runtime.GOOS, Home: home, Cwd: cwd, Vars: vars}
}

// getenv returns a trimmed variable, treating blank values as unset.
func (e Environment) getenv(name string) string {
	return strings.TrimSpace(e.Vars[name])
}

// ConfigHome is the XDG config home used by agents that honour it: $XDG_CONFIG_HOME when set,
// else ~/.config on every platform.
func (e Environment) ConfigHome() string {
	if v := e.getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(e.Home, ".config")
}

func (e Environment) applicationsDir() string {
	if e.ApplicationsDir != "" {
		return e.ApplicationsDir
	}
	return "/Applications"
}

func (e Environment) etcDir() string {
	if e.EtcDir != "" {
		return e.EtcDir
	}
	return "/etc"
}

// CanonicalSkillsDir is the directory shared by universal agents: <cwd>/.agents/skills for a
// project and <home>/.agents/skills for the global scope.
func CanonicalSkillsDir(env Environment, global bool) string {
	base := env.Cwd
	if global {
		base = env.Home
	}
	return filepath.Join(base, ".agents", "skills")
}

// GlobalLockPath is the global lock file: $XDG_STATE_HOME/skills/.skill-lock.json when the variable
// is set, else <home>/.agents/.skill-lock.json. Home resolution is the caller's.
func GlobalLockPath(env Environment) string {
	if v := env.getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "skills", ".skill-lock.json")
	}
	return filepath.Join(env.Home, ".agents", ".skill-lock.json")
}

// pathExists is the only filesystem probe used by detection: existence, errors count as absent.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// join joins a base with a slash-separated relative path from the registry.
func join(base, rel string) string {
	if rel == "" {
		return base
	}
	return filepath.Join(base, filepath.FromSlash(rel))
}

// homeDir builds a global skills directory under home.
func homeDir(rel string) func(Environment) string {
	return func(env Environment) string { return join(env.Home, rel) }
}

// configDir builds a global skills directory under the XDG config home.
func configDir(rel string) func(Environment) string {
	return func(env Environment) string { return join(env.ConfigHome(), rel) }
}

// envDir builds a global skills directory as $NAME/skills, falling back to ~/fallback/skills when
// the variable is unset.
func envDir(name, fallback string) func(Environment) string {
	return func(env Environment) string {
		if v := env.getenv(name); v != "" {
			return join(v, "skills")
		}
		return join(env.Home, fallback+"/skills")
	}
}

// homeExists detects an agent by a directory under home.
func homeExists(rel string) func(Environment) bool {
	return func(env Environment) bool { return pathExists(join(env.Home, rel)) }
}

// configExists detects an agent by a directory under the XDG config home.
func configExists(rel string) func(Environment) bool {
	return func(env Environment) bool { return pathExists(join(env.ConfigHome(), rel)) }
}

// projectExists detects an agent by a directory in the project.
func projectExists(rel string) func(Environment) bool {
	return func(env Environment) bool { return pathExists(join(env.Cwd, rel)) }
}

// projectOrHomeExists detects an agent by a directory in the project, or the same name under home.
func projectOrHomeExists(rel string) func(Environment) bool {
	return func(env Environment) bool {
		return pathExists(join(env.Cwd, rel)) || pathExists(join(env.Home, rel))
	}
}
