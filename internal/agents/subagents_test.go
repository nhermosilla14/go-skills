package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEveSubagentsSortsDirectories(t *testing.T) {
	project := t.TempDir()
	for _, name := range []string{"writer", "reviewer"} {
		if err := os.MkdirAll(filepath.Join(project, "agent", "subagents", name, "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(project, "agent", "subagents", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := EveSubagents(Environment{Cwd: project})
	want := []string{"reviewer", "writer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EveSubagents = %v, want %v", got, want)
	}
}

func TestEveSubagentsWithoutDirectory(t *testing.T) {
	if got := EveSubagents(Environment{Cwd: t.TempDir()}); len(got) != 0 {
		t.Errorf("EveSubagents = %v, want none", got)
	}
}
