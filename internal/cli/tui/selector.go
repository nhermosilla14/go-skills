package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/app"
)

// stepResult is what a step model reports after handling a message.
type stepResult int

const (
	stepRunning stepResult = iota
	stepFinished
	stepCancelled
)

// stepModel is one prompt of the flow. Steps never talk to each other: the flow moves forward
// only, and a step that finishes writes its answer through apply.
type stepModel interface {
	update(tea.Msg) (stepResult, tea.Cmd)
	view() string
	resize(width, height int)
	apply(*app.Answers)
}

// keys are the bindings shared by every step.
type keys struct {
	up      key.Binding
	down    key.Binding
	toggle  key.Binding
	all     key.Binding
	confirm key.Binding
	cancel  key.Binding
}

func defaultKeys() keys {
	return keys{
		up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k", "up")),
		down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j", "down")),
		toggle:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		all:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
		confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
		cancel:  key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel")),
	}
}

// isCancel reports whether msg is a cancel key. EOF on the input stream is turned into ctrl+c by
// the session before it reaches the program.
func isCancel(msg tea.KeyMsg) bool {
	return key.Matches(msg, defaultKeys().cancel)
}

// footer renders the key hints with the separator the glyph set uses.
func footer(theme theme, bindings ...key.Binding) string {
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		parts = append(parts, b.Help().Key+" "+b.Help().Desc)
	}
	sep := " · "
	if theme.ascii {
		sep = " | "
	}
	return strings.Join(parts, sep)
}

// item is one row of the selector.
type item struct {
	id      string
	title   string
	detail  string // description or directory hint
	hint    string // extra column, agent directories
	path    string // source path shown in the detail pane
	group   string // section title of the item
	locked  bool   // always included, not togglable
	checked bool
}

// section groups items under a heading. A positive more count is summarised as a note row, which
// is how hidden universal agents stay visible without a row each.
type section struct {
	title string
	items []int
	more  int
}

type rowKind int

const (
	rowSelectAll rowKind = iota
	rowHeading
	rowNote
	rowItem
)

type row struct {
	kind rowKind
	text string
	item int
}

// selector is the grouped multi-select with an optional detail pane. It knows nothing about
// skills or agents: it takes items and sections in and returns selected identifiers out.
type selector struct {
	title      string
	items      []item
	sections   []section
	selectAll  bool
	requireOne bool
	detail     bool
	step       app.StepID
	width      int
	height     int
	cursor     int
	offset     int
	rows       []row
	theme      theme
	keys       keys
}

func newSelector(theme theme, step app.StepID, title string, items []item, sections []section) *selector {
	skillStep := step == app.StepSkills || step == app.StepRemoveSkills
	s := &selector{
		title:      title,
		items:      items,
		sections:   sections,
		selectAll:  skillStep,
		requireOne: true,
		detail:     skillStep,
		step:       step,
		keys:       defaultKeys(),
		theme:      theme,
	}
	s.layout()
	s.cursor = s.firstRow()
	return s
}

// layout flattens the sections into the rows that are rendered and navigated.
func (s *selector) layout() {
	if s.width == 0 {
		s.width = defaultWidth
	}
	s.rows = nil
	if s.selectAll {
		s.rows = append(s.rows, row{kind: rowSelectAll, text: "Select all"})
	}
	for _, sec := range s.sections {
		if sec.title != "" && s.width >= tinyWidth {
			s.rows = append(s.rows, row{kind: rowHeading, text: sec.title})
		}
		for _, i := range sec.items {
			s.rows = append(s.rows, row{kind: rowItem, item: i})
		}
		if sec.more > 0 && s.width >= tinyWidth {
			s.rows = append(s.rows, row{kind: rowNote,
				text: fmt.Sprintf("%sand %d more", s.theme.ellipsis(), sec.more)})
		}
	}
}

func (s *selector) firstRow() int {
	for i, r := range s.rows {
		if s.selectable(r) {
			return i
		}
	}
	return 0
}

// selectable reports whether the cursor may stop on a row.
func (s *selector) selectable(r row) bool {
	switch r.kind {
	case rowSelectAll:
		return true
	case rowItem:
		return true
	default:
		return false
	}
}

// resize recomputes the layout for a new terminal size.
func (s *selector) resize(width, height int) {
	if width > 0 {
		s.width = width
	}
	if height > 0 {
		s.height = height
	}
	s.layout()
	if s.cursor >= len(s.rows) {
		s.cursor = s.firstRow()
	}
}

func (s *selector) update(msg tea.Msg) (stepResult, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return stepRunning, nil
	}
	switch {
	case isCancel(keyMsg):
		return stepCancelled, nil
	case key.Matches(keyMsg, s.keys.up):
		s.move(-1)
	case key.Matches(keyMsg, s.keys.down):
		s.move(1)
	case key.Matches(keyMsg, s.keys.toggle):
		s.toggle()
	case key.Matches(keyMsg, s.keys.all):
		s.toggleAll()
	case key.Matches(keyMsg, s.keys.confirm):
		if s.requireOne && s.selectedCount() == 0 {
			return stepRunning, nil
		}
		return stepFinished, nil
	}
	return stepRunning, nil
}

// move shifts the cursor by delta selectable rows, skipping headings and notes.
func (s *selector) move(delta int) {
	if len(s.rows) == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
		delta = -delta
	}
	for n := 0; n < delta; n++ {
		for i := s.cursor + dir; i >= 0 && i < len(s.rows); i += dir {
			if s.selectable(s.rows[i]) {
				s.cursor = i
				break
			}
		}
	}
}

// toggle flips the checkbox under the cursor. Locked items and locked sections do not change.
func (s *selector) toggle() {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return
	}
	r := s.rows[s.cursor]
	switch r.kind {
	case rowSelectAll:
		s.toggleAll()
	case rowItem:
		if s.items[r.item].locked {
			return
		}
		s.items[r.item].checked = !s.items[r.item].checked
	}
}

// toggleAll selects everything when anything selectable is unchecked, and clears it otherwise.
func (s *selector) toggleAll() {
	all := true
	for _, it := range s.items {
		if !it.locked && !it.checked {
			all = false
			break
		}
	}
	for i := range s.items {
		if s.items[i].locked {
			continue
		}
		s.items[i].checked = !all
	}
}

func (s *selector) selectedCount() int {
	count := 0
	for _, it := range s.items {
		if it.locked || it.checked {
			count++
		}
	}
	return count
}

func (s *selector) selectedIDs() []string {
	var ids []string
	for _, it := range s.items {
		if it.checked {
			ids = append(ids, it.id)
		}
	}
	return ids
}

func (s *selector) apply(a *app.Answers) {
	if s.step == app.StepAgents {
		a.Agents = s.selectedIDs()
		return
	}
	a.Skills = s.selectedIDs()
}

// detailShown reports whether the focused item has a pane to show.
func (s *selector) detailShown() bool {
	if !s.detail || s.width < minWidth || s.height < 12 {
		return false
	}
	it, ok := s.focusedItem()
	return ok && (it.path != "" || it.detail != "")
}

func (s *selector) focusedItem() (item, bool) {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return item{}, false
	}
	r := s.rows[s.cursor]
	if r.kind != rowItem {
		return item{}, false
	}
	return s.items[r.item], true
}

// detailLines are the lines of the detail pane, capped as the layout decision requires.
func (s *selector) detailLines() []string {
	it, ok := s.focusedItem()
	if !ok {
		return nil
	}
	lines := []string{s.theme.bold(it.title)}
	if it.path != "" {
		lines = append(lines, s.theme.faint(it.path))
	}
	lines = append(lines, s.theme.wrap(it.detail, s.width, 6)...)
	return lines
}

// viewportRows is the number of list rows that fit: header, footer and separators take four, the
// detail pane takes its own lines, and the list never shows more than twenty rows.
func (s *selector) viewportRows() int {
	rows := s.height - 4
	if s.detailShown() {
		rows -= len(s.detailLines()) + 1
	}
	if rows < 3 {
		rows = 3
	}
	if rows > maxRows {
		rows = maxRows
	}
	return rows
}

// follow keeps the cursor inside the visible window.
func (s *selector) follow(visible int) {
	if s.cursor < s.offset {
		s.offset = s.cursor
	}
	if s.cursor >= s.offset+visible {
		s.offset = s.cursor - visible + 1
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

func (s *selector) rowLine(index int) string {
	r := s.rows[index]
	focused := index == s.cursor
	switch r.kind {
	case rowSelectAll:
		return s.checkLine(focused, s.allSelected(), r.text, "")
	case rowHeading:
		return s.theme.accent(s.theme.cut(r.text, s.width))
	case rowNote:
		return s.theme.faint("  " + s.theme.cut(r.text, s.width-2))
	}
	it := s.items[r.item]
	if s.width < tinyWidth {
		return s.checkLine(focused, it.checked || it.locked, it.title, "")
	}
	note := it.detail
	if it.hint != "" {
		note = it.hint
	}
	return s.checkLine(focused, it.checked || it.locked, it.title, note)
}

// checkLine renders one checkbox row: marker, box, title and an optional note. The marker stays
// outside the sanitized text, which trims its own edges.
func (s *selector) checkLine(focused, checked bool, title, note string) string {
	marker := "  "
	if focused {
		marker = s.theme.cursor() + " "
	}
	markWidth := s.theme.width(marker)
	if s.width < markWidth+4 {
		// At extreme widths the marker would push the line past the terminal edge.
		marker, markWidth = "", 0
	}
	box := "[ ]"
	if checked {
		box = "[x]"
	}
	body := box + " " + sanitize(title)
	if note != "" && s.width >= tinyWidth {
		body += "  " + sanitize(note)
	}
	line := marker + s.theme.cut(body, s.width-markWidth)
	if focused {
		return s.theme.bold(line)
	}
	return line
}

func (s *selector) allSelected() bool {
	for _, it := range s.items {
		if !it.locked && !it.checked {
			return false
		}
	}
	return true
}

func (s *selector) view() string {
	s.layout()
	visible := s.viewportRows()
	s.follow(visible)
	lines := []string{s.theme.bold(s.theme.cut(s.title, s.width))}
	lines = append(lines, s.theme.faint(s.theme.rule(s.width)))
	for i := s.offset; i < len(s.rows) && i < s.offset+visible; i++ {
		lines = append(lines, s.rowLine(i))
	}
	if s.detailShown() {
		lines = append(lines, s.theme.faint(s.theme.rule(s.width)))
		lines = append(lines, s.detailLines()...)
	}
	hint := footer(s.theme, s.keys.up, s.keys.down, s.keys.toggle, s.keys.all, s.keys.confirm, s.keys.cancel)
	if s.requireOne && s.selectedCount() == 0 {
		hint = "select at least one; " + hint
	}
	lines = append(lines, s.theme.faint(s.theme.rule(s.width)))
	lines = append(lines, s.theme.faint(s.theme.cut(hint, s.width)))
	return strings.Join(lines, "\n")
}
