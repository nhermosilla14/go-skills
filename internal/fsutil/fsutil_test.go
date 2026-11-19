package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomicReplacesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new\n" {
		t.Fatalf("content = %q, %v; want %q", got, err, "new\n")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %d entries", len(entries))
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "proj")
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"root itself", root, true},
		{"child", filepath.Join(root, ".agents", "skills"), true},
		{"sibling with same prefix", root + "x", false},
		{"parent", filepath.Dir(root), false},
		{"dotdot prefix name", filepath.Join(root, "..x"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Within(root, tc.path); got != tc.want {
				t.Errorf("Within(%q, %q) = %v, want %v", root, tc.path, got, tc.want)
			}
		})
	}
}

func TestRealPathNonExistingTail(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := RealPath(filepath.Join(dir, "missing", "deeper"))
	if err != nil {
		t.Fatalf("RealPath: %v", err)
	}
	if want := filepath.Join(resolved, "missing", "deeper"); got != want {
		t.Errorf("RealPath = %q, want %q", got, want)
	}
}

func TestRemoveAllWithinRefusesOutsideAndRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := RemoveAllWithin(root, outside); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("outside path: err = %v, want ErrOutsideRoot", err)
	}
	if err := RemoveAllWithin(root, root); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("root itself: err = %v, want ErrOutsideRoot", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("outside directory was touched: %v", err)
	}
}

func TestRemoveAllWithinRemovesSymlinkNotTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllWithin(root, link); err != nil {
		t.Fatalf("RemoveAllWithin: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("symlink target removed: %v", err)
	}
}
