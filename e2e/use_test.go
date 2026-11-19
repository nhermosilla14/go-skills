package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// usePrompt builds the prompt for a SKILL.md file with the given contents and no supporting files.
func usePrompt(t *testing.T, skillMD string) string {
	t.Helper()
	raw, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read %s: %v", skillMD, err)
	}
	return usePromptContent(string(raw))
}

func usePromptContent(contents string) string {
	return "You are being given a Skill to execute for the user's next request.\n\n" +
		"Use the following SKILL.md as your instructions:\n\n" +
		"<SKILL.md>\n" + contents + "\n</SKILL.md>\n"
}

func TestE2EUsePrompt(t *testing.T) {
	project := t.TempDir()
	res := runUse(t, project, "use", sourceDir(t), "--skill", "flat-skill")
	if res.exit != 0 {
		t.Fatalf("use exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if want := usePrompt(t, filepath.Join(sourceDir(t), "skills", "flat", "SKILL.md")); res.stdout != want {
		t.Errorf("stdout differs from the documented prompt:\n--- got ---\n%q\n--- want ---\n%q", res.stdout, want)
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
	for _, leftover := range []string{"skills-lock.json", filepath.Join(".agents", "skills")} {
		if _, err := os.Stat(filepath.Join(project, leftover)); !os.IsNotExist(err) {
			t.Errorf("use left %s behind: %v", leftover, err)
		}
	}
}

func TestE2EUseSelection(t *testing.T) {
	t.Run("several skills need a selector", func(t *testing.T) {
		res := runUse(t, t.TempDir(), "use", sourceDir(t))
		if res.exit != 1 {
			t.Fatalf("exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
		}
		if res.stdout != "" {
			t.Errorf("stdout = %q, want empty on failure", res.stdout)
		}
		for _, want := range []string{"multiple skills", "flat-skill", "nested-skill"} {
			if !strings.Contains(res.stderr, want) {
				t.Errorf("stderr misses %q:\n%s", want, res.stderr)
			}
		}
	})

	t.Run("at selector on a git source", func(t *testing.T) {
		fx := newGitFixture(t)
		res := runUse(t, t.TempDir(), "use", fx.url+"#@alpha")
		if res.exit != 0 {
			t.Fatalf("exit = %d\nstderr:\n%s", res.exit, res.stderr)
		}
		if want := usePromptContent("---\nname: alpha\ndescription: d\n---\nversion two\n"); res.stdout != want {
			t.Errorf("stdout differs from the selected skill's prompt:\n--- got ---\n%q\n--- want ---\n%q", res.stdout, want)
		}
		if strings.Contains(res.stdout, "beta") {
			t.Error("the prompt carries another skill")
		}
	})

	t.Run("missing skill", func(t *testing.T) {
		res := runUse(t, t.TempDir(), "use", sourceDir(t), "--skill", "missing")
		if res.exit != 1 || res.stdout != "" {
			t.Fatalf("exit = %d, stdout = %q, want exit 1 and no stdout", res.exit, res.stdout)
		}
		for _, want := range []string{"No matching skill", "flat-skill", "nested-skill"} {
			if !strings.Contains(res.stderr, want) {
				t.Errorf("stderr misses %q:\n%s", want, res.stderr)
			}
		}
	})

	t.Run("duplicate skills", func(t *testing.T) {
		res := runUse(t, t.TempDir(), "use", sourceDir(t), "--skill", "flat-skill", "--skill", "nested-skill")
		if res.exit != 1 || res.stdout != "" {
			t.Fatalf("exit = %d, stdout = %q, want exit 1 and no stdout", res.exit, res.stdout)
		}
		if !strings.Contains(res.stderr, "Only one --skill value can be provided") {
			t.Errorf("stderr misses the duplicate-skill error:\n%s", res.stderr)
		}
	})

	t.Run("full depth", func(t *testing.T) {
		root, err := filepath.Abs("testdata/root-source")
		if err != nil {
			t.Fatal(err)
		}
		res := runUse(t, t.TempDir(), "use", root, "--skill", "hidden-skill")
		if res.exit != 1 || !strings.Contains(res.stderr, "No matching skill") {
			t.Fatalf("without --full-depth: exit = %d, stderr = %q", res.exit, res.stderr)
		}
		res = runUse(t, t.TempDir(), "use", root, "--skill", "hidden-skill", "--full-depth")
		if res.exit != 0 {
			t.Fatalf("with --full-depth: exit = %d\nstderr:\n%s", res.exit, res.stderr)
		}
		if !strings.Contains(res.stdout, "Hidden body.") {
			t.Errorf("prompt misses the deeper skill:\n%s", res.stdout)
		}
	})
}

func TestE2EUseAgentLaunch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a POSIX shell script")
	}
	fake := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' \"$1\" > \"$CLAUDE_ARGS_FILE\"\nexit 7\n"
	if err := os.WriteFile(filepath.Join(fake, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args")
	env := append(useTempEnv(t), "CLAUDE_ARGS_FILE="+argsFile)
	res := runUseAgentEnv(t, t.TempDir(), fake+string(os.PathListSeparator)+os.Getenv("PATH"),
		env, "use", sourceDir(t), "--skill", "flat-skill", "--agent", "claude-code")
	if res.exit != 7 {
		t.Fatalf("exit = %d, want the agent's 7\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty with --agent", res.stdout)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the agent did not receive the prompt: %v", err)
	}
	if want := usePrompt(t, filepath.Join(sourceDir(t), "skills", "flat", "SKILL.md")); string(raw) != want {
		t.Errorf("agent argv differs from the prompt:\n--- got ---\n%q\n--- want ---\n%q", raw, want)
	}

	missing := t.TempDir()
	res = runUseAgentEnv(t, t.TempDir(), missing, useTempEnv(t),
		"use", sourceDir(t), "--skill", "flat-skill", "--agent", "codex")
	if res.exit != 1 {
		t.Fatalf("missing agent exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
	if want := "Could not launch Codex: command not found: codex\n"; res.stderr != want {
		t.Errorf("stderr = %q, want %q", res.stderr, want)
	}
}

// runUse runs `use` with an isolated temp directory, so the kept materialized folder is removed
// with the test.
func runUse(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runUseAgentEnv(t, dir, os.Getenv("PATH"), useTempEnv(t), args...)
}

// useTempEnv points the OS temp directory at a test-owned directory.
func useTempEnv(t *testing.T) []string {
	t.Helper()
	tmp := t.TempDir()
	return []string{"TMPDIR=" + tmp, "TMP=" + tmp, "TEMP=" + tmp}
}

// runUseAgentEnv runs the binary with PATH and the temp directory replaced, so a fake agent on PATH
// is the one launched and the materialized skill dies with the test.
func runUseAgentEnv(t *testing.T, dir, path string, extraEnv []string, args ...string) result {
	t.Helper()
	env := make([]string, 0, len(os.Environ())+len(extraEnv)+1)
	for _, kv := range withoutCI(os.Environ()) {
		key, _, _ := splitEnv(kv)
		switch key {
		case "PATH", "TMPDIR", "TMP", "TEMP":
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "PATH="+path)
	env = append(env, extraEnv...)
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), exit: exit}
}
