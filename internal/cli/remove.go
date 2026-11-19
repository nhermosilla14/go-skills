package cli

import (
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/app"
)

// WriteRemove prints one line per removed, skipped or failed target, then the kept directories and
// lock notes, and a short summary. Progress and results belong on the human stream.
func WriteRemove(w io.Writer, rep app.RemoveReport) {
	for _, res := range rep.Results {
		line := fmt.Sprintf("%s: %s -> %s (%s)", res.Status, displayText(res.Skill),
			displayText(res.Agent), displayText(res.Path))
		if res.Message != "" {
			line += ": " + displayText(res.Message)
		}
		fmt.Fprintln(w, line)
	}
	for _, note := range rep.Notes {
		fmt.Fprintf(w, "note: %s\n", displayText(note))
	}
	removed, skipped, failed := rep.Counts()
	fmt.Fprintf(w, "Removed %d, skipped %d, failed %d\n", removed, skipped, failed)
	if rep.LockPath != "" {
		fmt.Fprintf(w, "Updated %s\n", displayText(rep.LockPath))
	}
}
