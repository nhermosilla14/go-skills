package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// decodeDocument requires stdout to hold exactly one JSON array and nothing else.
func decodeDocument(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc []map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not a JSON document: %v\nstdout:\n%s", err, stdout)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout holds more than one document:\n%s", stdout)
	}
	return doc
}

func sourceDir(t *testing.T) string {
	t.Helper()
	source, err := filepath.Abs("testdata/source")
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestE2EJSONContract(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		project := t.TempDir()
		res := run(t, project, "add", sourceDir(t), "-a", "claude-code", "-a", "opencode", "-y", "--json")
		if res.exit != 0 {
			t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
		}
		doc := decodeDocument(t, res.stdout)
		if len(doc) != 2 {
			t.Fatalf("document has %d entries, want 2:\n%s", len(doc), res.stdout)
		}
		entry := doc[0]
		if entry["name"] != "flat-skill" || entry["status"] != "installed" {
			t.Errorf("entry = %v", entry)
		}
		if entry["source"] != sourceDir(t) {
			t.Errorf("source = %v, want %q", entry["source"], sourceDir(t))
		}
		if entry["scope"] != "project" || entry["mode"] != "symlink" {
			t.Errorf("scope/mode = %v/%v", entry["scope"], entry["mode"])
		}
		if hash, _ := entry["hash"].(string); hash == "" {
			t.Error("hash is missing")
		}
		if path, _ := entry["path"].(string); !strings.HasSuffix(path, filepath.Join(".claude", "skills", "flat-skill")) {
			t.Errorf("path = %q", path)
		}
		agents, _ := entry["agents"].([]any)
		if len(agents) != 2 || agents[0] != "Claude Code" || agents[1] != "OpenCode" {
			t.Errorf("agents = %v, want both installed targets in registry order", agents)
		}
		for _, forbidden := range []string{"security", "fallback"} {
			if _, ok := entry[forbidden]; ok {
				t.Errorf("entry carries the upstream %q field", forbidden)
			}
		}
		if strings.Contains(res.stderr, "[") && strings.Contains(res.stderr, `"status"`) {
			t.Errorf("stderr carries JSON:\n%s", res.stderr)
		}
	})

	t.Run("installed entry excludes a skipped target", func(t *testing.T) {
		res := run(t, t.TempDir(), "add", sourceDir(t), "-a", "claude-code", "-a", "aider-desk", "-y", "--json")
		if res.exit != 0 {
			t.Fatalf("exit = %d, want 0 for an installed entry\nstderr:\n%s", res.exit, res.stderr)
		}
		doc := decodeDocument(t, res.stdout)
		if len(doc) != 2 {
			t.Fatalf("document has %d entries, want 2", len(doc))
		}
		entry := doc[0]
		if entry["status"] != "installed" {
			t.Errorf("status = %v, want installed", entry["status"])
		}
		agents, _ := entry["agents"].([]any)
		if len(agents) != 1 || agents[0] != "Claude Code" {
			t.Errorf("agents = %v, want only the installed target", agents)
		}
		if _, ok := entry["reason"]; ok {
			t.Errorf("installed entry kept a skip reason: %v", entry["reason"])
		}
	})

	t.Run("skipped target excluded from agents", func(t *testing.T) {
		res := run(t, t.TempDir(), "add", sourceDir(t), "-a", "aider-desk", "-a", "kiro-cli", "-y", "--json")
		if res.exit != 1 {
			t.Fatalf("exit = %d, want 1 because an entry was skipped\nstderr:\n%s", res.exit, res.stderr)
		}
		doc := decodeDocument(t, res.stdout)
		if len(doc) != 2 {
			t.Fatalf("document has %d entries, want 2", len(doc))
		}
		entry := doc[0]
		if entry["status"] != "skipped" {
			t.Errorf("status = %v, want skipped", entry["status"])
		}
		if reason, _ := entry["reason"].(string); !strings.Contains(reason, "project directory not found") {
			t.Errorf("reason = %q", reason)
		}
		if _, ok := entry["agents"]; ok {
			t.Errorf("skipped entry kept the agent list: %v", entry["agents"])
		}
	})

	t.Run("invalid agent", func(t *testing.T) {
		res := run(t, t.TempDir(), "add", sourceDir(t), "-a", "claude_code", "-y", "--json")
		if res.exit == 0 {
			t.Fatal("exit = 0, want a failure")
		}
		doc := decodeDocument(t, res.stdout)
		if len(doc) != 1 || doc[0]["status"] != "failed" {
			t.Fatalf("document = %v, want one failed entry", doc)
		}
		if msg, _ := doc[0]["error"].(string); !strings.Contains(msg, "invalid agents") {
			t.Errorf("error = %q", msg)
		}
	})

	t.Run("list conflict", func(t *testing.T) {
		res := run(t, t.TempDir(), "add", sourceDir(t), "--list", "-y", "--json")
		if res.exit != 1 {
			t.Fatalf("exit = %d, want 1", res.exit)
		}
		doc := decodeDocument(t, res.stdout)
		if len(doc) != 1 || doc[0]["status"] != "failed" {
			t.Fatalf("document = %v, want one failed entry", doc)
		}
		want := "The --json flag cannot be combined with --list."
		if msg, _ := doc[0]["error"].(string); msg != want {
			t.Errorf("error = %q, want %q", msg, want)
		}
	})

	t.Run("json without yes", func(t *testing.T) {
		res := run(t, t.TempDir(), "add", sourceDir(t), "-a", "opencode", "--json")
		if res.exit != 1 {
			t.Fatalf("exit = %d, want 1", res.exit)
		}
		doc := decodeDocument(t, res.stdout)
		want := "The --json flag requires --yes (or --all) to run non-interactively."
		if len(doc) != 1 || doc[0]["error"] != want {
			t.Fatalf("document = %v, want one failed entry with %q", doc, want)
		}
	})

	t.Run("argument error still emits the empty document", func(t *testing.T) {
		res := run(t, t.TempDir(), "add", "--json", "--no-such-flag", sourceDir(t))
		if res.exit == 0 {
			t.Fatal("exit = 0, want a failure")
		}
		if doc := decodeDocument(t, res.stdout); len(doc) != 0 {
			t.Fatalf("document = %v, want the empty array", doc)
		}
		if !strings.Contains(res.stderr, "unknown option") {
			t.Errorf("stderr = %q", res.stderr)
		}
	})
}

// runWithStdin runs the binary with a specific stdin, so the null device can be used.
func runWithStdin(t *testing.T, dir string, stdin *os.File, args ...string) result {
	t.Helper()
	cmd := exec.Command(skillsBin, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
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

func TestE2ENoTTYPromptError(t *testing.T) {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()

	project := t.TempDir()
	res := runWithStdin(t, project, devNull, "add", sourceDir(t), "-a", "opencode")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstderr:\n%s", res.exit, res.stderr)
	}
	want := "Interactive prompt required but stdin is not a TTY. Nothing was installed. " +
		"Use --agent <name> (or --agent '*') and -y to run non-interactively."
	if !strings.Contains(res.stderr, want) {
		t.Errorf("stderr does not carry the documented diagnostic:\n%s", res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); !os.IsNotExist(err) {
		t.Errorf("the refused run wrote a lock: %v", err)
	}
}

func TestE2EStreamSeparation(t *testing.T) {
	project := t.TempDir()
	res := run(t, project, "add", sourceDir(t), "-a", "opencode", "-y")
	if res.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("add wrote to stdout: %q", res.stdout)
	}
	if !strings.Contains(res.stderr, "Installed 2, skipped 0, failed 0") {
		t.Errorf("stderr misses the summary:\n%s", res.stderr)
	}

	list := run(t, project, "add", sourceDir(t), "--list")
	if list.exit != 0 {
		t.Fatalf("--list exit = %d\nstderr:\n%s", list.exit, list.stderr)
	}
	if !strings.Contains(list.stdout, "flat-skill\t") {
		t.Errorf("--list stdout misses the listing: %q", list.stdout)
	}
	if strings.Contains(list.stdout, "warning:") {
		t.Errorf("--list stdout carries a warning: %q", list.stdout)
	}
	if !strings.Contains(list.stderr, "warning:") {
		t.Errorf("--list stderr misses the discovery warning:\n%s", list.stderr)
	}

	help := run(t, project, "--help")
	if help.exit != 0 || !strings.HasPrefix(help.stdout, "Usage: skills") {
		t.Errorf("--help wrote to the wrong stream: %q", help.stdout)
	}
}
