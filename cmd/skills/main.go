// Command skills installs agent skills into a project. See docs/ for the behavior it implements.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/cli"
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

func runAdd(opts app.AddOptions, stdout, stderr io.Writer) int {
	workDir, err := os.Getwd()
	if err != nil {
		return reportError(stderr, err)
	}
	env := app.Env{
		WorkDir:         workDir,
		IsTTY:           isTerminal(os.Stdin),
		IncludeInternal: internalSkillsEnabled(),
		Now:             time.Now,
	}
	rep, err := app.Add(opts, env)
	for _, w := range rep.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if err != nil {
		return reportError(stderr, err)
	}
	if opts.List {
		cli.WriteListing(stdout, rep.Listed)
		return exitOK
	}
	cli.WriteReport(stderr, rep)
	if rep.Failed() {
		return exitFailure
	}
	return exitOK
}

// reportError prints err to stderr and returns the exit code for it.
func reportError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "error: %v\n", err)
	var usage *cli.UsageError
	if errors.As(err, &usage) || errors.Is(err, app.ErrUsage) {
		fmt.Fprintln(stderr, "Run 'skills --help' for usage.")
		return exitUsage
	}
	return exitFailure
}

// isTerminal reports whether f is a character device, which is how a terminal shows up.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func internalSkillsEnabled() bool {
	v := os.Getenv("INSTALL_INTERNAL_SKILLS")
	return v == "1" || v == "true"
}
