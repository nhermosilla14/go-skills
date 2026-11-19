package tui

import (
	"io"
	"strings"
	"testing"
)

func newTestOffer() *offerPrompt {
	o := newOfferPrompt(newTheme(io.Discard, false, true))
	o.confirm.resize(80, 24)
	return o
}

func TestOfferPromptShowsTheDocumentedText(t *testing.T) {
	view := newTestOffer().View()
	for _, want := range []string{
		"Install the find-skills skill? It helps your agent discover and suggest skills.",
		"One-time prompt - you won't be asked again if you dismiss.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("view misses %q:\n%s", want, view)
		}
	}
}

func TestOfferPromptAnswers(t *testing.T) {
	accepted := newTestOffer()
	if _, cmd := accepted.Update(enter()); cmd == nil {
		t.Fatal("accepting the offer did not quit the prompt")
	}
	if !accepted.accepted || !accepted.done || accepted.cancelled {
		t.Errorf("accepted = %+v", accepted)
	}

	declined := newTestOffer()
	declined.Update(runeKey('n'))
	if declined.accepted || !declined.done || declined.cancelled {
		t.Errorf("declined = %+v", declined)
	}

	cancelled := newTestOffer()
	cancelled.Update(ctrlC())
	if !cancelled.cancelled || !cancelled.done {
		t.Errorf("cancelled = %+v", cancelled)
	}
}
