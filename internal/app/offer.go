package app

import (
	"errors"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// FindSkillsPrompt is the dismissal key of the one-time find-skills question in the global lock.
const FindSkillsPrompt = "findSkillsPrompt"

// findSkillsSource is the source of the offered skill. Tests point it at a local fixture so no test
// reaches the network.
var findSkillsSource = "vercel-labs/skills"

// OfferPrompter asks the one-time find-skills question after an install has finished. It is a
// separate interface because a prompt session is already spent when the add flow ends.
type OfferPrompter interface {
	AskFindSkillsOffer() (bool, error)
}

// OfferReport is the outcome of the one-time find-skills suggestion.
type OfferReport struct {
	Accepted  bool             // the user accepted the offer
	Declined  bool             // the user declined or cancelled
	Dismissed bool             // the dismissal flag was recorded
	Results   []install.Result // the find-skills install, when accepted
	Warnings  []string         // failures of the offered install, never of the original one
}

// offerFindSkills asks the question once and, when accepted, installs find-skills for the same
// agents as the install that just finished. It never fails the original install: every problem
// becomes a warning.
func offerFindSkills(env Env, agentEnv agents.Environment, targets []string, prompter OfferPrompter) *OfferReport {
	ids := withoutAgent(targets, "replit")
	if len(ids) == 0 {
		return nil
	}
	path := agents.GlobalLockPath(agentEnv)
	lock, err := lockfile.LoadGlobal(path)
	if err != nil {
		return &OfferReport{Warnings: []string{
			"could not read the global lock, so the find-skills offer was skipped: " + err.Error(),
		}}
	}
	if lock.DismissedPrompt(FindSkillsPrompt) {
		return nil
	}
	accepted, err := prompter.AskFindSkillsOffer()
	if err != nil {
		if errors.Is(err, ErrCancelled) {
			return declineFindSkills(path, lock)
		}
		return &OfferReport{Warnings: []string{"the find-skills prompt failed: " + err.Error()}}
	}
	if !accepted {
		return declineFindSkills(path, lock)
	}
	report, err := Add(AddOptions{
		Source: findSkillsSource,
		Skills: []string{"find-skills"},
		Agents: ids,
		Yes:    true,
		Global: true,
	}, env)
	out := &OfferReport{Accepted: true, Results: report.Results}
	if err != nil {
		out.Warnings = append(out.Warnings, "could not install find-skills: "+err.Error())
		return out
	}
	out.Warnings = append(out.Warnings, report.Warnings...)
	if report.Failed() {
		out.Warnings = append(out.Warnings,
			"find-skills did not install for every agent; install it with 'skills add "+
				findSkillsSource+"@find-skills -g'")
	}
	return out
}

// declineFindSkills records the dismissal and reports whether it was written.
func declineFindSkills(path string, lock *lockfile.GlobalFile) *OfferReport {
	lock.DismissPrompt(FindSkillsPrompt)
	out := &OfferReport{Declined: true, Dismissed: true}
	if err := lock.Save(path); err != nil {
		out.Dismissed = false
		out.Warnings = append(out.Warnings,
			"could not record the find-skills dismissal: "+err.Error())
	}
	return out
}

// withoutAgent returns the identifiers except the named one.
func withoutAgent(ids []string, skip string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != skip {
			out = append(out, id)
		}
	}
	return out
}

// offerWanted reports whether a successful run may ask the find-skills question: interactive (a
// prompter that supports the offer), not -y, --list or --json, and nothing failed.
func offerWanted(opts AddOptions, prompter Prompter, rep Report) (OfferPrompter, bool) {
	if !promptsWanted(opts) || rep.Failed() || prompter == nil {
		return nil, false
	}
	offer, ok := prompter.(OfferPrompter)
	return offer, ok
}
