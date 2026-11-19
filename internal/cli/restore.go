package cli

import (
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/app"
)

// WriteRestore prints the restore run: the entries this build cannot rebuild, one line per source
// that failed, one line per skill, and the summary. Progress and results belong on the human stream.
func WriteRestore(w io.Writer, rep app.RestoreReport) {
	for _, entry := range rep.Unrestorable {
		fmt.Fprintf(w, "Skipped %s: %s\n", displayText(entry.Name), displayText(entry.Reason))
	}
	for _, check := range rep.Checks {
		if check.Err != "" {
			fmt.Fprintf(w, "✗ Failed to restore from %s: %s\n",
				displayText(check.Source), displayText(check.Err))
		}
	}
	restored, relinked, skipped := 0, 0, len(rep.Unrestorable)
	for _, res := range rep.Results {
		switch res.Status {
		case app.RestoreRestored:
			restored++
			if rep.DryRun {
				fmt.Fprintf(w, "would restore %s\n", displayText(res.Name))
				continue
			}
			fmt.Fprintf(w, "✓ Restored %s\n", displayText(res.Name))
		case app.RestoreRelinked:
			relinked++
			if rep.DryRun {
				fmt.Fprintf(w, "would relink %s\n", displayText(res.Name))
				continue
			}
			fmt.Fprintf(w, "✓ Relinked %s\n", displayText(res.Name))
		case app.RestoreSkipped:
			skipped++
			if rep.DryRun {
				fmt.Fprintf(w, "would skip %s\n", displayText(res.Name))
				continue
			}
			fmt.Fprintf(w, "Skipped %s", displayText(res.Name))
			if res.Reason != "" {
				fmt.Fprintf(w, ": %s", displayText(res.Reason))
			}
			fmt.Fprintln(w)
		case app.RestoreFailed:
			fmt.Fprintf(w, "✗ Failed to restore %s: %s\n",
				displayText(res.Name), displayText(res.Reason))
		}
	}
	fmt.Fprintf(w, "Restored %d, relinked %d, skipped %d, failed %d\n",
		restored, relinked, skipped, rep.FailedCount())
	if rep.LockPath != "" {
		fmt.Fprintf(w, "Updated %s\n", displayText(rep.LockPath))
	}
}
