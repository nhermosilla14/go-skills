package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/install"
)

// Entry is one skill of the --json document. The schema carries only what this tool can know: the
// upstream security and blob-fallback fields are not emitted.
type Entry struct {
	Name   string   `json:"name,omitempty"`
	Status string   `json:"status"`
	Source string   `json:"source,omitempty"`
	Ref    string   `json:"ref,omitempty"`
	Hash   string   `json:"hash,omitempty"`
	Path   string   `json:"path,omitempty"`
	Scope  string   `json:"scope,omitempty"`
	Agents []string `json:"agents,omitempty"`
	Mode   string   `json:"mode,omitempty"`
	Reason string   `json:"reason,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// ValidateJSON rejects the documented --json combinations. The messages are the upstream ones.
func ValidateJSON(opts app.AddOptions) error {
	if !opts.JSON {
		return nil
	}
	if !opts.Yes {
		return diagnostic("The --json flag requires --yes (or --all) to run non-interactively.")
	}
	if opts.List {
		return diagnostic("The --json flag cannot be combined with --list.")
	}
	return nil
}

// messageError is a diagnostic rendered verbatim, punctuation included, because the exit
// contract quotes it.
type messageError struct {
	msg string
}

func (e messageError) Error() string { return e.msg }

// diagnostic wraps a documented message the linters would otherwise rewrite.
func diagnostic(msg string) error { return messageError{msg: msg} }

// JSONRequested reports whether the raw arguments ask for --json, so a parse failure can still
// emit the empty document.
func JSONRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--json" {
			return true
		}
	}
	return false
}

// JSONEntries turns a finished report into one entry per selected skill, in install order.
func JSONEntries(rep app.Report) []Entry {
	names := map[string]string{}
	for _, a := range agents.All() {
		names[a.ID] = a.DisplayName
	}
	entries := []Entry{}
	index := map[string]int{}
	installed := map[int]bool{}
	failed := map[int]bool{}
	for _, r := range rep.Results {
		i, ok := index[r.Skill]
		if !ok {
			index[r.Skill] = len(entries)
			entries = append(entries, Entry{
				Name:   r.Skill,
				Source: rep.Source,
				Ref:    rep.Ref,
				Hash:   rep.Hashes[r.Skill],
				Scope:  rep.Scope,
			})
			i = len(entries) - 1
		}
		entry := &entries[i]
		switch r.Status {
		case install.Installed:
			installed[i] = true
			if entry.Path == "" {
				entry.Path = r.Path
			}
			if entry.Mode == "" {
				entry.Mode = string(r.Mode)
			}
			if name, ok := names[r.Agent]; ok {
				entry.Agents = append(entry.Agents, name)
			} else {
				entry.Agents = append(entry.Agents, r.Agent)
			}
		case install.Skipped:
			entry.Reason = joinMessages(entry.Reason, r.Message)
		default:
			failed[i] = true
			entry.Error = joinMessages(entry.Error, r.Message)
		}
	}
	// A failed target wins over an installed one, and one installed target makes the entry
	// installed even when other agents were skipped.
	for i := range entries {
		switch {
		case failed[i]:
			entries[i].Status = string(install.Failed)
			entries[i].Reason = ""
		case installed[i]:
			entries[i].Status = string(install.Installed)
			entries[i].Reason = ""
		default:
			entries[i].Status = string(install.Skipped)
		}
	}
	return entries
}

// JSONFailure is the document of a run that could not start or finish: one failed entry.
func JSONFailure(err error) []Entry {
	if err == nil {
		return []Entry{}
	}
	return []Entry{{Status: string(install.Failed), Error: Message(err)}}
}

// HasSkipped reports whether any entry was skipped, which makes the JSON run fail.
func HasSkipped(entries []Entry) bool {
	for _, e := range entries {
		if e.Status == string(install.Skipped) {
			return true
		}
	}
	return false
}

// WriteJSON writes the document once, as a single array followed by a newline. A nil slice is
// written as an empty array, never as null.
func WriteJSON(w io.Writer, entries []Entry) error {
	if entries == nil {
		entries = []Entry{}
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the JSON document: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", raw)
	return err
}

// joinMessages appends message to existing unless it is empty, keeping the order results arrived.
func joinMessages(existing, message string) string {
	if message == "" {
		return existing
	}
	if existing == "" {
		return message
	}
	return existing + "; " + message
}
