package tui

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/app"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// testQuestions is a flow with every prompt and a mix of grouped and ungrouped skills.
func testQuestions() app.Questions {
	return app.Questions{
		Steps: []app.StepID{
			app.StepSkills, app.StepAgents, app.StepScope, app.StepMethod, app.StepConfirm,
		},
		Source: "owner/repo",
		Skills: []app.SkillChoice{
			{Name: "alpha-skill", Description: "First skill of the alpha plugin", Group: "alpha", Path: "skills/alpha/SKILL.md"},
			{Name: "beta-skill", Description: "Second skill of the alpha plugin", Group: "alpha", Path: "skills/beta/SKILL.md"},
			{Name: "general-skill", Description: "Ungrouped skill in the General group", Group: "General", Path: "skills/general/SKILL.md"},
		},
		Agents: []app.AgentChoice{
			{ID: "claude-code", Name: "Claude Code", Dir: ".claude/skills"},
			{ID: "codex", Name: "Codex", Dir: ".agents/skills", Universal: true},
			{ID: "cursor", Name: "Cursor", Dir: ".agents/skills", Universal: true},
			{ID: "deepagents", Name: "Deep Agents", Dir: ".agents/skills", Universal: true, Hidden: true},
			{ID: "grok", Name: "Grok Build", Dir: ".grok/skills", Detected: true},
			{ID: "opencode", Name: "OpenCode", Dir: ".agents/skills", Universal: true},
		},
		Defaults: app.Answers{Scope: app.ScopeProject, Agents: []string{"codex", "cursor", "deepagents", "opencode"}},
	}
}

func newTestFlow(t *testing.T) *flow {
	t.Helper()
	f := newFlow(testQuestions(), newTheme(io.Discard, false, true))
	f.resize(80, 24)
	return f
}

func enter() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyEnter} }
func escape() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }
func ctrlC() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyCtrlC} }
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}
func down() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyDown} }

func send(t *testing.T, f *flow, msgs ...tea.Msg) {
	t.Helper()
	for _, msg := range msgs {
		if _, cmd := f.Update(msg); cmd != nil {
			// The flow only returns tea.Quit here; running it keeps the model consistent.
			cmd()
		}
	}
}

// advanceTo finishes every step before index.
func advanceTo(t *testing.T, f *flow, index int) {
	t.Helper()
	for f.index < index {
		if f.current == nil {
			t.Fatal("flow finished early")
		}
		switch f.current.(type) {
		case *selector:
			send(t, f, runeKey('a'), enter())
		case *confirm:
			send(t, f, enter())
		default:
			send(t, f, enter())
		}
	}
	if f.current == nil {
		t.Fatalf("flow finished before step %d", index)
	}
}

func TestFlowAnswersAccumulate(t *testing.T) {
	f := newTestFlow(t)
	if got := f.steps[0]; got != app.StepSkills {
		t.Fatalf("first step = %v, want skills", got)
	}
	send(t, f, down(), runeKey(' ')) // cursor on the first skill row, select it
	send(t, f, enter())              // finish skills
	if got := f.answers.Skills; len(got) != 1 || got[0] != "alpha-skill" {
		t.Fatalf("skills = %v, want [alpha-skill]", got)
	}
	send(t, f, enter()) // agents: the locked universal agents are already selected
	send(t, f, enter()) // scope: project
	send(t, f, enter()) // method: symlink
	send(t, f, enter()) // proceed
	if !f.done || f.cancelled {
		t.Fatalf("done = %v, cancelled = %v, want a finished flow", f.done, f.cancelled)
	}
	if f.answers.Scope != app.ScopeProject || !f.answers.Proceed || f.answers.Mode != "symlink" {
		t.Fatalf("answers = %+v", f.answers)
	}
	want := []string{"codex", "cursor", "grok", "opencode"}
	if strings.Join(f.answers.Agents, ",") != strings.Join(want, ",") {
		t.Fatalf("agents = %v, want %v", f.answers.Agents, want)
	}
}

func TestFlowCancelAtEveryStep(t *testing.T) {
	for index := range testQuestions().Steps {
		t.Run(string(testQuestions().Steps[index]), func(t *testing.T) {
			f := newTestFlow(t)
			advanceTo(t, f, index)
			send(t, f, ctrlC())
			if !f.cancelled || !f.done {
				t.Fatalf("cancel at step %d: cancelled = %v, done = %v", index, f.cancelled, f.done)
			}
		})
	}
}

func TestFlowEscapeCancelsAndConfirmNoCancels(t *testing.T) {
	f := newTestFlow(t)
	advanceTo(t, f, 4)
	send(t, f, escape())
	if !f.cancelled {
		t.Fatal("escape did not cancel the flow")
	}

	f = newTestFlow(t)
	advanceTo(t, f, 4)
	send(t, f, runeKey('n'))
	if !f.cancelled {
		t.Fatal("declining the confirmation did not cancel the flow")
	}
}

func TestFlowSkillsRequireOneSelection(t *testing.T) {
	f := newTestFlow(t)
	send(t, f, enter())
	if f.index != 0 {
		t.Fatal("the skills step accepted an empty selection")
	}
	if f.answers.Skills != nil {
		t.Fatalf("skills = %v, want none yet", f.answers.Skills)
	}
}

func TestFlowSelectAllTogglesEverything(t *testing.T) {
	f := newTestFlow(t)
	send(t, f, runeKey('a'))
	sel, ok := f.current.(*selector)
	if !ok {
		t.Fatal("current step is not the selector")
	}
	if got := len(sel.selectedIDs()); got != len(sel.items) {
		t.Fatalf("selected %d of %d after select all", got, len(sel.items))
	}
	send(t, f, runeKey('a'))
	if got := len(sel.selectedIDs()); got != 0 {
		t.Fatalf("selected %d after the second select all, want 0", got)
	}
}

func TestFlowEveAcceptedSkipsAgentPicker(t *testing.T) {
	q := testQuestions()
	q.Steps = []app.StepID{app.StepSkills, app.StepEve, app.StepAgents, app.StepConfirm}
	f := newFlow(q, newTheme(io.Discard, false, true))
	f.resize(80, 24)
	send(t, f, runeKey('a'), enter()) // skills
	send(t, f, enter())               // Eve: yes
	if len(f.answers.Agents) != 1 || f.answers.Agents[0] != "eve" {
		t.Fatalf("agents = %v, want [eve]", f.answers.Agents)
	}
	if f.steps[f.index] != app.StepConfirm {
		t.Fatalf("next step = %v, want the agent step skipped", f.steps[f.index])
	}
}

func TestFlowEveDeclinedKeepsAgentPicker(t *testing.T) {
	q := testQuestions()
	q.Steps = []app.StepID{app.StepSkills, app.StepEve, app.StepAgents, app.StepConfirm}
	f := newFlow(q, newTheme(io.Discard, false, true))
	f.resize(80, 24)
	send(t, f, runeKey('a'), enter())
	send(t, f, runeKey('n'))
	if f.steps[f.index] != app.StepAgents {
		t.Fatalf("next step = %v, want agents", f.steps[f.index])
	}
	if f.answers.Agents != nil {
		t.Fatalf("agents = %v, want none until the picker answers", f.answers.Agents)
	}
}

func TestFlowUpdateScopeAnswers(t *testing.T) {
	for cursor, want := range map[int]app.Scope{
		0: app.ScopeProject,
		1: app.ScopeGlobal,
		2: app.ScopeBoth,
	} {
		q := app.Questions{
			Steps:    []app.StepID{app.StepUpdateScope},
			Defaults: app.Answers{Scope: app.ScopeProject},
		}
		f := newFlow(q, newTheme(io.Discard, false, true))
		f.resize(80, 24)
		for i := 0; i < cursor; i++ {
			send(t, f, down())
		}
		send(t, f, enter())
		if !f.done {
			t.Fatalf("cursor %d: flow did not finish", cursor)
		}
		if f.answers.Scope != want {
			t.Errorf("cursor %d: scope = %q, want %q", cursor, f.answers.Scope, want)
		}
	}
}

func TestFlowResizeKeepsState(t *testing.T) {
	f := newTestFlow(t)
	send(t, f, runeKey('a'))
	f.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	if f.width != 40 {
		t.Fatalf("width = %d, want 40", f.width)
	}
	sel := f.current.(*selector)
	if sel.width != 40 {
		t.Fatalf("selector width = %d, want 40", sel.width)
	}
	if !strings.Contains(sel.view(), "beta-skill") {
		t.Fatal("the resized view lost its rows")
	}
	for _, width := range []int{0, 1, 20, 39, 40, 120, 1000} {
		f.Update(tea.WindowSizeMsg{Width: width, Height: 6})
		if got := f.View(); got == "" {
			t.Fatalf("empty view at width %d", width)
		}
	}
}

func TestViewSanitizesHostileText(t *testing.T) {
	q := testQuestions()
	q.Skills = []app.SkillChoice{{
		Name:        "flat",
		Description: "\x1b]0;PWNED\x07 a \x9b CSI \r tab\there " + strings.Repeat("long ", 100),
		Group:       "General",
		Path:        "skills/flat/SKILL.md",
	}}
	q.Steps = []app.StepID{app.StepSkills}
	f := newFlow(q, newTheme(io.Discard, false, true))
	f.resize(40, 24)
	send(t, f, down())
	view := f.View()
	if strings.ContainsRune(view, '\x1b') || strings.ContainsRune(view, '\x07') || strings.ContainsRune(view, '\r') {
		t.Fatalf("view contains escapes: %q", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got > 40 {
			t.Fatalf("line %q is %d cells wide", line, got)
		}
	}
}

// TestGoldenViews pins the layout at the three widths the architecture fixes.
func TestGoldenViews(t *testing.T) {
	steps := []struct {
		name  string
		step  app.StepID
		index int
	}{
		{"skills", app.StepSkills, 0},
		{"agents", app.StepAgents, 1},
		{"scope", app.StepScope, 2},
		{"method", app.StepMethod, 3},
		{"confirm", app.StepConfirm, 4},
	}
	for _, tc := range steps {
		for _, width := range []int{40, 80, 120} {
			t.Run(fmt.Sprintf("%s-%d", tc.name, width), func(t *testing.T) {
				f := newTestFlow(t)
				advanceTo(t, f, tc.index)
				if tc.step == app.StepSkills {
					send(t, f, down()) // focus the first skill so the detail pane renders
				}
				f.resize(width, 24)
				checkGolden(t, fmt.Sprintf("%s-%dx24", tc.name, width), f.View())
			})
		}
	}
}

func TestGoldenEveQuestion(t *testing.T) {
	q := testQuestions()
	q.Steps = []app.StepID{app.StepSkills, app.StepEve, app.StepConfirm}
	q.Defaults.Skills = []string{"alpha-skill", "beta-skill"}
	f := newFlow(q, newTheme(io.Discard, false, true))
	f.resize(80, 24)
	advanceTo(t, f, 1)
	checkGolden(t, "eve-80x24", f.View())
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	got = strings.ReplaceAll(got, "\r\n", "\n")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if normalized := strings.ReplaceAll(string(want), "\r\n", "\n"); got != normalized {
		t.Errorf("view differs from %s:\n--- got ---\n%s\n--- want ---\n%s", path, got, normalized)
	}
}
