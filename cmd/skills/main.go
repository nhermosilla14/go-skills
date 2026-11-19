// Command skills installs agent skills into a project. See docs/ for the behavior it implements.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/cli"
	"github.com/nhermosilla14/go-skills/internal/cli/tui"
)

// Build metadata, set at link time with -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cmd, err := cli.Parse(args)
	if err != nil {
		// A run that never started still owes the --json caller one document.
		if cli.JSONRequested(args) {
			_ = cli.WriteJSON(stdout, nil)
		}
		return reportError(stderr, err)
	}
	switch cmd.Kind {
	case cli.KindHelp:
		fmt.Fprint(stdout, cli.Help())
		return exitOK
	case cli.KindVersion:
		fmt.Fprintf(stdout, "skills %s (commit %s, built %s)\n", version, commit, date)
		return exitOK
	default:
		return runAdd(cmd.Add, stdout, stderr)
	}
}

// runAdd runs one add and owns the --json document, which is written once on every exit path.
func runAdd(opts app.AddOptions, stdout, stderr io.Writer) (code int) {
	var document []cli.Entry
	if opts.JSON {
		defer func() {
			if err := cli.WriteJSON(stdout, document); err != nil {
				fmt.Fprintf(stderr, "error: %v\n", err)
				code = exitFailure
			}
		}()
	}
	if err := cli.ValidateJSON(opts); err != nil {
		document = cli.JSONFailure(err)
		fmt.Fprintf(stderr, "error: %s\n", cli.Message(err))
		return exitFailure
	}
	workDir, err := os.Getwd()
	if err != nil {
		document = cli.JSONFailure(err)
		return reportError(stderr, err)
	}
	interactive := canPrompt()
	env := app.Env{
		WorkDir:         workDir,
		IsTTY:           interactive,
		IncludeInternal: internalSkillsEnabled(),
		Now:             time.Now,
		AgentEnv:        agents.FromOS(workDir),
	}
	var prompter app.Prompter = app.NoPrompts{}
	if interactive && !opts.JSON {
		prompter = tui.New(os.Stdin, stderr, tui.Options{Color: colorEnabled(), ASCII: !utf8Locale()})
	}
	rep, err := app.Add(opts, env, prompter)
	for _, w := range rep.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if err != nil {
		document = cli.JSONFailure(err)
		if errors.Is(err, app.ErrCancelled) {
			fmt.Fprintln(stderr, "Installation cancelled")
			if interactive {
				return exitOK
			}
			return exitFailure
		}
		return reportError(stderr, err)
	}
	if opts.List {
		cli.WriteListing(stdout, rep.Listed)
		return exitOK
	}
	cli.WriteReport(stderr, rep)
	if opts.JSON {
		document = cli.JSONEntries(rep)
		if rep.Failed() || cli.HasSkipped(document) {
			return exitFailure
		}
		return exitOK
	}
	if rep.Failed() {
		return exitFailure
	}
	return exitOK
}

// reportError prints err to stderr and returns the exit code for it.
func reportError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "error: %s\n", cli.Message(err))
	var usage *cli.UsageError
	if errors.As(err, &usage) || errors.Is(err, app.ErrUsage) {
		fmt.Fprintln(stderr, "Run 'skills --help' for usage.")
		return exitUsage
	}
	return exitFailure
}

// canPrompt reports whether an interactive flow may run: stdin must be a terminal and TERM must
// not be dumb, because raw mode on a dumb terminal corrupts the session.
func canPrompt() bool {
	return isTerminal(os.Stdin) && !dumbTerminal()
}

// isTerminal reports whether f is a terminal, as golang.org/x/term defines it.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func dumbTerminal() bool {
	return strings.EqualFold(os.Getenv("TERM"), "dumb")
}

// colorEnabled follows the ADR: NO_COLOR wins, TERM=dumb disables, and the human stream decides.
// CI no longer forces colour on.
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" || dumbTerminal() {
		return false
	}
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// utf8Locale reports whether the glyph set may use Unicode. The first of LC_ALL, LC_CTYPE and
// LANG decides; an unset locale on Windows assumes UTF-8.
func utf8Locale() bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return runtime.GOOS == "windows"
}

func internalSkillsEnabled() bool {
	v := os.Getenv("INSTALL_INTERNAL_SKILLS")
	return v == "1" || v == "true"
}
