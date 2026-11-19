package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
)

func TestWriteRestoreSummarises(t *testing.T) {
	rep := app.RestoreReport{
		Unrestorable: []app.UnrestorableSkill{
			{Name: "local\nskill", SourceType: "local", Reason: "Local paths are not restored"},
		},
		Checks: []app.RestoreCheck{
			{Source: "file:///repo"},
			{Source: "file:///other", Err: "failed to clone"},
		},
		Results: []app.RestoreResult{
			{Name: "alpha", Status: app.RestoreRestored},
			{Name: "beta", Status: app.RestoreRelinked},
			{Name: "gamma", Status: app.RestoreSkipped, Reason: "already installed"},
			{Name: "delta", Status: app.RestoreFailed, Reason: "not found in the source"},
		},
		LockPath: "/p/skills-lock.json",
	}
	var out bytes.Buffer
	WriteRestore(&out, rep)
	if strings.ContainsAny(out.String(), "\x1b\r") {
		t.Errorf("output carries terminal escapes: %q", out.String())
	}
	for _, want := range []string{
		"Skipped local skill: Local paths are not restored",
		"✗ Failed to restore from file:///other: failed to clone",
		"✓ Restored alpha",
		"✓ Relinked beta",
		"Skipped gamma: already installed",
		"✗ Failed to restore delta: not found in the source",
		"Restored 1, relinked 1, skipped 2, failed 2",
		"Updated /p/skills-lock.json",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}
}

func TestWriteRestoreDryRun(t *testing.T) {
	rep := app.RestoreReport{
		DryRun: true,
		Results: []app.RestoreResult{
			{Name: "alpha", Status: app.RestoreRestored},
			{Name: "beta", Status: app.RestoreRelinked},
			{Name: "gamma", Status: app.RestoreSkipped},
		},
	}
	var out bytes.Buffer
	WriteRestore(&out, rep)
	for _, want := range []string{"would restore alpha", "would relink beta", "would skip gamma"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "✓") {
		t.Errorf("dry run reports completed work:\n%s", out.String())
	}
}
