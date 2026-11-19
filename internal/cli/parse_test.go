package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
)

func TestParseCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Kind
	}{
		{"no args shows help", nil, KindHelp},
		{"help", []string{"help"}, KindHelp},
		{"--help", []string{"--help"}, KindHelp},
		{"-h", []string{"-h"}, KindHelp},
		{"add --help", []string{"add", "--help"}, KindHelp},
		{"version", []string{"version"}, KindVersion},
		{"--version", []string{"--version"}, KindVersion},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != tc.want {
				t.Errorf("Kind = %v, want %v", cmd.Kind, tc.want)
			}
		})
	}
}

func TestParseAddOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want app.AddOptions
	}{
		{
			name: "source and yes",
			args: []string{"add", "./skills", "-y"},
			want: app.AddOptions{Source: "./skills", Yes: true},
		},
		{
			name: "aliases",
			args: []string{"i", "./x", "-y"},
			want: app.AddOptions{Source: "./x", Yes: true},
		},
		{
			name: "greedy agent list stops at option",
			args: []string{"a", "./x", "-a", "claude-code", "opencode", "-y"},
			want: app.AddOptions{Source: "./x", Agents: []string{"claude-code", "opencode"}, Yes: true},
		},
		{
			name: "equals form takes one value",
			args: []string{"add", "--agent=claude-code", "./x"},
			want: app.AddOptions{Source: "./x", Agents: []string{"claude-code"}},
		},
		{
			name: "repeatable skill accumulates",
			args: []string{"add", "./x", "-s", "a", "-s", "b", "--skill", "c"},
			want: app.AddOptions{Source: "./x", Skills: []string{"a", "b", "c"}},
		},
		{
			name: "grouped booleans",
			args: []string{"add", "./x", "-yl"},
			want: app.AddOptions{Source: "./x", Yes: true, List: true},
		},
		{
			name: "copy and list",
			args: []string{"add", "./x", "--copy", "-l"},
			want: app.AddOptions{Source: "./x", Copy: true, List: true},
		},
		{
			name: "double dash ends options",
			args: []string{"add", "--", "-weird-dir"},
			want: app.AddOptions{Source: "-weird-dir"},
		},
		{
			name: "wildcard passes through",
			args: []string{"add", "./x", "-a", "*", "-s", "*"},
			want: app.AddOptions{Source: "./x", Agents: []string{"*"}, Skills: []string{"*"}},
		},
		{
			name: "full depth",
			args: []string{"add", "./x", "--full-depth"},
			want: app.AddOptions{Source: "./x", FullDepth: true},
		},
		{
			name: "all overrides explicit selections",
			args: []string{"add", "./x", "--all", "-s", "one", "-a", "opencode"},
			want: app.AddOptions{Source: "./x", Agents: []string{"*"}, Skills: []string{"*"}, Yes: true},
		},
		{
			name: "json flag",
			args: []string{"add", "./x", "-y", "--json"},
			want: app.AddOptions{Source: "./x", Yes: true, JSON: true},
		},
		{
			name: "subagent list is parsed",
			args: []string{"add", "./x", "--subagent", "root", "helper", "-y"},
			want: app.AddOptions{Source: "./x", Subagents: []string{"root", "helper"}, Yes: true},
		},
		{
			name: "rename last occurrence wins",
			args: []string{"add", "./x", "--rename", "first", "--rename=second"},
			want: app.AddOptions{Source: "./x", Rename: "second"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindAdd {
				t.Fatalf("Kind = %v, want KindAdd", cmd.Kind)
			}
			if !reflect.DeepEqual(cmd.Add, tc.want) {
				t.Errorf("Add = %+v, want %+v", cmd.Add, tc.want)
			}
		})
	}
}

func TestParseUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"list"}, "unknown command 'list'"},
		{"unknown long flag", []string{"add", "./x", "--force"}, "unknown option '--force'"},
		{"unknown short flag", []string{"add", "./x", "-q"}, "unknown option '-q'"},
		{"extra source", []string{"add", "./a", "./b"}, "unexpected argument './b'"},
		{"missing source", []string{"add", "-y"}, "missing required argument: source"},
		{"install without source", []string{"install"}, "restoring from skills-lock.json is not supported yet"},
		{"global unsupported", []string{"add", "./x", "-g"}, "global scope is not supported yet"},
		{"list flag without value", []string{"add", "./x", "--agent", "-y"}, "--agent requires an agent name"},
		{"skill equals empty", []string{"add", "./x", "--skill="}, "--skill requires a skill name"},
		{"boolean with value", []string{"add", "./x", "--copy=1"}, "--copy does not take a value"},
		{"short list not last", []string{"add", "./x", "-as", "x"}, "-a takes a value and must be last"},
		{"rename without value", []string{"add", "./x", "--rename"}, "--rename requires a skill name"},
		{"rename with empty value", []string{"add", "./x", "--rename="}, "--rename requires a skill name"},
		{"full depth with value", []string{"add", "./x", "--full-depth=1"}, "--full-depth does not take a value"},
		{"json with value", []string{"add", "./x", "--json=1"}, "--json does not take a value"},
		{"subagent without value", []string{"add", "./x", "--subagent", "-y"}, "--subagent requires a subagent name"},
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

func TestHelpListsAgentsAndOnlyWorkingFlags(t *testing.T) {
	help := Help()
	for _, want := range []string{
		"claude-code", "opencode", "--copy", "--list", "--json", "--subagent",
		"owner/repo@v1.2.3", "git@github.com:owner/repo.git", "git on PATH",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help misses %q", want)
		}
	}
	if strings.Contains(help, "--global") {
		t.Error("help advertises unsupported --global")
	}
}
