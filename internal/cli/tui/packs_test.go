package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/app"
)

// notionPacks are the choices the pack picker presents.
func notionPacks() []app.NotionPackChoice {
	return []app.NotionPackChoice{
		{ID: "pack-1", Name: "Alpha Pack", Description: "First pack"},
		{ID: "pack-2", Name: "Beta Pack", Description: "Second pack"},
	}
}

func TestPacksPromptListsEveryPackByName(t *testing.T) {
	p := newPacksPrompt(newTheme(io.Discard, false, true), "Mining Team", notionPacks())
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := p.View()
	for _, want := range []string{"Select Notion packs to install", "Mining Team", "Alpha Pack", "Beta Pack"} {
		if !strings.Contains(view, want) {
			t.Errorf("view misses %q:\n%s", want, view)
		}
	}
}

func TestPacksPromptReturnsCheckedPacks(t *testing.T) {
	p := newPacksPrompt(newTheme(io.Discard, false, true), "", notionPacks())
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Every pack starts checked; untoggle the second so only the first is answered.
	for _, msg := range []tea.Msg{down(), runeKey(' '), enter()} {
		_, _ = p.Update(msg)
	}
	if p.cancelled || !p.done {
		t.Fatalf("prompt = %+v, want a finished answer", p)
	}
	if got := p.selected(); len(got) != 1 || got[0] != "pack-1" {
		t.Errorf("selected = %v, want pack-1", got)
	}
}

func TestPacksPromptCancel(t *testing.T) {
	p := newPacksPrompt(newTheme(io.Discard, false, true), "", notionPacks())
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p.Update(ctrlC())
	if !p.cancelled || !p.done {
		t.Errorf("prompt = %+v, want a cancellation", p)
	}
}
