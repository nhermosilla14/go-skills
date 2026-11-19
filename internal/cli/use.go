package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// UseAgent is one of the agents `use --agent` knows how to launch.
type UseAgent struct {
	ID          string
	DisplayName string
	Command     string
}

// useAgents is the fixed, ordered list of launchable agents.
var useAgents = []UseAgent{
	{ID: "claude-code", DisplayName: "Claude Code", Command: "claude"},
	{ID: "codex", DisplayName: "Codex", Command: "codex"},
	{ID: "sarvam-code", DisplayName: "Sarvam Code", Command: "sarvam-code"},
}

// ResolveUseAgent looks up a launchable agent by its identifier.
func ResolveUseAgent(id string) (UseAgent, bool) {
	for _, a := range useAgents {
		if a.ID == id {
			return a, true
		}
	}
	return UseAgent{}, false
}

// UseAgentIDs returns the accepted --agent identifiers in documented order.
func UseAgentIDs() []string {
	ids := make([]string, 0, len(useAgents))
	for _, a := range useAgents {
		ids = append(ids, a.ID)
	}
	return ids
}

// ValidateUse reports the use-specific problems of a parsed command line, one message per
// problem. A missing source is left to the caller, which also prints the use help.
func ValidateUse(opts UseOptions) []string {
	var errs []string
	if strings.TrimSpace(opts.Source) == "" {
		errs = append(errs, "missing required argument: source")
	}
	if len(opts.Skills) > 1 {
		errs = append(errs, "Only one --skill value can be provided")
	}
	if len(opts.Agents) > 1 {
		errs = append(errs, "skills use --agent accepts exactly one agent.")
	} else if len(opts.Agents) == 1 {
		id := opts.Agents[0]
		switch id {
		case "*":
			errs = append(errs, "skills use --agent does not support '*'; specify exactly one agent.")
		default:
			if _, ok := ResolveUseAgent(id); !ok {
				errs = append(errs, fmt.Sprintf("skills use --agent must be one of %s.",
					strings.Join(UseAgentIDs(), ", ")))
			}
		}
	}
	return errs
}

// LaunchError reports that an agent's executable is missing. It reads as the documented
// diagnostic and still matches exec.ErrNotFound.
type LaunchError struct {
	Agent UseAgent
}

func (e *LaunchError) Error() string {
	return fmt.Sprintf("Could not launch %s: command not found: %s", e.Agent.DisplayName, e.Agent.Command)
}

func (e *LaunchError) Unwrap() error { return exec.ErrNotFound }

// LaunchAgent runs the agent with the prompt as one argument, with the given streams inherited and
// no shell. It returns the child's exit code; a missing code counts as 1.
func LaunchAgent(agent UseAgent, prompt string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.Command(agent.Command, prompt)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code <= 0 {
			code = 1
		}
		return code, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return 1, &LaunchError{Agent: agent}
	}
	return 1, err
}

// UseHelp returns the help of the use command.
func UseHelp() string {
	return `Usage: skills use <source>[@skill] [options]

Resolve a skill like add, copy it into a temporary directory and print the
prompt an agent needs to execute it. Nothing is installed and no lock file is
written. With --agent the prompt is passed to the agent instead of printed.

Use options:
  -s, --skill <name>   Select the skill to use; only one value is allowed
  -a, --agent <agent>  Launch the agent with the prompt: claude-code, codex or
                       sarvam-code
      --full-depth     Search all subdirectories, even under a root SKILL.md
  -h, --help           Show this help

The temporary directory is kept after the command exits because the prompt
points the agent at its supporting files.
`
}
