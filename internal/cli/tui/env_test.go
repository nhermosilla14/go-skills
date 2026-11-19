package tui

import (
	"os"
	"testing"
)

// TestMain pins the environment the layout depends on, so a developer's terminal or locale cannot
// change a golden file. Goldens also pass the theme options explicitly.
func TestMain(m *testing.M) {
	os.Setenv("NO_COLOR", "1")
	os.Setenv("TERM", "dumb")
	os.Setenv("LANG", "C.UTF-8")
	os.Exit(m.Run())
}
