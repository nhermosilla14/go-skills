package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
)

func TestWriteUpdate(t *testing.T) {
	rep := app.UpdateReport{
		Checks: []app.UpdateCheck{
			{Source: "file:///repo"},
			{Source: "owner/gone", Err: "fatal: repository not found"},
		},
		Results: []app.UpdateResult{
			{Name: "alpha", Status: app.UpdateCurrent},
			{Name: "beta", Status: app.UpdateUpdated},
			{Name: "gamma", Status: app.UpdateSkipped, Reason: "several skills match this name"},
			{Name: "delta", Status: app.UpdateFailed, Reason: "not found in the source"},
		},
		Uncheckable: []app.UncheckableSkill{
			{Name: "local-skill", Reason: "Local path", Source: "./src"},
		},
		Notes: []string{"note: an example note"},
	}
	var out bytes.Buffer
	WriteUpdate(&out, rep)
	want := `Checking for skill updates…
Checking skills from source: file:///repo
✗ Failed to check skills from owner/gone
Updating beta…
✓ Updated beta
Skipped gamma: several skills match this name
✗ Failed to update delta: not found in the source
note: an example note
1 skill(s) cannot be checked automatically:
  local-skill  Local path
To update: skills add ./src -y
Updated 1 skill(s)
Failed to update 1 skill(s)
`
	if out.String() != want {
		t.Errorf("output =\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestWriteUpdateUpToDate(t *testing.T) {
	var out bytes.Buffer
	WriteUpdate(&out, app.UpdateReport{
		Checks:  []app.UpdateCheck{{Source: "file:///repo"}},
		Results: []app.UpdateResult{{Name: "alpha", Status: app.UpdateCurrent}},
	})
	want := "Checking for skill updates…\n" +
		"Checking skills from source: file:///repo\n" +
		"✓ All project skills are up to date\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestWriteUpdateNothingToUpdate(t *testing.T) {
	var out bytes.Buffer
	WriteUpdate(&out, app.UpdateReport{
		NothingToUpdate: true,
		Notes:           []string{"No project skills to update.", "Install project skills with skills add <source>."},
	})
	want := "Checking for skill updates…\n" +
		"No project skills to update.\n" +
		"Install project skills with skills add <source>.\n"
	if out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}

func TestWriteUpdateSuppressesTheSummaryWhenSkipping(t *testing.T) {
	var out bytes.Buffer
	WriteUpdate(&out, app.UpdateReport{
		Uncheckable: []app.UncheckableSkill{{Name: "local", Reason: "Local path", Source: "./src"}},
	})
	if strings.Contains(out.String(), "up to date") {
		t.Errorf("output claims everything is current:\n%s", out.String())
	}
}

func TestWriteUpdateSanitizesUntrustedText(t *testing.T) {
	var out bytes.Buffer
	WriteUpdate(&out, app.UpdateReport{
		Checks: []app.UpdateCheck{{Source: "owner/\x1b[31mrepo\nFORGED", Err: "boom"}},
		Results: []app.UpdateResult{
			{Name: "ev\nil", Status: app.UpdateFailed, Reason: "bad\x1b[0m thing"},
			{Name: "ok", Status: app.UpdateSkipped, Reason: "several\x1b[1mmatches"},
		},
		Uncheckable: []app.UncheckableSkill{{Name: "lo\ncal", Reason: "Local\x1bpath", Source: ".\x1b/src"}},
		Notes:       []string{"note: \x1b[31mred"},
	})
	if strings.ContainsAny(out.String(), "\x1b\r") {
		t.Errorf("output carries terminal escapes: %q", out.String())
	}
	if !strings.Contains(out.String(), "ev il") {
		t.Errorf("output = %q, want the sanitized skill name", out.String())
	}
}
