package e2e

import (
	"strings"
	"testing"
)

// TestE2EExperimentalSyncStub asserts that the excluded npm/node_modules integration fails as a
// usage error and points at add, instead of looking like an unknown command.
func TestE2EExperimentalSyncStub(t *testing.T) {
	for _, command := range []string{"experimental_sync", "sync"} {
		t.Run(command, func(t *testing.T) {
			res := run(t, t.TempDir(), command)
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstderr:\n%s", res.exit, res.stderr)
			}
			if res.stdout != "" {
				t.Errorf("stdout = %q, want empty", res.stdout)
			}
			for _, want := range []string{"sync is not supported", "npm", "node_modules", "skills add"} {
				if !strings.Contains(res.stderr, want) {
					t.Errorf("stderr misses %q:\n%s", want, res.stderr)
				}
			}
			if !strings.Contains(res.stderr, "Run 'skills --help' for usage.") {
				t.Errorf("stderr misses the usage pointer:\n%s", res.stderr)
			}
		})
	}
}

// TestE2EExperimentalSyncHelpStaysAStub asserts that asking a dead command for help does not
// revive it: the stub exits 2 just like a bare run.
func TestE2EExperimentalSyncHelpStaysAStub(t *testing.T) {
	res := run(t, t.TempDir(), "experimental_sync", "--help")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr:\n%s", res.exit, res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout = %q, want empty", res.stdout)
	}
}
