package cli

import (
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/app"
)

// WriteUpdate prints the update run: the header, one line per source check, one line per skill
// that changed or was left alone, the entries that cannot be checked, and the summary. A run that
// updates both scopes prints one section per scope under its own header.
func WriteUpdate(w io.Writer, rep app.UpdateReport) {
	fmt.Fprintln(w, "Checking for skill updates…")
	if len(rep.Phases) > 0 {
		for _, phase := range rep.Phases {
			fmt.Fprintln(w)
			fmt.Fprintln(w, scopeHeader(phase.Scope))
			fmt.Fprintln(w)
			writeUpdatePhase(w, phase)
		}
		return
	}
	writeUpdatePhase(w, rep)
}

// scopeHeader is the section title of one phase of a both-scope run.
func scopeHeader(scope string) string {
	if scope == string(app.ScopeGlobal) {
		return "Global Skills"
	}
	return "Project Skills"
}

// writeUpdatePhase prints the checks, results, notes and summary of one scope.
func writeUpdatePhase(w io.Writer, rep app.UpdateReport) {
	for _, check := range rep.Checks {
		if check.Err != "" {
			fmt.Fprintf(w, "✗ Failed to check skills from %s\n", displayText(check.Source))
			continue
		}
		fmt.Fprintf(w, "Checking skills from source: %s\n", displayText(check.Source))
	}
	updated, failed := 0, 0
	for _, res := range rep.Results {
		switch res.Status {
		case app.UpdateUpdated:
			updated++
			fmt.Fprintf(w, "Updating %s…\n", displayText(res.Name))
			fmt.Fprintf(w, "✓ Updated %s\n", displayText(res.Name))
		case app.UpdateFailed:
			failed++
			fmt.Fprintf(w, "✗ Failed to update %s: %s\n", displayText(res.Name), displayText(res.Reason))
		case app.UpdateSkipped:
			fmt.Fprintf(w, "Skipped %s: %s\n", displayText(res.Name), displayText(res.Reason))
		}
	}
	for _, note := range rep.Notes {
		fmt.Fprintln(w, displayText(note))
	}
	writeUncheckable(w, rep.Uncheckable, rep.Scope == string(app.ScopeGlobal))
	if updated > 0 {
		fmt.Fprintf(w, "Updated %d skill(s)\n", updated)
	}
	if failed > 0 {
		fmt.Fprintf(w, "Failed to update %d skill(s)\n", failed)
	}
	if !rep.NothingToUpdate && !rep.Changed() && !rep.Skipped() && !rep.Failed() {
		if rep.Scope == string(app.ScopeGlobal) {
			fmt.Fprintln(w, "✓ All global skills are up to date")
			return
		}
		fmt.Fprintln(w, "✓ All project skills are up to date")
	}
}

// writeUncheckable prints the entries that cannot be checked automatically, grouped by the source
// whose reinstall command refreshes them.
func writeUncheckable(w io.Writer, entries []app.UncheckableSkill, global bool) {
	if len(entries) == 0 {
		return
	}
	fmt.Fprintf(w, "%d skill(s) cannot be checked automatically:\n", len(entries))
	var sources []string
	seen := map[string]bool{}
	for _, entry := range entries {
		fmt.Fprintf(w, "  %s  %s\n", displayText(entry.Name), displayText(entry.Reason))
		if entry.Source == "" || seen[entry.Source] {
			continue
		}
		seen[entry.Source] = true
		sources = append(sources, entry.Source)
	}
	scope := ""
	if global {
		scope = " -g"
	}
	for _, source := range sources {
		fmt.Fprintf(w, "To update: skills add %s%s -y\n", displayText(source), scope)
	}
}
