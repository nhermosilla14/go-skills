package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestInternalSkillsEnabled(t *testing.T) {
	tests := map[string]bool{
		"1":    true,
		"true": true,
		"TRUE": false,
		"yes":  false,
		"0":    false,
		"":     false,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv("INSTALL_INTERNAL_SKILLS", value)
			if got := internalSkillsEnabled(); got != want {
				t.Errorf("internalSkillsEnabled() with %q = %v, want %v", value, got, want)
			}
		})
	}
}

func TestRunUseHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"use", "--help"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: skills use") {
		t.Errorf("stdout is not the use help:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Add options:") {
		t.Errorf("stdout is the top-level help:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunInitHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"init", "--help"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit = %d, want 0\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: skills init") {
		t.Errorf("stdout is not the init help:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Add options:") {
		t.Errorf("stdout is the top-level help:\n%s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUseMissingSource(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"use"}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on failure", stdout.String())
	}
	for _, want := range []string{"missing required argument: source", "Usage: skills use"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr misses %q:\n%s", want, stderr.String())
		}
	}
}
