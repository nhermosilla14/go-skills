package tui

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/search"
)

const (
	// searchMinChars is how much must be typed before a search runs.
	searchMinChars = 2
	// searchMaxRows caps the visible results.
	searchMaxRows = 8
	// searchDebounce is how long the prompt waits after the last key before searching.
	searchDebounce = 200 * time.Millisecond
)

// SearchFunc runs one search query. The prompt owns no configuration: the caller closes over the
// client and the owner filter.
type SearchFunc func(query string) ([]search.Result, error)

// searchDebounceMsg fires when the typing pause ends.
type searchDebounceMsg struct{ seq int }

// searchDoneMsg carries the outcome of one search.
type searchDoneMsg struct {
	seq     int
	results []search.Result
	err     error
}

// searchPrompt is the interactive find prompt: one text input with a result list.
type searchPrompt struct {
	run       SearchFunc
	theme     theme
	query     []rune
	results   []search.Result
	cursor    int
	offset    int
	searched  bool
	searching bool
	failure   string
	seq       int
	width     int
	height    int
	done      bool
	cancelled bool
	selected  search.Result
}

func newSearchPrompt(theme theme, run SearchFunc) *searchPrompt {
	return &searchPrompt{run: run, theme: theme, width: defaultWidth, height: defaultHeight}
}

func (m *searchPrompt) Init() tea.Cmd { return nil }

func (m *searchPrompt) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		if m.done {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.cancelled, m.done = true, true
			return m, tea.Quit
		case tea.KeyUp:
			m.move(-1)
		case tea.KeyDown:
			m.move(1)
		case tea.KeyEnter:
			if result, ok := m.focused(); ok {
				m.selected, m.done = result, true
				return m, tea.Quit
			}
		case tea.KeyBackspace:
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				return m, m.changed()
			}
		case tea.KeySpace:
			m.query = append(m.query, ' ')
			return m, m.changed()
		case tea.KeyRunes:
			m.query = append(m.query, msg.Runes...)
			return m, m.changed()
		}
	case searchDebounceMsg:
		if msg.seq != m.seq || len(m.query) < searchMinChars {
			return m, nil
		}
		m.searching, m.searched = true, true
		query, seq, run := m.queryText(), m.seq, m.run
		return m, func() tea.Msg {
			results, err := run(query)
			return searchDoneMsg{seq: seq, results: results, err: err}
		}
	case searchDoneMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.searching = false
		m.results = msg.results
		m.cursor, m.offset = 0, 0
		m.failure = ""
		if msg.err != nil {
			m.results = nil
			m.failure = msg.err.Error()
		}
		return m, nil
	}
	return m, nil
}

// changed resets the result list and schedules a debounced search when the query is long enough.
func (m *searchPrompt) changed() tea.Cmd {
	m.seq++
	m.results = nil
	m.cursor, m.offset = 0, 0
	m.searched, m.searching = false, false
	m.failure = ""
	if len(m.query) < searchMinChars {
		return nil
	}
	seq := m.seq
	return tea.Tick(searchDebounce, func(time.Time) tea.Msg { return searchDebounceMsg{seq: seq} })
}

func (m *searchPrompt) queryText() string { return string(m.query) }

func (m *searchPrompt) move(delta int) {
	if len(m.results) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.results) {
		m.cursor = len(m.results) - 1
	}
}

// focused returns the highlighted result, if there is one.
func (m *searchPrompt) focused() (search.Result, bool) {
	if m.searching || m.cursor < 0 || m.cursor >= len(m.results) {
		return search.Result{}, false
	}
	return m.results[m.cursor], true
}

func (m *searchPrompt) resize(width, height int) {
	if width > 0 {
		m.width = width
	}
	if height > 0 {
		m.height = height
	}
}

// visibleRows is how many result rows fit: header, footer and two rules take four.
func (m *searchPrompt) visibleRows() int {
	rows := m.height - 4
	if rows < 3 {
		rows = 3
	}
	if rows > searchMaxRows {
		rows = searchMaxRows
	}
	return rows
}

// follow keeps the cursor inside the visible window.
func (m *searchPrompt) follow(visible int) {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *searchPrompt) view() string {
	lines := []string{m.theme.bold(m.theme.cut("Search skills: "+m.queryText(), m.width))}
	lines = append(lines, m.theme.faint(m.theme.rule(m.width)))
	switch {
	case len(m.query) < searchMinChars:
		lines = append(lines, m.theme.faint(m.theme.cut("Start typing to search (min 2 chars)", m.width)))
	case m.searching:
		lines = append(lines, m.theme.faint(m.theme.cut("Searching"+m.theme.ellipsis(), m.width)))
	case m.failure != "":
		lines = append(lines, m.theme.faint(m.theme.cut("Search failed: "+m.failure, m.width)))
	case m.searched && len(m.results) == 0:
		lines = append(lines, m.theme.faint(m.theme.cut("No skills found", m.width)))
	default:
		visible := m.visibleRows()
		m.follow(visible)
		for i := m.offset; i < len(m.results) && i < m.offset+visible; i++ {
			lines = append(lines, m.rowLine(i))
		}
	}
	lines = append(lines, m.theme.faint(m.theme.rule(m.width)))
	hint := footer(m.theme, defaultKeys().up, defaultKeys().down,
		defaultKeys().confirm, defaultKeys().cancel)
	lines = append(lines, m.theme.faint(m.theme.cut(hint, m.width)))
	return strings.Join(lines, "\n")
}

// rowLine renders one result: full reference, install badge and the cursor marker.
func (m *searchPrompt) rowLine(index int) string {
	result := m.results[index]
	marker := "  "
	if index == m.cursor {
		marker = m.theme.cursor() + " "
	}
	markWidth := m.theme.width(marker)
	if m.width < markWidth+4 {
		marker, markWidth = "", 0
	}
	ref := searchDisplayRef(result)
	if badge := search.FormatInstalls(result.Installs); badge != "" {
		ref += "  " + badge
	}
	line := marker + m.theme.cut(ref, m.width-markWidth)
	if index == m.cursor {
		return m.theme.bold(line)
	}
	return line
}

// searchDisplayRef is the install reference shown for a result.
func searchDisplayRef(result search.Result) string {
	pkg := result.Source
	if pkg == "" {
		pkg = result.ID
	}
	return pkg + "@" + result.Name
}

// View implements tea.Model.
func (m *searchPrompt) View() string { return m.view() }

// RunSearch shows the interactive search prompt and returns the selected result. found is false
// when the user cancels.
func RunSearch(in io.Reader, out io.Writer, opts Options, run SearchFunc) (result search.Result, found bool, err error) {
	if run == nil {
		return search.Result{}, false, errors.New("interactive search has no search function")
	}
	model := newSearchPrompt(newTheme(out, opts.Color, opts.ASCII), run)
	program := tea.NewProgram(model,
		tea.WithInput(cancelableInput(in)),
		tea.WithOutput(out),
	)
	final, err := program.Run()
	if err != nil {
		return search.Result{}, false, fmt.Errorf("interactive search failed: %w", err)
	}
	done, ok := final.(*searchPrompt)
	if !ok {
		return search.Result{}, false, errors.New("interactive search returned an unexpected model")
	}
	if done.cancelled || !done.done {
		return search.Result{}, false, nil
	}
	return done.selected, true, nil
}
