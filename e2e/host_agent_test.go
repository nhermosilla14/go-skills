package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EHostAgentDetection runs an install and a removal with a host marker and without -y: the
// run is non-interactive, targets the mapped agent, and --json is accepted.
func TestE2EHostAgentDetection(t *testing.T) {
	host := []string{"OPENCODE=1"}
	project := t.TempDir()
	src := sourceDir(t)

	res := runEnv(t, project, host, "add", src)
	if res.exit != 0 {
		t.Fatalf("add exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "OpenCode Agent detected — installing non-interactively") {
		t.Errorf("stderr misses the detected agent:\n%s", res.stderr)
	}
	canonical := filepath.Join(project, ".agents", "skills", "flat-skill")
	if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
		t.Fatalf("the host install is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude", "skills", "flat-skill")); !os.IsNotExist(err) {
		t.Errorf("the install wrote an agent-specific copy: %v", err)
	}

	t.Run("json without yes", func(t *testing.T) {
		jsonProject := t.TempDir()
		res := runEnv(t, jsonProject, host, "add", src, "-a", "opencode", "--json")
		if res.exit != 0 {
			t.Fatalf("json add exit = %d\nstderr:\n%s", res.exit, res.stderr)
		}
		doc := decodeDocument(t, res.stdout)
		if len(doc) != 2 {
			t.Fatalf("document = %v, want one entry per installed skill", doc)
		}
		for _, entry := range doc {
			if entry["status"] != "installed" {
				t.Errorf("entry = %v, want installed", entry)
			}
		}
	})

	res = runEnv(t, project, host, "remove", "flat-skill")
	if res.exit != 0 {
		t.Fatalf("remove exit = %d\nstderr:\n%s", res.exit, res.stderr)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Errorf("the removal left the canonical copy: %v", err)
	}
}

// TestE2ECursorTraceIDIsNotAnAgent proves the Cursor demotion end to end: its integrated terminal
// marker alone keeps the interactive flow, which a non-terminal run refuses.
func TestE2ECursorTraceIDIsNotAnAgent(t *testing.T) {
	res := runEnv(t, t.TempDir(), []string{"CURSOR_TRACE_ID=trace"}, "add", sourceDir(t))
	if res.exit != 1 {
		t.Fatalf("exit = %d, want the interactive refusal\nstderr:\n%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "Agent detected") {
		t.Errorf("a bare CURSOR_TRACE_ID was reported as an agent:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr, "Interactive prompt required") {
		t.Errorf("stderr misses the documented refusal:\n%s", res.stderr)
	}
}
