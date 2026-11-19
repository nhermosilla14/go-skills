package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// offerSource installs one discoverable skill and points the offered source at a local
// find-skills fixture, so no test reaches the network.
func offerSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeSourceFile(t, src, "alpha/SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	offered := t.TempDir()
	writeSourceFile(t, offered, "find-skills/SKILL.md",
		"---\nname: find-skills\ndescription: discover skills\n---\n")
	old := findSkillsSource
	findSkillsSource = offered
	t.Cleanup(func() { findSkillsSource = old })
	return src
}

// scriptedOffer is one prompter that answers the add flow and the one-time offer.
type scriptedOffer struct {
	*Scripted
	*OfferScripted
}

// offerAnswers are the answers of the install that precedes the offer.
func offerAnswers() Answers {
	return Answers{Scope: ScopeProject, Mode: "symlink", Proceed: true}
}

func TestFindSkillsOfferDeclinedRecordsDismissal(t *testing.T) {
	src := offerSource(t)
	env := interactiveEnv(t, t.TempDir())
	offer := &OfferScripted{}
	prompter := &scriptedOffer{Scripted: NewScripted(t, offerAnswers()), OfferScripted: offer}

	rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
		env, prompter)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Offer == nil || !rep.Offer.Declined || !rep.Offer.Dismissed {
		t.Fatalf("offer = %+v, want a recorded decline", rep.Offer)
	}
	if offer.Asked() != 1 {
		t.Errorf("offer asked %d times, want once", offer.Asked())
	}
	lock, err := lockfile.LoadGlobal(agents.GlobalLockPath(env.AgentEnv))
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if !lock.DismissedPrompt(FindSkillsPrompt) {
		t.Errorf("dismissed = %v, want findSkillsPrompt", lock.Dismissed)
	}
}

func TestFindSkillsOfferAcceptedInstallsGlobally(t *testing.T) {
	src := offerSource(t)
	env := interactiveEnv(t, t.TempDir())
	offer := &OfferScripted{Accept: true}
	prompter := &scriptedOffer{Scripted: NewScripted(t, offerAnswers()), OfferScripted: offer}

	rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
		env, prompter)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Offer == nil || !rep.Offer.Accepted || rep.Offer.Declined {
		t.Fatalf("offer = %+v, want an accepted offer", rep.Offer)
	}
	if len(rep.Offer.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", rep.Offer.Warnings)
	}
	installed := filepath.Join(env.AgentEnv.Home, ".agents", "skills", "find-skills", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("find-skills was not installed globally: %v", err)
	}
	lock, err := lockfile.LoadGlobal(agents.GlobalLockPath(env.AgentEnv))
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if _, ok := lock.Get("find-skills"); !ok {
		t.Errorf("global lock tracks %v, want find-skills", lock.Skills)
	}
}

func TestFindSkillsOfferNeverRunsNonInteractively(t *testing.T) {
	src := offerSource(t)
	env := interactiveEnv(t, t.TempDir())
	offer := &OfferScripted{}
	prompter := &scriptedOffer{Scripted: NewScripted(t), OfferScripted: offer}
	rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode"}, Yes: true},
		env, prompter)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Offer != nil {
		t.Errorf("offer = %+v, want none", rep.Offer)
	}
	if offer.Asked() != 0 {
		t.Errorf("offer asked %d times, want never", offer.Asked())
	}
}

func TestFindSkillsOfferSkipsDismissedAndListRuns(t *testing.T) {
	src := offerSource(t)
	t.Run("dismissed", func(t *testing.T) {
		env := interactiveEnv(t, t.TempDir())
		lock := lockfile.NewGlobal()
		lock.DismissPrompt(FindSkillsPrompt)
		if err := lock.Save(agents.GlobalLockPath(env.AgentEnv)); err != nil {
			t.Fatal(err)
		}
		offer := &OfferScripted{Accept: true}
		prompter := &scriptedOffer{Scripted: NewScripted(t, offerAnswers()), OfferScripted: offer}
		rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
			env, prompter)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if rep.Offer != nil || offer.Asked() != 0 {
			t.Errorf("offer = %+v, asked = %d, want none", rep.Offer, offer.Asked())
		}
	})
	t.Run("list", func(t *testing.T) {
		env := interactiveEnv(t, t.TempDir())
		offer := &OfferScripted{Accept: true}
		prompter := &scriptedOffer{Scripted: NewScripted(t), OfferScripted: offer}
		if _, err := Add(AddOptions{Source: src, List: true}, env, prompter); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if offer.Asked() != 0 {
			t.Errorf("offer asked %d times, want never", offer.Asked())
		}
	})
}

func TestFindSkillsOfferUnreadableLockIsAWarning(t *testing.T) {
	src := offerSource(t)
	env := interactiveEnv(t, t.TempDir())
	// A file where the global lock directory must go makes the lock unreadable.
	state := filepath.Join(env.AgentEnv.Home, "state")
	env.AgentEnv.Vars = map[string]string{"XDG_STATE_HOME": state}
	if err := os.WriteFile(state, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	offer := &OfferScripted{}
	prompter := &scriptedOffer{Scripted: NewScripted(t, offerAnswers()), OfferScripted: offer}

	rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
		env, prompter)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("the original install failed: %+v", rep.Results)
	}
	if rep.Offer == nil || len(rep.Offer.Warnings) != 1 ||
		!strings.Contains(rep.Offer.Warnings[0], "could not read the global lock") {
		t.Fatalf("offer = %+v, want one warning", rep.Offer)
	}
	if offer.Asked() != 0 {
		t.Errorf("offer asked %d times, want never", offer.Asked())
	}
}

func TestDeclineFindSkillsSaveFailureIsAWarning(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := lockfile.NewGlobal()
	report := declineFindSkills(filepath.Join(blocker, "lock"), lock)
	if !report.Declined || report.Dismissed {
		t.Fatalf("report = %+v, want a decline without a dismissal", report)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "dismissal") {
		t.Errorf("warnings = %v, want one about the dismissal", report.Warnings)
	}
}

func TestFindSkillsOfferPromptErrorIsAWarning(t *testing.T) {
	src := offerSource(t)
	env := interactiveEnv(t, t.TempDir())
	offer := &OfferScripted{Err: errors.New("terminal went away")}
	prompter := &scriptedOffer{Scripted: NewScripted(t, offerAnswers()), OfferScripted: offer}

	rep, err := Add(AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
		env, prompter)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("the original install failed: %+v", rep.Results)
	}
	if rep.Offer == nil || len(rep.Offer.Warnings) != 1 ||
		!strings.Contains(rep.Offer.Warnings[0], "terminal went away") {
		t.Fatalf("offer = %+v, want one warning", rep.Offer)
	}
}
