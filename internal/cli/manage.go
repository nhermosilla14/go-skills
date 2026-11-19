package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// maxListedAgents is how many agent names the human list shows before collapsing the rest.
const maxListedAgents = 5

// ListEntry is one skill of the `list --json` document. Absent lock fields are null.
type ListEntry struct {
	Name        string   `json:"name"`
	Path        *string  `json:"path"`
	Scope       string   `json:"scope"`
	Agents      []string `json:"agents"`
	Subagents   []string `json:"subagents,omitempty"`
	Source      *string  `json:"source"`
	SourceURL   *string  `json:"sourceUrl"`
	SourceType  *string  `json:"sourceType"`
	RenamedFrom *string  `json:"renamedFrom"`
	Status      string   `json:"status"`
}

// ListJSON turns a list report into the machine-readable shape. Agents stay untruncated.
func ListJSON(rep app.ListReport) []ListEntry {
	entries := make([]ListEntry, 0, len(rep.Skills))
	for _, s := range rep.Skills {
		entry := ListEntry{
			Name:        s.Name,
			Scope:       rep.Scope,
			Agents:      append([]string{}, s.Agents...),
			Subagents:   append([]string{}, s.Subagents...),
			Source:      optionalText(s.Source),
			SourceURL:   optionalText(s.SourceURL),
			SourceType:  optionalText(s.SourceType),
			RenamedFrom: optionalText(s.RenamedFrom),
			Status:      string(s.Status),
		}
		entry.Path = optionalText(s.Path)
		entries = append(entries, entry)
	}
	return entries
}

// WriteListJSON writes the document once, as one pretty-printed array followed by a newline.
func WriteListJSON(w io.Writer, entries []ListEntry) error {
	if entries == nil {
		entries = []ListEntry{}
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the JSON document: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", raw)
	return err
}

// WriteList prints the human listing: one aligned name/path line per skill followed by its agents
// and lock source, then the lock entries that have no directory on disk.
func WriteList(w io.Writer, rep app.ListReport) {
	header, empty, hint := "Project Skills", "No project skills found.", "Try listing global skills with -g"
	if rep.Scope == string(app.ScopeGlobal) {
		header, empty, hint = "Global Skills", "No global skills found.", "Try listing project skills without -g"
	}
	fmt.Fprintln(w, header)
	fmt.Fprintln(w)

	var shown, missing []app.SkillInfo
	for _, s := range rep.Skills {
		if s.Status == app.StatusMissing {
			missing = append(missing, s)
			continue
		}
		shown = append(shown, s)
	}
	if len(shown) == 0 && len(missing) == 0 {
		fmt.Fprintln(w, empty)
		fmt.Fprintln(w, hint)
		return
	}

	width := 0
	for _, s := range shown {
		if n := utf8.RuneCountInString(displayText(s.Name)); n > width {
			width = n
		}
	}
	for _, s := range shown {
		fmt.Fprintf(w, "%s %s\n", padText(displayText(s.Name), width), displayText(s.Path))
		fmt.Fprintf(w, "  Agents: %s  Source: %s\n", agentList(s.Agents, s.Subagents), sourceLine(s))
	}
	if len(missing) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Missing (in the lock file, not on disk)")
	for _, s := range missing {
		fmt.Fprintf(w, "  %s  Source: %s\n", displayText(s.Name), sourceLine(s))
	}
}

// WriteCheck prints the check report: updates, local changes and check failures, or the clean
// summary when there is nothing to report.
func WriteCheck(w io.Writer, rep app.CheckReport) {
	for _, name := range rep.Updates {
		fmt.Fprintf(w, "↑ %s has an update\n", displayText(name))
	}
	for _, name := range rep.Modified {
		fmt.Fprintf(w, "✎ %s has local changes\n", displayText(name))
	}
	for _, failure := range rep.Failures {
		fmt.Fprintf(w, "✗ %s\n", displayText(failure))
	}
	if !rep.Failed() {
		fmt.Fprintf(w, "✓ All %s skills are up to date\n", checkScope(rep.Scope))
	}
}

// checkScope is the scope word of the check summary.
func checkScope(scope string) string {
	if scope == string(app.ScopeGlobal) {
		return "global"
	}
	return "project"
}

// agentList renders the agent names, collapsing the tail when there are too many. No agents means
// the skill is not linked to any agent directory. An Eve placement adds the root agent and the
// subagents the skill was installed for.
func agentList(names, subagents []string) string {
	if len(names) == 0 {
		return "not linked"
	}
	suffix := ""
	shown := names
	if len(shown) > maxListedAgents {
		suffix = fmt.Sprintf(" +%d more", len(shown)-maxListedAgents)
		shown = shown[:maxListedAgents]
	}
	clean := make([]string, len(shown))
	for i, name := range shown {
		clean[i] = displayText(name)
	}
	out := strings.Join(clean, ", ") + suffix
	if len(subagents) > 0 {
		out += " (" + strings.Join(subagentLabels(subagents), ", ") + ")"
	}
	return out
}

// subagentLabels names an Eve placement: the root agent and the selected subagents.
func subagentLabels(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		if name == "" {
			out[i] = "root"
			continue
		}
		out[i] = displayText(name)
	}
	return out
}

// sourceLine renders the lock source of a skill with its rename and modification notes.
func sourceLine(s app.SkillInfo) string {
	source := displayText(s.Source)
	if s.Status == app.StatusUntracked {
		source = "untracked"
	}
	if s.RenamedFrom != "" {
		source += " (renamed from " + displayText(s.RenamedFrom) + ")"
	}
	if s.Status == app.StatusModified {
		source += " modified (differs from the lock)"
	}
	return source
}

// displayText makes untrusted text safe to print: a name, path or source can come from a lock file.
func displayText(s string) string {
	return skill.SanitizeDisplay(s)
}

// padText appends spaces so s occupies width runes.
func padText(s string, width int) string {
	if missing := width - utf8.RuneCountInString(s); missing > 0 {
		return s + strings.Repeat(" ", missing)
	}
	return s
}

// optionalText returns nil for an absent value, so JSON writes null.
func optionalText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
