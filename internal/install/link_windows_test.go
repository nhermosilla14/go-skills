//go:build windows

package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// TestJunctionCreation proves the Windows linker writes an absolute-target junction. On hosts or
// filesystems without junction support the test skips; the copy fallback is covered on every
// platform by TestApplyLinkFailureFallsBackToCopy.
func TestJunctionCreation(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := platformLink(link, target); err != nil {
		t.Skipf("junctions are not supported here: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("junction is not reported as a link: %v", info.Mode())
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("junction target %q is not absolute", got)
	}
	if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
		t.Errorf("junction does not resolve: %v", err)
	}
}

func TestJunctionThenApplyLinks(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "src", "alpha")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: alpha\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selected, _ := agents.Expand([]string{"opencode", "claude-code"})
	entries := Plan(project, []skill.Skill{{Name: "alpha", Dir: src}}, selected,
		PlanOptions{Env: agents.Environment{GOOS: "windows", Home: t.TempDir(), Cwd: project, Vars: map[string]string{}}})
	results := Apply(project, entries)
	for _, r := range results {
		if r.Status != Installed {
			t.Fatalf("result %+v, want installed", r)
		}
	}
	link := filepath.Join(project, ".claude", "skills", "alpha")
	if _, err := os.Readlink(link); err != nil {
		// No junction privilege is fine only if the fallback copied instead.
		info, statErr := os.Lstat(link)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("destination is neither a junction nor a copied directory: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(link, "SKILL.md")); err != nil {
		t.Errorf("destination does not resolve: %v", err)
	}
}
