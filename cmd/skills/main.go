// Command skills installs agent skills into a project. See docs/ for the behavior it implements.
package main

import (
	"context"
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
	"github.com/nhermosilla14/go-skills/internal/skill"
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
	case cli.KindList:
		return runList(cmd, stdout, stderr)
	case cli.KindCheck:
		return runCheck(cmd.Check, stdout, stderr)
	case cli.KindRemove:
		return runRemove(cmd.Remove, stderr)
	case cli.KindUpdate:
		return runUpdate(cmd.Update, stderr)
	case cli.KindRestore:
		return runRestore(cmd.Restore, stderr)
	case cli.KindUse:
		return runUse(cmd, stdout, stderr)
	case cli.KindInit:
		return runInit(cmd.Init, stderr)
	case cli.KindFind:
		return runFind(cmd.Find, stdout, stderr)
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
	env, err := projectEnv()
	if err != nil {
		document = cli.JSONFailure(err)
		return reportError(stderr, err)
	}
	// Host-agent mode makes the run non-interactive, so --json is valid without -y here.
	opts = app.ApplyHost(opts, env)
	if err := cli.ValidateJSON(opts); err != nil {
		document = cli.JSONFailure(err)
		fmt.Fprintf(stderr, "error: %s\n", cli.Message(err))
		return exitFailure
	}
	if env.Host.ID != "" {
		fmt.Fprintf(stderr, "%s Agent detected — installing non-interactively\n", env.Host.Name)
	}
	interactive := env.IsTTY
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
	cli.WriteFindOffer(stderr, rep.Offer)
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

// runList runs `list`, which is the product of the command: the human report or the JSON document
// goes to stdout and everything else to stderr.
func runList(cmd cli.Command, stdout, stderr io.Writer) (code int) {
	var document []cli.ListEntry
	if cmd.JSON {
		defer func() {
			if err := cli.WriteListJSON(stdout, document); err != nil {
				fmt.Fprintf(stderr, "error: %v\n", err)
				code = exitFailure
			}
		}()
	}
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	rep, err := app.List(cmd.List, env)
	for _, warning := range rep.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", warning)
	}
	if err != nil {
		return reportError(stderr, err)
	}
	if cmd.JSON {
		document = cli.ListJSON(rep)
		return exitOK
	}
	cli.WriteList(stdout, rep)
	return exitOK
}

// runCheck runs `check`, reporting findings on stdout and exiting non-zero when there are any.
func runCheck(opts app.CheckOptions, stdout, stderr io.Writer) int {
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	rep, err := app.Check(context.Background(), opts, env)
	if err != nil {
		return reportError(stderr, err)
	}
	cli.WriteCheck(stdout, rep)
	if rep.Failed() {
		return exitFailure
	}
	return exitOK
}

// runUpdate runs `update`, whose progress and result summary go to the human stream.
func runUpdate(opts app.UpdateOptions, stderr io.Writer) int {
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	prompter := app.Prompter(app.NoPrompts{})
	if env.IsTTY {
		prompter = tui.New(os.Stdin, stderr, tui.Options{Color: colorEnabled(), ASCII: !utf8Locale()})
	}
	rep, err := app.Update(opts, env, prompter)
	if err != nil {
		if errors.Is(err, app.ErrCancelled) {
			fmt.Fprintln(stderr, "Cancelled")
			if env.IsTTY {
				return exitOK
			}
			return exitFailure
		}
		return reportError(stderr, err)
	}
	cli.WriteUpdate(stderr, rep)
	if rep.Failed() {
		return exitFailure
	}
	return exitOK
}

// runRestore runs `install` without a source, which rebuilds the project from skills-lock.json.
// Restore never prompts, so it takes no prompter.
func runRestore(opts app.RestoreOptions, stderr io.Writer) int {
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	rep, err := app.Restore(opts, env)
	if err != nil {
		return reportError(stderr, err)
	}
	cli.WriteRestore(stderr, rep)
	if rep.Failed() {
		return exitFailure
	}
	return exitOK
}

// runUse runs `use`: it prints the prompt on stdout, or hands it to an agent and propagates the
// agent's exit code. Validation failures go to stderr with exit 1.
func runUse(cmd cli.Command, stdout, stderr io.Writer) int {
	if cmd.UseHelp {
		fmt.Fprint(stdout, cli.UseHelp())
		return exitOK
	}
	if errs := cli.ValidateUse(cmd.Use); len(errs) > 0 {
		for _, msg := range errs {
			fmt.Fprintf(stderr, "error: %s\n", msg)
		}
		if cmd.Use.Source == "" {
			fmt.Fprint(stderr, cli.UseHelp())
		}
		return exitFailure
	}
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	opts := app.UseOptions{Source: cmd.Use.Source, FullDepth: cmd.Use.FullDepth}
	if len(cmd.Use.Skills) == 1 {
		opts.Skill = cmd.Use.Skills[0]
	}
	if len(cmd.Use.Agents) == 0 {
		if _, err := app.Use(opts, env, stdout); err != nil {
			return reportError(stderr, err)
		}
		return exitOK
	}
	agent, ok := cli.ResolveUseAgent(cmd.Use.Agents[0])
	if !ok {
		return reportError(stderr, fmt.Errorf("unknown agent %q", cmd.Use.Agents[0]))
	}
	rep, err := app.Use(opts, env, io.Discard)
	if err != nil {
		return reportError(stderr, err)
	}
	code, err := cli.LaunchAgent(agent, rep.Prompt, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		var launch *cli.LaunchError
		if errors.As(err, &launch) {
			fmt.Fprintln(stderr, launch.Error())
			return exitFailure
		}
		return reportError(stderr, err)
	}
	if code != 0 {
		return code
	}
	return exitOK
}

// runInit runs `init`, whose human output goes to stderr; nothing is written to stdout.
func runInit(opts app.InitOptions, stderr io.Writer) int {
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	rep, err := app.Init(opts, env)
	if err != nil {
		return reportError(stderr, err)
	}
	cli.WriteInit(stderr, rep)
	return exitOK
}

// runRemove runs `remove`, whose progress and result summary go to the human stream.
func runRemove(opts app.RemoveOptions, stderr io.Writer) int {
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	prompter := app.Prompter(app.NoPrompts{})
	if env.IsTTY {
		prompter = tui.New(os.Stdin, stderr, tui.Options{Color: colorEnabled(), ASCII: !utf8Locale()})
	}
	rep, err := app.Remove(opts, env, prompter)
	if err != nil {
		for _, note := range rep.Notes {
			fmt.Fprintf(stderr, "note: %s\n", skill.SanitizeDisplay(note))
		}
		if errors.Is(err, app.ErrCancelled) {
			fmt.Fprintln(stderr, "Removal cancelled")
			if env.IsTTY {
				return exitOK
			}
			return exitFailure
		}
		return reportError(stderr, err)
	}
	cli.WriteRemove(stderr, rep)
	if rep.Failed() {
		return exitFailure
	}
	return exitOK
}

// projectEnv builds the use-case environment from the process. Host detection runs once here, from
// the same environment the paths and agent detection use.
func projectEnv() (app.Env, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return app.Env{}, err
	}
	agentEnv := agents.FromOS(workDir)
	host, _ := agents.DetectHost(agentEnv)
	return app.Env{
		WorkDir:         workDir,
		IsTTY:           canPrompt(),
		IncludeInternal: internalSkillsEnabled(),
		Now:             time.Now,
		AgentEnv:        agentEnv,
		Host:            host,
	}, nil
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
