package main

import (
	"context"
	"io"
	"os"

	"github.com/nhermosilla14/go-skills/internal/cli"
	"github.com/nhermosilla14/go-skills/internal/cli/tui"
	"github.com/nhermosilla14/go-skills/internal/search"
)

// runFind runs `find`: a keyword search and its report, or the interactive prompt when there is no
// query and a terminal. Nothing is written to disk.
func runFind(opts cli.FindOptions, stdout, stderr io.Writer) int {
	env, err := projectEnv()
	if err != nil {
		return reportError(stderr, err)
	}
	client := search.New(os.Getenv("SKILLS_API_URL"))
	if opts.Query != "" {
		res, err := client.Search(context.Background(), opts.Query, opts.Owner)
		if err != nil {
			return reportError(stderr, err)
		}
		cli.WriteFindResults(stdout, opts.Query, opts.Owner, res)
		return exitOK
	}
	if !env.IsTTY {
		cli.WriteFindTip(stdout)
		return exitOK
	}
	result, found, err := tui.RunSearch(os.Stdin, stderr, tui.Options{
		Color: colorEnabled(),
		ASCII: !utf8Locale(),
	}, func(query string) ([]search.Result, error) {
		res, err := client.Search(context.Background(), query, opts.Owner)
		if err != nil {
			return nil, err
		}
		return res.Results, nil
	})
	if err != nil {
		return reportError(stderr, err)
	}
	if !found {
		return exitOK
	}
	cli.WriteFindResult(stdout, result)
	return exitOK
}
