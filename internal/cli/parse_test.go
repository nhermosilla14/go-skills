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

func TestParseRestore(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want app.RestoreOptions
	}{
		{"install without source", []string{"install"}, app.RestoreOptions{}},
		{"i without source", []string{"i"}, app.RestoreOptions{}},
		{"install options", []string{"install", "-a", "claude-code", "opencode", "--force", "--dry-run"},
			app.RestoreOptions{Agents: []string{"claude-code", "opencode"}, Force: true, DryRun: true}},
		{"experimental install", []string{"experimental_install", "--agent=*", "--force"},
			app.RestoreOptions{Agents: []string{"*"}, Force: true}},
		{"experimental install short agent", []string{"experimental_install", "-a", "opencode"},
			app.RestoreOptions{Agents: []string{"opencode"}}},
		{"install frozen", []string{"install", "--frozen-lockfile"},
			app.RestoreOptions{Frozen: app.FrozenEnabled}},
		{"experimental install no frozen", []string{"experimental_install", "--no-frozen-lockfile"},
			app.RestoreOptions{Frozen: app.FrozenDisabled}},
		{"frozen wins over no frozen",
			[]string{"install", "--no-frozen-lockfile", "--frozen-lockfile"},
			app.RestoreOptions{Frozen: app.FrozenEnabled}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindRestore {
				t.Fatalf("Kind = %v, want KindRestore", cmd.Kind)
			}
			if !reflect.DeepEqual(cmd.Restore, tc.want) {
				t.Errorf("Restore = %+v, want %+v", cmd.Restore, tc.want)
			}
		})
	}
	for _, args := range [][]string{{"experimental_install", "-h"}, {"install", "--help"}} {
		cmd, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse %v: %v", args, err)
		}
		if cmd.Kind != KindHelp {
			t.Errorf("Parse %v = %v, want help", args, cmd.Kind)
		}
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
		{
			name: "global flag",
			args: []string{"add", "./x", "-g", "-y"},
			want: app.AddOptions{Source: "./x", Global: true, Yes: true},
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
		{"unknown command", []string{"frobnicate"}, "unknown command 'frobnicate'"},
		{"experimental sync", []string{"experimental_sync"}, "not supported"},
		{"sync alias", []string{"sync"}, "npm/node_modules"},
		{"unknown long flag", []string{"add", "./x", "--force"}, "unknown option '--force'"},
		{"unknown short flag", []string{"add", "./x", "-q"}, "unknown option '-q'"},
		{"extra source", []string{"add", "./a", "./b"}, "unexpected argument './b'"},
		{"missing source", []string{"add", "-y"}, "missing required argument: source"},
		{"install with add-only option", []string{"install", "--skill", "a"}, "--skill does not apply when restoring"},
		{"install global", []string{"install", "-g"}, "--global cannot be used when restoring"},
		{"experimental install global", []string{"experimental_install", "-g"}, "-g cannot be used when restoring"},
		{"experimental install source", []string{"experimental_install", "./x"}, "unexpected argument './x'"},
		{"install force with source", []string{"install", "./x", "--force"}, "--force only applies when restoring"},
		{"install dry-run with source", []string{"install", "./x", "--dry-run"}, "--dry-run only applies when restoring"},
		{"install frozen with source", []string{"install", "./x", "--frozen-lockfile"},
			"--frozen-lockfile only applies when restoring"},
		{"install no-frozen with source", []string{"install", "./x", "--no-frozen-lockfile"},
			"--no-frozen-lockfile only applies when restoring"},
		{"add frozen", []string{"add", "./x", "--frozen-lockfile"}, "unknown option '--frozen-lockfile'"},
		{"experimental install frozen with value", []string{"experimental_install", "--frozen-lockfile=1"},
			"--frozen-lockfile does not take a value"},
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

func TestParseListAndCheck(t *testing.T) {
	cmd, err := Parse([]string{"list", "-a", "claude-code", "opencode", "--json"})
	if err != nil {
		t.Fatalf("Parse list: %v", err)
	}
	if cmd.Kind != KindList {
		t.Fatalf("Kind = %v, want KindList", cmd.Kind)
	}
	if !reflect.DeepEqual(cmd.List.Agents, []string{"claude-code", "opencode"}) || !cmd.JSON {
		t.Errorf("list = %+v, json = %v", cmd.List, cmd.JSON)
	}

	cmd, err = Parse([]string{"ls", "--agent=*", "-a", "codex"})
	if err != nil {
		t.Fatalf("Parse ls: %v", err)
	}
	if cmd.Kind != KindList || !reflect.DeepEqual(cmd.List.Agents, []string{"*", "codex"}) {
		t.Errorf("ls = %+v", cmd.List)
	}

	cmd, err = Parse([]string{"list", "-g"})
	if err != nil || cmd.Kind != KindList || !cmd.List.Global {
		t.Errorf("list -g = %+v, %v", cmd.List, err)
	}

	cmd, err = Parse([]string{"check", "alpha", "--global"})
	if err != nil || cmd.Kind != KindCheck || !cmd.Check.Global {
		t.Errorf("check --global = %+v, %v", cmd.Check, err)
	}

	cmd, err = Parse([]string{"check", "alpha", "--", "-beta"})
	if err != nil {
		t.Fatalf("Parse check: %v", err)
	}
	if cmd.Kind != KindCheck || !reflect.DeepEqual(cmd.Check.Skills, []string{"alpha", "-beta"}) {
		t.Errorf("check = %+v", cmd.Check)
	}

	for _, args := range [][]string{{"list", "-h"}, {"check", "--help"}} {
		cmd, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse %v: %v", args, err)
		}
		if cmd.Kind != KindHelp {
			t.Errorf("Parse %v = %v, want help", args, cmd.Kind)
		}
	}
}

func TestParseListAndCheckUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"list positional", []string{"list", "extra"}, "unexpected argument 'extra'"},
		{"list unknown", []string{"list", "--force"}, "unknown option '--force'"},
		{"list json value", []string{"list", "--json=1"}, "--json does not take a value"},
		{"list agent without value", []string{"list", "-a"}, "--agent requires an agent name"},
		{"check unknown", []string{"check", "--json"}, "unknown option '--json'"},
		{"check global value", []string{"check", "--global=1"}, "--global does not take a value"},
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

func TestParseRemoveOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want app.RemoveOptions
	}{
		{
			name: "names and agent",
			args: []string{"remove", "alpha", "beta", "-a", "claude-code", "opencode", "-y"},
			want: app.RemoveOptions{Skills: []string{"alpha", "beta"},
				Agents: []string{"claude-code", "opencode"}, Yes: true},
		},
		{
			name: "aliases",
			args: []string{"rm", "alpha"},
			want: app.RemoveOptions{Skills: []string{"alpha"}},
		},
		{
			name: "single letter alias",
			args: []string{"r", "alpha"},
			want: app.RemoveOptions{Skills: []string{"alpha"}},
		},
		{
			name: "skill flag and positionals accumulate",
			args: []string{"remove", "alpha", "-s", "beta", "--skill=gamma"},
			want: app.RemoveOptions{Skills: []string{"alpha", "beta", "gamma"}},
		},
		{
			name: "wildcard skill",
			args: []string{"remove", "-s", "*", "-y"},
			want: app.RemoveOptions{Skills: []string{"*"}, Yes: true},
		},
		{
			name: "all implies every agent and yes",
			args: []string{"remove", "--all", "-a", "claude-code"},
			want: app.RemoveOptions{Agents: []string{"*"}, All: true, Yes: true},
		},
		{
			name: "double dash keeps an option-like name",
			args: []string{"remove", "-y", "--", "-weird"},
			want: app.RemoveOptions{Skills: []string{"-weird"}, Yes: true},
		},
		{
			name: "wildcard agent",
			args: []string{"remove", "alpha", "-a", "*", "-y"},
			want: app.RemoveOptions{Skills: []string{"alpha"}, Agents: []string{"*"}, Yes: true},
		},
		{
			name: "global",
			args: []string{"remove", "alpha", "-g"},
			want: app.RemoveOptions{Skills: []string{"alpha"}, Global: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindRemove {
				t.Fatalf("Kind = %v, want KindRemove", cmd.Kind)
			}
			if !reflect.DeepEqual(cmd.Remove, tc.want) {
				t.Errorf("Remove = %+v, want %+v", cmd.Remove, tc.want)
			}
		})
	}
}

func TestParseRemoveUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"remove", "--force"}, "unknown option '--force'"},
		{"unknown short", []string{"remove", "-q"}, "unknown option '-q'"},
		{"agent without value", []string{"remove", "-a"}, "--agent requires an agent name"},
		{"skill without value", []string{"remove", "--skill"}, "--skill requires a skill name"},
		{"boolean with value", []string{"remove", "--all=1"}, "--all does not take a value"},
		{"short value not last", []string{"remove", "-ay"}, "-a takes a value and must be last"},
		{"help", []string{"remove", "--help"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if tc.want == "" {
				if err != nil || cmd.Kind != KindHelp {
					t.Fatalf("Parse %v = %v, %v; want help", tc.args, cmd.Kind, err)
				}
				return
			}
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

func TestParseUpdateOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want app.UpdateOptions
	}{
		{
			name: "positional names",
			args: []string{"update", "alpha", "Beta"},
			want: app.UpdateOptions{Skills: []string{"alpha", "Beta"}},
		},
		{
			name: "alias with project and yes",
			args: []string{"upgrade", "alpha", "-p", "-y"},
			want: app.UpdateOptions{Skills: []string{"alpha"}, Project: true, Yes: true},
		},
		{
			name: "grouped short flags",
			args: []string{"update", "-py"},
			want: app.UpdateOptions{Project: true, Yes: true},
		},
		{
			name: "long flags",
			args: []string{"update", "--project", "--yes", "alpha"},
			want: app.UpdateOptions{Skills: []string{"alpha"}, Project: true, Yes: true},
		},
		{
			name: "double dash keeps an option-like name",
			args: []string{"update", "--", "-weird"},
			want: app.UpdateOptions{Skills: []string{"-weird"}},
		},
		{
			name: "global",
			args: []string{"update", "-g"},
			want: app.UpdateOptions{Global: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindUpdate {
				t.Fatalf("Kind = %v, want KindUpdate", cmd.Kind)
			}
			if !reflect.DeepEqual(cmd.Update, tc.want) {
				t.Errorf("Update = %+v, want %+v", cmd.Update, tc.want)
			}
		})
	}
}

func TestParseUpdateUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"update", "--force"}, "unknown option '--force'"},
		{"unknown short", []string{"update", "-q"}, "unknown option '-q'"},
		{"boolean with value", []string{"update", "--project=1"}, "--project does not take a value"},
		{"short value", []string{"update", "-y=1"}, "-y does not take a value"},
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

func TestParseUpdateHelp(t *testing.T) {
	for _, args := range [][]string{{"update", "-h"}, {"upgrade", "--help"}} {
		cmd, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse %v: %v", args, err)
		}
		if cmd.Kind != KindHelp {
			t.Errorf("Parse %v = %v, want help", args, cmd.Kind)
		}
	}
}

func TestParseInit(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want app.InitOptions
	}{
		{"no name", []string{"init"}, app.InitOptions{}},
		{"name", []string{"init", "my-skill"}, app.InitOptions{Name: "my-skill"}},
		{"name after terminator", []string{"init", "--", "-my-skill"}, app.InitOptions{Name: "-my-skill"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindInit {
				t.Fatalf("Kind = %v, want KindInit", cmd.Kind)
			}
			if cmd.Init != tc.want {
				t.Errorf("Init = %+v, want %+v", cmd.Init, tc.want)
			}
		})
	}
}

func TestParseInitUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"two names", []string{"init", "one", "two"}, "unexpected argument 'two'"},
		{"empty name", []string{"init", ""}, "skill name must not be empty"},
		{"unknown flag", []string{"init", "--force"}, "unknown option '--force'"},
		{"unknown short", []string{"init", "-q"}, "unknown option '-q'"},
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

func TestParseInitHelp(t *testing.T) {
	for _, args := range [][]string{{"init", "-h"}, {"init", "--help"}} {
		cmd, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse %v: %v", args, err)
		}
		if cmd.Kind != KindInit || !cmd.InitHelp {
			t.Errorf("Parse %v = %v (InitHelp %v), want init help", args, cmd.Kind, cmd.InitHelp)
		}
	}
}

func TestInitHelpNamesTheCommand(t *testing.T) {
	help := InitHelp()
	for _, want := range []string{
		"Usage: skills init", "SKILL.md", "./<name>/SKILL.md", "YAML-escaped",
		"never overwritten", "escape", "exit 2",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("init help misses %q", want)
		}
	}
	if strings.Contains(help, "Add options:") {
		t.Error("init help leaked the top-level help")
	}
}

func TestHelpListsAgentsAndOnlyWorkingFlags(t *testing.T) {
	help := Help()
	for _, want := range []string{
		"claude-code", "opencode", "--copy", "--list", "--json", "--subagent",
		"owner/repo@v1.2.3", "git@github.com:owner/repo.git", "git on PATH",
		"remove, rm, r", "--all", "update, upgrade", "--project",
		"experimental_install", "Restore options:", "--dry-run",
		"--frozen-lockfile", "--no-frozen-lockfile", "JENKINS_URL", "init [name]",
		"find, f, s, search", "Find options:", "--owner <owner>",
		"Not supported in this build:", "experimental_sync, sync", "npm/node_modules",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help misses %q", want)
		}
	}
	if !strings.Contains(help, "--global") {
		t.Error("help misses the global scope flag")
	}
}

func TestParseFind(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want FindOptions
	}{
		{"find with one word", []string{"find", "react"}, FindOptions{Query: "react"}},
		{"f alias", []string{"f", "react"}, FindOptions{Query: "react"}},
		{"s alias", []string{"s", "react"}, FindOptions{Query: "react"}},
		{"search alias", []string{"search", "react"}, FindOptions{Query: "react"}},
		{"positionals join with single spaces", []string{"find", "react", "native"},
			FindOptions{Query: "react native"}},
		{"owner is trimmed and lowercased", []string{"find", "react", "--owner", " Vercel "},
			FindOptions{Query: "react", Owner: "vercel"}},
		{"last owner wins", []string{"find", "react", "--owner", "first", "--owner=Second"},
			FindOptions{Query: "react", Owner: "second"}},
		{"no query", []string{"find"}, FindOptions{}},
		{"double dash", []string{"find", "--", "--weird"}, FindOptions{Query: "--weird"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if cmd.Kind != KindFind {
				t.Fatalf("Kind = %v, want KindFind", cmd.Kind)
			}
			if !reflect.DeepEqual(cmd.Find, tc.want) {
				t.Errorf("Find = %+v, want %+v", cmd.Find, tc.want)
			}
		})
	}
}

func TestParseFindRejectsBadOwnerAndOptions(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"find", "react", "--owner"}, "--owner requires a GitHub owner"},
		{[]string{"find", "react", "--owner="}, "--owner requires a GitHub owner"},
		{[]string{"find", "react", "--owner", "bad_owner"}, "--owner must be a valid GitHub owner"},
		{[]string{"find", "react", "--owner", strings.Repeat("a", 40)},
			"--owner must be a valid GitHub owner"},
		{[]string{"find", "react", "--json"}, "unknown option '--json'"},
		{[]string{"find", "react", "-y"}, "unknown option '-y'"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
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

func TestParseFindHelp(t *testing.T) {
	for _, args := range [][]string{{"find", "-h"}, {"find", "--help"}} {
		cmd, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse %v: %v", args, err)
		}
		if cmd.Kind != KindHelp {
			t.Errorf("Parse %v = %v, want help", args, cmd.Kind)
		}
	}
}
