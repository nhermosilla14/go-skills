package tui

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/search"
)

// typeText sends one keystroke message carrying the whole string.
func typeText(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

// fakeSearch records queries and returns a fixed plan.
type fakeSearch struct {
	queries []string
	results map[string][]search.Result
	err     error
}

func (f *fakeSearch) run(query string) ([]search.Result, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}
	return f.results[query], nil
}

func newTestSearch(run SearchFunc) *searchPrompt {
	m := newSearchPrompt(newTheme(io.Discard, false, true), run)
	m.resize(80, 24)
	return m
}

// searchNow delivers the debounce for the scheduled keystroke, runs the search it triggers and
// applies the results.
func searchNow(t *testing.T, m *searchPrompt, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("the keystroke scheduled no debounce")
	}
	_, cmd = m.Update(searchDebounceMsg{seq: m.seq})
	if cmd == nil {
		t.Fatal("the debounce scheduled no search")
	}
	msg, ok := cmd().(searchDoneMsg)
	if !ok {
		t.Fatalf("search command returned %T", cmd())
	}
	m.Update(msg)
}

func TestSearchPromptRequiresTwoCharacters(t *testing.T) {
	fake := &fakeSearch{}
	m := newTestSearch(fake.run)
	_, cmd := m.Update(typeText("r"))
	if cmd != nil {
		t.Fatal("a one-character query scheduled a search")
	}
	m.Update(searchDebounceMsg{seq: m.seq})
	if len(fake.queries) != 0 {
		t.Fatalf("queries = %v, want none", fake.queries)
	}
	if !strings.Contains(m.View(), "Start typing to search (min 2 chars)") {
		t.Errorf("view misses the hint:\n%s", m.View())
	}
}

func TestSearchPromptTypesAndShowsResults(t *testing.T) {
	fake := &fakeSearch{results: map[string][]search.Result{
		"react": {{ID: "o/r/react", Name: "react", Source: "o/r", Installs: 1200}},
	}}
	m := newTestSearch(fake.run)
	_, cmd := m.Update(typeText("react"))
	searchNow(t, m, cmd)
	if len(fake.queries) != 1 || fake.queries[0] != "react" {
		t.Fatalf("queries = %v, want [react]", fake.queries)
	}
	view := m.View()
	for _, want := range []string{"Search skills: react", "o/r@react", "1.2K installs"} {
		if !strings.Contains(view, want) {
			t.Errorf("view misses %q:\n%s", want, view)
		}
	}
}

func TestSearchPromptSelection(t *testing.T) {
	fake := &fakeSearch{results: map[string][]search.Result{
		"re": {
			{ID: "o/r/one", Name: "one", Source: "o/r"},
			{ID: "o/r/two", Name: "two", Source: "o/r"},
		},
	}}
	m := newTestSearch(fake.run)
	_, cmd := m.Update(typeText("re"))
	searchNow(t, m, cmd)
	m.Update(down())
	_, cmd = m.Update(enter())
	if cmd == nil {
		t.Fatal("selecting a result did not quit the prompt")
	}
	if !m.done || m.cancelled {
		t.Fatalf("done = %v, cancelled = %v", m.done, m.cancelled)
	}
	if m.selected.ID != "o/r/two" {
		t.Errorf("selected = %+v, want the second result", m.selected)
	}
}

func TestSearchPromptCancelAndEmptyEnter(t *testing.T) {
	m := newTestSearch(func(string) ([]search.Result, error) { return nil, nil })
	m.Update(typeText("re"))
	if _, cmd := m.Update(enter()); cmd != nil {
		t.Fatal("enter without results quit the prompt")
	}
	m.Update(escape())
	if !m.cancelled || !m.done {
		t.Fatalf("cancelled = %v, done = %v", m.cancelled, m.done)
	}
}

func TestSearchPromptShowsFailure(t *testing.T) {
	fake := &fakeSearch{err: errors.New("skills.sh returned HTTP 500")}
	m := newTestSearch(fake.run)
	_, cmd := m.Update(typeText("re"))
	searchNow(t, m, cmd)
	if !strings.Contains(m.View(), "Search failed: skills.sh returned HTTP 500") {
		t.Errorf("view misses the failure:\n%s", m.View())
	}
}

func TestSearchPromptIgnoresStaleResults(t *testing.T) {
	fake := &fakeSearch{results: map[string][]search.Result{
		"react": {{ID: "o/r/react", Name: "react", Source: "o/r"}},
	}}
	m := newTestSearch(fake.run)
	m.Update(typeText("rea"))
	_, cmd := m.Update(searchDebounceMsg{seq: m.seq})
	stale := cmd()
	m.Update(typeText("ct"))
	m.Update(stale)
	if len(m.results) != 0 {
		t.Fatalf("results = %+v, want a stale response to be dropped", m.results)
	}
}

func TestRunSearchCancel(t *testing.T) {
	var out strings.Builder
	_, found, err := RunSearch(strings.NewReader("\x1b"), &out, Options{}, func(string) ([]search.Result, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("RunSearch: %v", err)
	}
	if found {
		t.Fatal("a cancelled search reported a result")
	}
	if !strings.Contains(out.String(), "Search skills:") {
		t.Errorf("output misses the prompt: %q", out.String())
	}
}

func TestRunSearchNeedsAFunction(t *testing.T) {
	if _, _, err := RunSearch(strings.NewReader("\x1b"), io.Discard, Options{}, nil); err == nil {
		t.Fatal("RunSearch without a search function succeeded")
	}
}
