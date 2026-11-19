package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseUseOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want UseOptions
	}{
		{
			name: "source with selector",
			args: []string{"use", "owner/repo@alpha"},
			want: UseOptions{Source: "owner/repo@alpha"},
		},
		{
			name: "skill and agent",
			args: []string{"use", "./src", "--skill", "alpha", "--agent", "codex"},
			want: UseOptions{Source: "./src", Skills: []string{"alpha"}, Agents: []string{"codex"}},
		},
		{
			name: "short flags",
			args: []string{"use", "./src", "-s", "alpha", "-a", "claude-code"},
			want: UseOptions{Source: "./src", Skills: []string{"alpha"}, Agents: []string{"claude-code"}},
		},
		{
			name: "repeated skills accumulate",
			args: []string{"use", "./src", "-s", "alpha", "-s", "beta"},
			want: UseOptions{Source: "./src", Skills: []string{"alpha", "beta"}},
		},
		{
			name: "equals form",
			args: []string{"use", "./src", "--skill=alpha", "--agent=codex"},
			want: UseOptions{Source: "./src", Skills: []string{"alpha"}, Agents: []string{"codex"}},
		},
		{
			name: "full depth",
			args: []string{"use", "./src", "--full-depth"},
			want: UseOptions{Source: "./src", FullDepth: true},
		},
		{
			name: "double dash keeps an option-like source",
			args: []string{"use", "--", "-weird"},
			want: UseOptions{Source: "-weird"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindUse {
				t.Fatalf("Kind = %v, want KindUse", cmd.Kind)
			}
			if !reflect.DeepEqual(cmd.Use, tc.want) {
				t.Errorf("Use = %+v, want %+v", cmd.Use, tc.want)
			}
		})
	}
}

func TestParseUseHelp(t *testing.T) {
	for _, args := range [][]string{{"use", "--help"}, {"use", "-h"}, {"use", "help"}} {
		cmd, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse %v: %v", args, err)
		}
		if cmd.Kind != KindUse || !cmd.UseHelp {
			t.Errorf("Parse %v = %v, %+v; want the use help", args, cmd.Kind, cmd.Use)
		}
	}
}

func TestParseUseUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown long", []string{"use", "./src", "--json"}, "unknown option '--json'"},
		{"unknown short", []string{"use", "./src", "-q"}, "unknown option '-q'"},
		{"skill without value", []string{"use", "./src", "--skill"}, "--skill requires a skill name"},
		{"agent without value", []string{"use", "./src", "-a"}, "--agent requires an agent name"},
		{"boolean with value", []string{"use", "./src", "--full-depth=1"}, "--full-depth does not take a value"},
		{"extra source", []string{"use", "one", "two"}, "only one source is allowed"},
		{"value not last in group", []string{"use", "./src", "-ah"}, "-a takes a value and must be last"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.args)
			var usage *UsageError
			if !errors.As(err, &usage) {
				t.Fatalf("err = %v, want UsageError", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateUse(t *testing.T) {
	tests := []struct {
		name string
		opts UseOptions
		want []string
	}{
		{
			name: "valid",
			opts: UseOptions{Source: "owner/repo", Skills: []string{"alpha"}, Agents: []string{"codex"}},
		},
		{
			name: "missing source",
			opts: UseOptions{},
			want: []string{"missing required argument: source"},
		},
		{
			name: "two skills",
			opts: UseOptions{Source: "owner/repo", Skills: []string{"alpha", "beta"}},
			want: []string{"Only one --skill value can be provided"},
		},
		{
			name: "wildcard agent",
			opts: UseOptions{Source: "owner/repo", Agents: []string{"*"}},
			want: []string{"skills use --agent does not support '*'; specify exactly one agent."},
		},
		{
			name: "two agents",
			opts: UseOptions{Source: "owner/repo", Agents: []string{"codex", "claude-code"}},
			want: []string{"skills use --agent accepts exactly one agent."},
		},
		{
			name: "unknown agent",
			opts: UseOptions{Source: "owner/repo", Agents: []string{"opencode"}},
			want: []string{"must be one of claude-code, codex, sarvam-code"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateUse(tc.opts)
			if len(got) != len(tc.want) {
				t.Fatalf("ValidateUse = %v, want %v", got, tc.want)
			}
			for i := range got {
				if !strings.Contains(got[i], tc.want[i]) {
					t.Errorf("message %d = %q, want containing %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestUseHelpNamesTheCommand(t *testing.T) {
	help := UseHelp()
	for _, want := range []string{
		"Usage: skills use", "--skill", "--agent", "claude-code", "codex", "sarvam-code",
		"--full-depth", "temporary directory",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("use help misses %q", want)
		}
	}
	if strings.Contains(help, "Add options:") {
		t.Error("use help leaked the top-level help")
	}
}

func TestUseAgentMapping(t *testing.T) {
	tests := map[string]string{
		"claude-code": "claude",
		"codex":       "codex",
		"sarvam-code": "sarvam-code",
	}
	if got := UseAgentIDs(); !reflect.DeepEqual(got, []string{"claude-code", "codex", "sarvam-code"}) {
		t.Errorf("UseAgentIDs = %v", got)
	}
	for id, command := range tests {
		t.Run(id, func(t *testing.T) {
			agent, ok := ResolveUseAgent(id)
			if !ok {
				t.Fatalf("ResolveUseAgent(%q) not found", id)
			}
			if agent.Command != command {
				t.Errorf("command = %q, want %q", agent.Command, command)
			}
		})
	}
	if _, ok := ResolveUseAgent("opencode"); ok {
		t.Error("opencode resolved although use only launches three agents")
	}
}

func TestLaunchAgentPassesPromptAndStreams(t *testing.T) {
	agent := fakeAgent(t, `printf '%s' "$1" > "$ARGS_FILE"
read line
printf 'out:%s\n' "$line"
printf 'err\n' >&2
exit 7
`)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("ARGS_FILE", argsFile)
	var stdout, stderr bytes.Buffer
	code, err := LaunchAgent(agent, "the whole prompt", strings.NewReader("hello\n"), &stdout, &stderr)
	if err != nil {
		t.Fatalf("LaunchAgent: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want the child's 7", code)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the agent did not receive the prompt: %v", err)
	}
	if got := string(raw); got != "the whole prompt" {
		t.Errorf("prompt argument = %q, want one argv element", got)
	}
	if stdout.String() != "out:hello\n" {
		t.Errorf("stdout = %q, want the child's inherited stream", stdout.String())
	}
	if stderr.String() != "err\n" {
		t.Errorf("stderr = %q, want the child's inherited stream", stderr.String())
	}
}

func TestLaunchAgentMissingCommand(t *testing.T) {
	agent := UseAgent{ID: "ghost", DisplayName: "Ghost Agent", Command: "skills-use-no-such-command"}
	code, err := LaunchAgent(agent, "prompt", nil, io.Discard, io.Discard)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	var launch *LaunchError
	if !errors.As(err, &launch) {
		t.Fatalf("err = %v, want LaunchError", err)
	}
	if want := "Could not launch Ghost Agent: command not found: skills-use-no-such-command"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestLaunchAgentSignalledChildCountsAsOne(t *testing.T) {
	agent := fakeAgent(t, "kill -TERM $$\n")
	code, err := LaunchAgent(agent, "prompt", nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("LaunchAgent: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1 when the child has no exit code", code)
	}
}

// fakeAgent writes a POSIX shell script and returns an agent that runs it.
func fakeAgent(t *testing.T, body string) UseAgent {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), "fake-agent")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return UseAgent{ID: "fake", DisplayName: "Fake Agent", Command: path}
}
