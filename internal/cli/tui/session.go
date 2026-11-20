package tui

import (
	"errors"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nhermosilla14/go-skills/internal/app"
)

// Options configure one Session. The caller decides colour and glyph set, because nothing below
// cmd/skills probes the terminal.
type Options struct {
	// Color emits the theme's SGR sequences. False when NO_COLOR is set, TERM is dumb, or the
	// human stream is not a terminal.
	Color bool
	// ASCII replaces box-drawing glyphs and the ellipsis outside a UTF-8 locale.
	ASCII bool
}

// Session implements app.Prompter over a terminal. It starts one program for the add flow and
// refuses a second; the one-time find-skills offer gets its own program because it runs after the
// flow has finished.
type Session struct {
	in        io.Reader
	out       io.Writer
	opts      Options
	used      bool
	offerUsed bool
	packsUsed bool
}

// New returns a session that reads keys from in and renders on out, which must be the human
// stream (stderr).
func New(in io.Reader, out io.Writer, opts Options) *Session {
	return &Session{in: in, out: out, opts: opts}
}

// Ask runs the flow and returns every answer. Cancellation, including EOF on the input stream,
// returns app.ErrCancelled; a question with no steps returns the defaults without a program.
func (s *Session) Ask(questions app.Questions) (app.Answers, error) {
	if len(questions.Steps) == 0 {
		return questions.Defaults, nil
	}
	if s.used {
		return app.Answers{}, errors.New("interactive prompt already finished")
	}
	s.used = true
	model := newFlow(questions, newTheme(s.out, s.opts.Color, s.opts.ASCII))
	program := tea.NewProgram(model,
		tea.WithInput(cancelableInput(s.in)),
		tea.WithOutput(s.out),
	)
	final, err := program.Run()
	if err != nil {
		return app.Answers{}, fmt.Errorf("interactive prompt failed: %w", err)
	}
	done, ok := final.(*flow)
	if !ok {
		return app.Answers{}, errors.New("interactive prompt returned an unexpected model")
	}
	if done.cancelled || !done.done {
		return app.Answers{}, app.ErrCancelled
	}
	return done.answers, nil
}

// The one-time find-skills offer, as documented.
const (
	findSkillsQuestion = "Install the find-skills skill? It helps your agent discover and suggest skills."
	findSkillsNote     = "One-time prompt - you won't be asked again if you dismiss."
)

// AskFindSkillsOffer asks the one-time find-skills question in its own program. A cancelled prompt
// returns app.ErrCancelled so the caller can treat it as a decline.
func (s *Session) AskFindSkillsOffer() (bool, error) {
	if s.offerUsed {
		return false, errors.New("find-skills prompt already finished")
	}
	s.offerUsed = true
	model := newOfferPrompt(newTheme(s.out, s.opts.Color, s.opts.ASCII))
	program := tea.NewProgram(model,
		tea.WithInput(cancelableInput(s.in)),
		tea.WithOutput(s.out),
	)
	final, err := program.Run()
	if err != nil {
		return false, fmt.Errorf("find-skills prompt failed: %w", err)
	}
	done, ok := final.(*offerPrompt)
	if !ok {
		return false, errors.New("find-skills prompt returned an unexpected model")
	}
	if done.cancelled || !done.done {
		return false, app.ErrCancelled
	}
	return done.accepted, nil
}

// SelectNotionPacks asks which Notion packs to install, in its own program: the answer decides
// what the add flow discovers, so it runs before the flow starts. Cancelling returns
// app.ErrCancelled.
func (s *Session) SelectNotionPacks(workspace string, packs []app.NotionPackChoice) ([]string, error) {
	if s.packsUsed {
		return nil, errors.New("notion pack prompt already finished")
	}
	s.packsUsed = true
	model := newPacksPrompt(newTheme(s.out, s.opts.Color, s.opts.ASCII), workspace, packs)
	program := tea.NewProgram(model,
		tea.WithInput(cancelableInput(s.in)),
		tea.WithOutput(s.out),
	)
	final, err := program.Run()
	if err != nil {
		return nil, fmt.Errorf("notion pack prompt failed: %w", err)
	}
	done, ok := final.(*packsPrompt)
	if !ok {
		return nil, errors.New("notion pack prompt returned an unexpected model")
	}
	if done.cancelled || !done.done {
		return nil, app.ErrCancelled
	}
	return done.selected(), nil
}

// cancelableInput wraps the input so the end of the stream becomes ctrl+c. Bubble Tea stops
// reading on EOF but keeps the program alive, so without this an exhausted stream would hang
// instead of cancelling. A terminal file keeps its descriptor methods: Bubble Tea needs them to
// put the terminal in raw mode, and the cancel reader reads through this wrapper.
func cancelableInput(in io.Reader) io.Reader {
	if f, ok := in.(*os.File); ok {
		return &ttyInput{f: f}
	}
	return &cancelOnEOF{r: in}
}

// eofState tracks whether the synthetic ctrl+c was already delivered.
type eofState struct {
	sent bool
}

// read turns a final EOF into one ctrl+c byte. Bytes read alongside the EOF are returned first.
func (e *eofState) read(r io.Reader, p []byte) (int, error) {
	if e.sent {
		return 0, io.EOF
	}
	n, err := r.Read(p)
	if errors.Is(err, io.EOF) {
		e.sent = true
		if n > 0 {
			return n, nil
		}
		if len(p) == 0 {
			return 0, io.EOF
		}
		p[0] = 0x03
		return 1, nil
	}
	return n, err
}

// cancelOnEOF is the wrapper for inputs that are not terminal files.
type cancelOnEOF struct {
	r io.Reader
	eofState
}

func (c *cancelOnEOF) Read(p []byte) (int, error) { return c.read(c.r, p) }

// ttyInput is the wrapper for a terminal file. It implements the read-write-closer and descriptor
// methods Bubble Tea and its cancel reader look for.
type ttyInput struct {
	f *os.File
	eofState
}

func (t *ttyInput) Read(p []byte) (int, error)  { return t.read(t.f, p) }
func (t *ttyInput) Write(p []byte) (int, error) { return t.f.Write(p) }
func (t *ttyInput) Close() error                { return t.f.Close() }
func (t *ttyInput) Fd() uintptr                 { return t.f.Fd() }
func (t *ttyInput) Name() string                { return t.f.Name() }
