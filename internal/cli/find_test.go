package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
	"github.com/nhermosilla14/go-skills/internal/search"
)

func TestWriteFindResults(t *testing.T) {
	results := []search.Result{
		{ID: "o/r/react", Name: "react", Source: "o/r", Installs: 1200},
		{ID: "o/r/one", Name: "one", Source: "o/r", Installs: 1},
		{ID: "o/r/none", Name: "none", Source: "o/r"},
	}
	var out bytes.Buffer
	WriteFindResults(&out, "react", "", search.Response{Results: results, Exact: 3})
	text := out.String()
	want := "Install with npx skills add o/r@react\n" +
		"o/r@react 1.2K installs\n" +
		"└ https://skills.sh/o/r/react\n" +
		"\n" +
		"Install with npx skills add o/r@one\n" +
		"o/r@one 1 install\n" +
		"└ https://skills.sh/o/r/one\n" +
		"\n" +
		"Install with npx skills add o/r@none\n" +
		"o/r@none\n" +
		"└ https://skills.sh/o/r/none\n" +
		"\n"
	if text != want {
		t.Errorf("output =\n%s\nwant:\n%s", text, want)
	}
}

func TestWriteFindResultsPartialDivider(t *testing.T) {
	results := []search.Result{
		{ID: "o/r/exact", Name: "exact", Source: "o/r"},
		{ID: "o/r/partial", Name: "partial", Source: "o/r"},
	}
	for _, tc := range []struct {
		exact int
		want  string
	}{
		{1, `Partial matches (some terms of "a b"):`},
		{0, `No skills matched every term in "a b". Partial matches:`},
	} {
		var out bytes.Buffer
		WriteFindResults(&out, "a b", "", search.Response{Results: results, Exact: tc.exact})
		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("exact %d output misses %q:\n%s", tc.exact, tc.want, out.String())
		}
	}
}

func TestWriteFindResultsNoResults(t *testing.T) {
	var out bytes.Buffer
	WriteFindResults(&out, "react", "", search.Response{})
	if out.String() != "No skills found for \"react\"\n" {
		t.Errorf("output = %q", out.String())
	}
	out.Reset()
	WriteFindResults(&out, "react", "vercel", search.Response{})
	if out.String() != "No skills found for \"react\" from owner \"vercel\"\n" {
		t.Errorf("output = %q", out.String())
	}
}

func TestWriteFindResultSanitizesServerText(t *testing.T) {
	var out bytes.Buffer
	WriteFindResult(&out, search.Result{
		ID:     "o/r/\x1b[31mred",
		Name:   "na\x07me",
		Source: "o\r/r",
	})
	text := out.String()
	for _, bad := range []string{"\x1b", "\x07", "\r"} {
		if strings.Contains(text, bad) {
			t.Errorf("output keeps %q: %q", bad, text)
		}
	}
}

func TestWriteFindTip(t *testing.T) {
	var out bytes.Buffer
	WriteFindTip(&out)
	for _, want := range []string{
		"Tip: if running in a coding agent, follow these steps:",
		"1) npx skills find [query] [--owner <owner>]",
		"2) npx skills add <owner/repo@skill>",
		"Usage: npx skills find <query> [--owner <owner>]",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("tip misses %q:\n%s", want, out.String())
		}
	}
}

func TestWriteFindOffer(t *testing.T) {
	var out bytes.Buffer
	WriteFindOffer(&out, nil)
	if out.Len() != 0 {
		t.Errorf("nil offer wrote %q", out.String())
	}
	out.Reset()
	WriteFindOffer(&out, &app.OfferReport{Declined: true, Dismissed: true})
	if !strings.Contains(out.String(),
		"You can install it later with: npx skills add vercel-labs/skills@find-skills") {
		t.Errorf("declined offer output = %q", out.String())
	}
	out.Reset()
	WriteFindOffer(&out, &app.OfferReport{Warnings: []string{"could not install find-skills"}})
	if !strings.Contains(out.String(), "warning: could not install find-skills") {
		t.Errorf("accepted offer output = %q", out.String())
	}
}
