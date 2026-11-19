package hash

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeTree creates files under dir; keys are slash-separated relative paths.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDirPublishedVectors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"empty", nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{
			"single file",
			map[string]string{"SKILL.md": "# hi\n"},
			"93c36952d8843b33e68ae970ebabcc03e1687c9bb0dd1ecaf357060318952f55",
		},
		{
			"two files",
			map[string]string{"SKILL.md": "a\n", "docs/b.md": "b\n"},
			"b22c4d22aa319d2924312fa18fcbfedc88fa89c74dbd8f298c683e1caf7b7f24",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, tc.files)
			got, err := Dir(dir)
			if err != nil {
				t.Fatalf("Dir: %v", err)
			}
			if got != tc.want {
				t.Errorf("Dir = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDirIgnoresExcludedEntries(t *testing.T) {
	base := t.TempDir()
	plain := filepath.Join(base, "plain")
	noisy := filepath.Join(base, "noisy")
	writeTree(t, plain, map[string]string{"SKILL.md": "x\n"})
	writeTree(t, noisy, map[string]string{
		"SKILL.md":                "x\n",
		"metadata.json":           "{}",
		"sub/metadata.json":       "{}",
		".git/HEAD":               "ref",
		"__pycache__/a.pyc":       "bin",
		"__pypackages__/lib/x.py": "x",
		"sub/.git/config":         "cfg",
		"sub/__pycache__/mod.pyc": "bin",
	})
	want, _ := Dir(plain)
	got, err := Dir(noisy)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != want {
		t.Errorf("excluded entries changed the hash: %s != %s", got, want)
	}
}

func TestDirKeepsFileNamedGit(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeTree(t, a, map[string]string{"SKILL.md": "x\n"})
	writeTree(t, b, map[string]string{"SKILL.md": "x\n", ".git": "file"})
	ha, _ := Dir(a)
	hb, _ := Dir(b)
	if ha == hb {
		t.Error("a regular file named .git must be hashed")
	}
}

func TestDirNormalizesCRLFOnlyInText(t *testing.T) {
	lf := t.TempDir()
	crlf := t.TempDir()
	writeTree(t, lf, map[string]string{"SKILL.md": "a\nb\n"})
	writeTree(t, crlf, map[string]string{"SKILL.md": "a\r\nb\r\n"})
	hl, _ := Dir(lf)
	hc, _ := Dir(crlf)
	if hl != hc {
		t.Errorf("text CRLF and LF hash differently: %s != %s", hc, hl)
	}

	binLF := t.TempDir()
	binCRLF := t.TempDir()
	writeTree(t, binLF, map[string]string{"x.bin": "\x00a\nb\n"})
	writeTree(t, binCRLF, map[string]string{"x.bin": "\x00a\r\nb\r\n"})
	bl, _ := Dir(binLF)
	bc, _ := Dir(binCRLF)
	if bl == bc {
		t.Error("binary CRLF must not be normalized")
	}
}

func TestDirSortsByUnsignedBytes(t *testing.T) {
	// "B.md" (0x42) < "b.md" (0x62) < "ä.md" (0xC3 0xA4): the framing must follow that order.
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"b.md": "1", "B.md": "2"})
	got, err := Dir(dir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	want := "473fe1bf717fecac53c699774d475c352247993e66be7d3bb2ec70c17f9cae6e"
	if got != want {
		t.Errorf("Dir = %s, want %s", got, want)
	}
}

func TestDirSymlinkPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	plain := t.TempDir()
	writeTree(t, plain, map[string]string{"SKILL.md": "x\n"})
	want, _ := Dir(plain)

	dir := t.TempDir()
	outside := t.TempDir()
	writeTree(t, outside, map[string]string{"secret.txt": "s"})
	writeTree(t, dir, map[string]string{"SKILL.md": "x\n"})
	mustSymlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(dir, "escape.txt"))
	mustSymlink(t, filepath.Join(dir, "missing"), filepath.Join(dir, "broken"))
	mustSymlink(t, dir, filepath.Join(dir, "loop"))
	got, err := Dir(dir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != want {
		t.Errorf("escaping, broken or cyclic links changed the hash: %s != %s", got, want)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
}

func TestSingleFileMatchesTheSKILLMDDirectoryForm(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"SKILL.md": "---\nname: a\n---\nbody\n"})
	want, err := Dir(dir)
	if err != nil {
		t.Fatal(err)
	}
	flat := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(flat, []byte("---\nname: a\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SingleFile(flat)
	if err != nil {
		t.Fatalf("SingleFile: %v", err)
	}
	if got != want {
		t.Errorf("SingleFile = %s, want %s", got, want)
	}
}

func TestSingleFileNormalizesLineEndings(t *testing.T) {
	dir := t.TempDir()
	lf := filepath.Join(dir, "lf.md")
	crlf := filepath.Join(dir, "crlf.md")
	if err := os.WriteFile(lf, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crlf, []byte("a\r\nb\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want, err := SingleFile(lf)
	if err != nil {
		t.Fatal(err)
	}
	got, err := SingleFile(crlf)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("CRLF hash = %s, want %s", got, want)
	}
}
