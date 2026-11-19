package tui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
)

// runSession drives a Session over an in-memory input stream, as the architecture requires: no
// test touches a real terminal.
func runSession(t *testing.T, questions app.Questions, keys string) (app.Answers, string, error) {
	t.Helper()
	var out bytes.Buffer
	session := New(strings.NewReader(keys), &out, Options{})
	answers, err := session.Ask(questions)
	return answers, out.String(), err
}

func TestSessionReturnsEveryAnswer(t *testing.T) {
	answers, _, err := runSession(t, testQuestions(), "a\r\r\r\r\r")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !answers.Proceed || answers.Scope != app.ScopeProject {
		t.Fatalf("answers = %+v", answers)
	}
	if len(answers.Skills) != 3 {
		t.Fatalf("skills = %v, want all three", answers.Skills)
	}
	if len(answers.Agents) == 0 {
		t.Fatal("no agents answered")
	}
}

func TestSessionCancelReturnsErrCancelled(t *testing.T) {
	_, _, err := runSession(t, testQuestions(), "\x1b")
	if !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}

	// EOF on the input stream is a cancel too: it must never wait for more input.
	_, _, err = runSession(t, testQuestions(), "")
	if !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("EOF err = %v, want ErrCancelled", err)
	}
}

func TestSessionWithoutStepsStartsNoProgram(t *testing.T) {
	questions := app.Questions{Defaults: app.Answers{Scope: app.ScopeProject, Agents: []string{"codex"}}}
	answers, out, err := runSession(t, questions, "")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answers.Scope != app.ScopeProject || len(answers.Agents) != 1 {
		t.Fatalf("answers = %+v", answers)
	}
	if out != "" {
		t.Fatalf("output = %q, want nothing", out)
	}
}

func TestSessionRefusesASecondRun(t *testing.T) {
	session := New(strings.NewReader("\x1b"), &bytes.Buffer{}, Options{})
	if _, err := session.Ask(testQuestions()); !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("first Ask: %v", err)
	}
	if _, err := session.Ask(testQuestions()); err == nil {
		t.Fatal("second Ask succeeded, want an error")
	}
}

func TestSessionWritesOnlyItsOwnFrames(t *testing.T) {
	_, out, err := runSession(t, testQuestions(), "a\r\r\r\r\r")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	// The program is inline: the last frame stays on the human stream.
	if !strings.Contains(out, "Proceed with installation?") {
		t.Errorf("output does not contain the final frame: %q", out)
	}
	if strings.Contains(out, "error") {
		t.Errorf("output contains an error: %q", out)
	}
}
