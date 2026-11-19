package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

// scriptedAdd answers the add flow and the one-time find-skills offer.
type scriptedAdd struct {
	*app.Scripted
	*app.OfferScripted
}

// offerEnv is an isolated process environment for the scripted offer flow.
func offerEnv(t *testing.T, project string) app.Env {
	t.Helper()
	return app.Env{
		WorkDir: project,
		IsTTY:   true,
		Now:     time.Now,
		AgentEnv: agents.Environment{
			GOOS:            runtime.GOOS,
			Home:            t.TempDir(),
			Cwd:             project,
			Vars:            map[string]string{},
			ApplicationsDir: t.TempDir(),
			EtcDir:          t.TempDir(),
		},
	}
}

// TestE2EFindSkillsOfferOnce declines the offer once and proves the dismissal is recorded and the
// question is never asked again. It runs the whole use case against an isolated home.
func TestE2EFindSkillsOfferOnce(t *testing.T) {
	src := t.TempDir()
	writeSkill(t, src, "alpha")
	env := offerEnv(t, t.TempDir())
	answers := app.Answers{Scope: app.ScopeProject, Mode: install.ModeSymlink, Proceed: true}

	offer := &app.OfferScripted{}
	prompter := &scriptedAdd{Scripted: app.NewScripted(t, answers), OfferScripted: offer}
	rep, err := app.Add(app.AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
		env, prompter)
	if err != nil {
		t.Fatalf("first add: %v", err)
	}
	if rep.Failed() {
		t.Fatalf("first add failed: %+v", rep.Results)
	}
	if rep.Offer == nil || !rep.Offer.Declined || !rep.Offer.Dismissed {
		t.Fatalf("offer = %+v, want a recorded decline", rep.Offer)
	}
	lock, err := lockfile.LoadGlobal(agents.GlobalLockPath(env.AgentEnv))
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if !lock.DismissedPrompt(app.FindSkillsPrompt) {
		t.Fatalf("dismissed = %v, want findSkillsPrompt true", lock.Dismissed)
	}

	second := &scriptedAdd{Scripted: app.NewScripted(t, answers), OfferScripted: &app.OfferScripted{}}
	rep, err = app.Add(app.AddOptions{Source: src, Agents: []string{"opencode"}, Skills: []string{"alpha"}},
		env, second)
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	if rep.Offer != nil {
		t.Fatalf("second add asked the offer again: %+v", rep.Offer)
	}
	if got := second.OfferScripted.Asked(); got != 0 {
		t.Errorf("offer asked %d times after dismissal, want never", got)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, lockfile.FileName)); err != nil {
		t.Errorf("the project lock is missing: %v", err)
	}
}
