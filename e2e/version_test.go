package e2e

import "testing"

// TestE2EVersionMetadata asserts that the build metadata TestMain injects through -ldflags is
// what --version reports, the contract release builds rely on.
func TestE2EVersionMetadata(t *testing.T) {
	res := run(t, t.TempDir(), "--version")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", res.exit, res.stderr)
	}
	want := "skills 0.0.0-test (commit 0000000, built 2000-01-01T00:00:00Z)\n"
	if res.stdout != want {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}
	if res.stderr != "" {
		t.Errorf("stderr = %q, want empty", res.stderr)
	}
}
