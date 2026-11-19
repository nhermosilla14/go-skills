package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/app"
)

// flow is the root model: it owns the answers, the compiled steps and the step in progress.
type flow struct {
	questions app.Questions
	answers   app.Answers
	steps     []app.StepID
	index     int
	current   stepModel
	width     int
	height    int
	theme     theme
	done      bool
	cancelled bool
	// eveAccepted marks that the Eve confirmation replaced the agent picker.
	eveAccepted bool
}

func newFlow(questions app.Questions, theme theme) *flow {
	f := &flow{
		questions: questions,
		answers:   questions.Defaults,
		steps:     questions.Steps,
		width:     defaultWidth,
		height:    defaultHeight,
		theme:     theme,
	}
	f.open()
	return f
}

// Init implements tea.Model. The flow has nothing to fetch: the source is already acquired.
func (f *flow) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (f *flow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.resize(msg.Width, msg.Height)
		return f, nil
	case tea.KeyMsg:
		if f.done || f.current == nil {
			return f, nil
		}
		step := f.steps[f.index]
		result, cmd := f.current.update(msg)
		switch result {
		case stepCancelled:
			f.cancelled = true
			f.done = true
			return f, tea.Quit
		case stepFinished:
			f.current.apply(&f.answers)
			if step == app.StepEve && len(f.answers.Agents) > 0 {
				f.eveAccepted = true
			}
			f.index++
			return f, f.open()
		}
		return f, cmd
	}
	return f, nil
}

// View implements tea.Model.
func (f *flow) View() string {
	if f.current == nil {
		return ""
	}
	return f.current.view()
}

func (f *flow) resize(width, height int) {
	if width <= 0 {
		width = defaultWidth
	}
	if height <= 0 {
		height = defaultHeight
	}
	f.width, f.height = width, height
	if f.current != nil {
		f.current.resize(width, height)
	}
}

// open starts the step at the current index, skipping the agent picker when the Eve answer already
// chose the targets. When no step is left the flow is done and quits.
func (f *flow) open() tea.Cmd {
	for f.index < len(f.steps) {
		step := f.steps[f.index]
		if step == app.StepAgents && f.eveAccepted {
			f.index++
			continue
		}
		f.current = f.newStep(step)
		f.current.resize(f.width, f.height)
		return nil
	}
	// Keep the last step rendered: the program is inline, so the final frame stays in the
	// terminal's scrollback.
	f.done = true
	return tea.Quit
}

// newStep builds the sub-model of one prompt from the questions.
func (f *flow) newStep(step app.StepID) stepModel {
	switch step {
	case app.StepSkills:
		return newSelector(f.theme, step, "Select skills to install",
			skillItems(f.questions.Skills), skillSections(f.questions.Skills))
	case app.StepAgents:
		items, sections := agentLists(f.questions.Agents)
		return newSelector(f.theme, step, "Select agents to install", items, sections)
	case app.StepScope:
		return newChoice(f.theme, step, "Installation scope", []option{
			{label: "Project", detail: "Install in current directory (committed with your project)"},
			{label: "Global", detail: "Install in home directory (available across all projects)"},
		})
	case app.StepUpdateScope:
		return newChoice(f.theme, step, "Update scope", []option{
			{label: "Project", detail: "Update skills installed in this project"},
			{label: "Global", detail: "Not supported yet"},
			{label: "Both", detail: "Not supported yet"},
		})
	case app.StepMethod:
		return newChoice(f.theme, step, "Installation method", []option{
			{label: symlinkLabel, detail: "Single source of truth, easy updates"},
			{label: copyLabel, detail: "Independent copies for each agent"},
		})
	case app.StepEve:
		return newConfirm(f.theme, step, f.eveQuestion())
	case app.StepRemoveSkills:
		return newSelector(f.theme, step, "Select skills to remove",
			skillItems(f.questions.Skills), skillSections(f.questions.Skills))
	case app.StepRemoveConfirm:
		title := f.questions.Message
		if title == "" {
			title = "Remove the selected skills?"
		}
		return newConfirm(f.theme, step, title)
	}
	return newConfirm(f.theme, step, "Proceed with installation?")
}

// eveQuestion is upstream's message, naming the skills that will be installed.
func (f *flow) eveQuestion() string {
	names := f.answers.Skills
	if len(names) == 0 {
		names = make([]string, 0, len(f.questions.Skills))
		for _, s := range f.questions.Skills {
			names = append(names, s.Name)
		}
	}
	return fmt.Sprintf("Detected an Eve project. Install %s for your eve agent to use?", strings.Join(names, ", "))
}

// skillItems turns the question choices into selector items.
func skillItems(skills []app.SkillChoice) []item {
	items := make([]item, 0, len(skills))
	for _, s := range skills {
		items = append(items, item{
			id:     s.Name,
			title:  s.Name,
			detail: s.Description,
			path:   s.Path,
			group:  s.Group,
		})
	}
	return items
}

// skillSections groups the items by plugin name, dropping the heading when the source has no
// groups so the list stays a plain multi-select.
func skillSections(skills []app.SkillChoice) []section {
	grouped := false
	for _, s := range skills {
		if s.Group != "" {
			grouped = true
			break
		}
	}
	if !grouped {
		indexes := make([]int, len(skills))
		for i := range skills {
			indexes[i] = i
		}
		return []section{{items: indexes}}
	}
	var sections []section
	index := map[string]int{}
	for i, s := range skills {
		at, ok := index[s.Group]
		if !ok {
			index[s.Group] = len(sections)
			sections = append(sections, section{title: s.Group})
			at = len(sections) - 1
		}
		sections[at].items = append(sections[at].items, i)
	}
	return sections
}

// agentLists builds the universal locked section and the selectable agents. Hidden universal
// agents are counted, not listed.
func agentLists(agents []app.AgentChoice) ([]item, []section) {
	var items []item
	var locked, others []int
	hidden := 0
	for _, a := range agents {
		if a.Universal {
			if a.Hidden {
				hidden++
				continue
			}
			locked = append(locked, len(items))
			items = append(items, item{id: a.ID, title: a.Name, locked: true, checked: true})
			continue
		}
		others = append(others, len(items))
		items = append(items, item{id: a.ID, title: a.Name, hint: a.Dir, checked: a.Detected})
	}
	sections := []section{{title: "Universal (.agents/skills)", items: locked, more: hidden}}
	if len(others) > 0 {
		sections = append(sections, section{title: "Other agents", items: others})
	}
	return items, sections
}
