package cli

import (
	"fmt"
	"io"

	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/search"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// findBaseURL is the site whose pages the results link to.
const findBaseURL = "https://skills.sh"

// WriteFindResults prints the keyword search report: one install block per result, with the
// documented divider when partial matches follow the exact ones.
func WriteFindResults(w io.Writer, query, owner string, res search.Response) {
	if len(res.Results) == 0 {
		suffix := ""
		if owner != "" {
			suffix = fmt.Sprintf(" from owner %q", displayText(owner))
		}
		fmt.Fprintf(w, "No skills found for %q%s\n", displayText(query), suffix)
		return
	}
	for i, result := range res.Results {
		if i == res.Exact && res.Exact < len(res.Results) {
			if res.Exact == 0 {
				fmt.Fprintf(w, "No skills matched every term in %q. Partial matches:\n",
					displayText(query))
			} else {
				fmt.Fprintf(w, "Partial matches (some terms of %q):\n", displayText(query))
			}
			fmt.Fprintln(w)
		}
		WriteFindResult(w, result)
		fmt.Fprintln(w)
	}
}

// WriteFindResult prints one result: the install command, the source reference with its install
// badge, and the skills.sh page.
func WriteFindResult(w io.Writer, result search.Result) {
	pkg := result.Source
	if pkg == "" {
		pkg = result.ID
	}
	ref := displayText(pkg) + "@" + displayText(result.Name)
	fmt.Fprintf(w, "Install with npx skills add %s\n", ref)
	if badge := search.FormatInstalls(result.Installs); badge != "" {
		fmt.Fprintf(w, "%s %s\n", ref, badge)
	} else {
		fmt.Fprintln(w, ref)
	}
	fmt.Fprintf(w, "└ %s/%s\n", findBaseURL, displayText(result.ID))
}

// WriteFindTip prints the guidance a run without a query and without a terminal gets.
func WriteFindTip(w io.Writer) {
	fmt.Fprint(w, `Tip: if running in a coding agent, follow these steps:
  1) npx skills find [query] [--owner <owner>]
  2) npx skills add <owner/repo@skill>

Usage: npx skills find <query> [--owner <owner>]
`)
}

// WriteFindOffer prints the outcome of the one-time find-skills offer: the manual install command
// when it was declined, and the warnings of an install that did not fully succeed.
func WriteFindOffer(w io.Writer, offer *app.OfferReport) {
	if offer == nil {
		return
	}
	if offer.Declined {
		fmt.Fprintln(w, "You can install it later with: npx skills add vercel-labs/skills@find-skills")
	}
	for _, warning := range offer.Warnings {
		fmt.Fprintf(w, "warning: %s\n", skill.SanitizeDisplay(warning))
	}
}
