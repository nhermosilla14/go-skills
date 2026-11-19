package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/install"
)

// option is one row of a single-select.
type option struct {
	label  string
	detail string
}

// choice is the single-select used for the scope and the install method.
type choice struct {
	title   string
	step    app.StepID
	options []option
	cursor  int
	width   int
	theme   theme
	keys    keys
}

func newChoice(theme theme, step app.StepID, title string, options []option) *choice {
	return &choice{
		title:   title,
		step:    step,
		options: options,
		width:   defaultWidth,
		theme:   theme,
		keys:    defaultKeys(),
	}
}

func (c *choice) update(msg tea.Msg) (stepResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return stepRunning, nil
	}
	switch {
	case isCancel(keyMsg):
		return stepCancelled, nil
	case key.Matches(keyMsg, c.keys.up):
		c.move(-1)
	case key.Matches(keyMsg, c.keys.down):
		c.move(1)
	case key.Matches(keyMsg, c.keys.confirm):
		return stepFinished, nil
	}
	return stepRunning, nil
}

func (c *choice) move(delta int) {
	if len(c.options) == 0 {
		return
	}
	c.cursor = (c.cursor + delta + len(c.options)) % len(c.options)
}

func (c *choice) resize(width, _ int) {
	if width > 0 {
		c.width = width
	}
}

func (c *choice) apply(a *app.Answers) {
	switch c.step {
	case app.StepScope:
		if c.cursor == 1 {
			a.Scope = app.ScopeGlobal
			return
		}
		a.Scope = app.ScopeProject
	case app.StepUpdateScope:
		switch c.cursor {
		case 1:
			a.Scope = app.ScopeGlobal
		case 2:
			a.Scope = app.ScopeBoth
		default:
			a.Scope = app.ScopeProject
		}
	case app.StepMethod:
		if c.cursor == 1 {
			a.Mode = install.ModeCopy
			return
		}
		a.Mode = install.ModeSymlink
	}
}

func (c *choice) view() string {
	lines := []string{c.theme.bold(c.theme.cut(c.title, c.width)), c.theme.faint(c.theme.rule(c.width))}
	for i, opt := range c.options {
		marker := "  "
		if i == c.cursor {
			marker = c.theme.cursor() + " "
		}
		markWidth := c.theme.width(marker)
		if c.width < markWidth+4 {
			marker, markWidth = "", 0
		}
		body := sanitize(opt.label)
		if opt.detail != "" && c.width >= tinyWidth {
			body += "  " + sanitize(opt.detail)
		}
		line := marker + c.theme.cut(body, c.width-markWidth)
		if i == c.cursor {
			line = c.theme.bold(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, c.theme.faint(c.theme.rule(c.width)))
	lines = append(lines, c.theme.faint(c.theme.cut(
		footer(c.theme, c.keys.up, c.keys.down, c.keys.confirm, c.keys.cancel), c.width)))
	return strings.Join(lines, "\n")
}

const (
	// symlinkLabel and copyLabel are the install-method choices, in documented order.
	symlinkLabel = "Symlink (Recommended)"
	copyLabel    = "Copy to all agents"
)

// confirm is the yes/no prompt of the final confirmation and of the Eve question.
type confirm struct {
	title  string
	note   string // optional faint line under the question
	step   app.StepID
	cursor int // 0 yes, 1 no
	width  int
	theme  theme
	keys   keys
}

func newConfirm(theme theme, step app.StepID, title string) *confirm {
	return &confirm{title: title, step: step, width: defaultWidth, theme: theme, keys: defaultKeys()}
}

// newConfirmNote builds a yes/no prompt with a faint note under the question.
func newConfirmNote(theme theme, step app.StepID, title, note string) *confirm {
	c := newConfirm(theme, step, title)
	c.note = note
	return c
}

func (c *confirm) update(msg tea.Msg) (stepResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return stepRunning, nil
	}
	switch {
	case isCancel(keyMsg):
		return stepCancelled, nil
	case key.Matches(keyMsg, c.keys.up), key.Matches(keyMsg, c.keys.down):
		c.cursor = 1 - c.cursor
	case keyMsg.String() == "y":
		c.cursor = 0
		return c.answer()
	case keyMsg.String() == "n":
		c.cursor = 1
		return c.answer()
	case key.Matches(keyMsg, c.keys.confirm):
		return c.answer()
	}
	return stepRunning, nil
}

// answer finishes the prompt. Declining a final confirmation cancels the run; declining the Eve
// question only continues to the agent picker.
func (c *confirm) answer() (stepResult, tea.Cmd) {
	if c.cursor == 0 {
		return stepFinished, nil
	}
	if c.step == app.StepConfirm || c.step == app.StepRemoveConfirm {
		return stepCancelled, nil
	}
	return stepFinished, nil
}

func (c *confirm) resize(width, _ int) {
	if width > 0 {
		c.width = width
	}
}

func (c *confirm) apply(a *app.Answers) {
	switch c.step {
	case app.StepConfirm, app.StepRemoveConfirm:
		a.Proceed = c.cursor == 0
	case app.StepEve:
		// Declining clears the targets so the agent picker decides them.
		a.Agents = nil
		if c.cursor == 0 {
			a.Agents = []string{"eve"}
		}
	}
}

// offerPrompt runs the one-time find-skills question as its own program, because the add flow's
// session has already finished by the time it is asked.
type offerPrompt struct {
	confirm   *confirm
	accepted  bool
	done      bool
	cancelled bool
}

func newOfferPrompt(theme theme) *offerPrompt {
	return &offerPrompt{
		confirm: newConfirmNote(theme, app.StepFindSkillsOffer, findSkillsQuestion, findSkillsNote),
	}
}

func (o *offerPrompt) Init() tea.Cmd { return nil }

func (o *offerPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		o.confirm.resize(size.Width, size.Height)
		return o, nil
	}
	result, cmd := o.confirm.update(msg)
	switch result {
	case stepCancelled:
		o.cancelled, o.done = true, true
		return o, tea.Quit
	case stepFinished:
		o.accepted, o.done = o.confirm.cursor == 0, true
		return o, tea.Quit
	}
	return o, cmd
}

func (o *offerPrompt) View() string { return o.confirm.view() }

func (c *confirm) view() string {
	var lines []string
	for _, line := range c.theme.wrap(c.title, c.width, 3) {
		lines = append(lines, c.theme.bold(line))
	}
	if c.note != "" {
		for _, line := range c.theme.wrap(c.note, c.width, 3) {
			lines = append(lines, c.theme.faint(line))
		}
	}
	lines = append(lines, c.theme.faint(c.theme.rule(c.width)))
	for i, label := range []string{"Yes", "No"} {
		marker := "  "
		if i == c.cursor {
			marker = c.theme.cursor() + " "
		}
		markWidth := c.theme.width(marker)
		if c.width < markWidth+4 {
			marker, markWidth = "", 0
		}
		line := marker + c.theme.cut(label, c.width-markWidth)
		if i == c.cursor {
			line = c.theme.bold(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, c.theme.faint(c.theme.rule(c.width)))
	lines = append(lines, c.theme.faint(c.theme.cut(
		footer(c.theme, c.keys.up, c.keys.down, c.keys.confirm, c.keys.cancel), c.width)))
	return strings.Join(lines, "\n")
}
