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

// Session implements app.Prompter over a terminal. It starts one program on the first run and
// refuses to run a second.
type Session struct {
	in   io.Reader
	out  io.Writer
	opts Options
	used bool
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
