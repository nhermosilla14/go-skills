// Package tui implements the interactive add flow: one inline Bubble Tea program that asks every
// decision before anything is installed. It renders on the human stream only.
package tui

import (
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

const (
	// minWidth is the design minimum; below it the detail pane is dropped.
	minWidth = 40
	// tinyWidth is where descriptions and group headings are dropped too.
	tinyWidth = 24
	// maxRows caps the visible list rows, as upstream does.
	maxRows = 20
	// defaultWidth and defaultHeight are used until a window size arrives.
	defaultWidth  = 80
	defaultHeight = 24
)

// theme holds the glyph set and the styles of one rendering. Color is decided by the caller from
// the human stream, NO_COLOR and TERM; the theme never probes the terminal.
type theme struct {
	color bool
	ascii bool
	r     *lipgloss.Renderer
}

func newTheme(w io.Writer, color, ascii bool) theme {
	return theme{color: color, ascii: ascii, r: lipgloss.NewRenderer(w)}
}

// sanitize neutralizes untrusted text before it is measured: control characters are dropped and
// tabs would otherwise break the width arithmetic.
func sanitize(s string) string {
	return strings.ReplaceAll(skill.SanitizeDisplay(s), "\t", " ")
}

func (t theme) bold(s string) string {
	if !t.color {
		return s
	}
	return t.r.NewStyle().Bold(true).Render(s)
}

func (t theme) faint(s string) string {
	if !t.color {
		return s
	}
	return t.r.NewStyle().Faint(true).Render(s)
}

func (t theme) accent(s string) string {
	if !t.color {
		return s
	}
	return t.r.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Render(s)
}

// cursor is the marker in front of the focused row.
func (t theme) cursor() string {
	if t.ascii {
		return ">"
	}
	return "❯"
}

// rule is the horizontal separator across the layout.
func (t theme) rule(width int) string {
	if width < 0 {
		width = 0
	}
	glyph := "─"
	if t.ascii {
		glyph = "-"
	}
	return strings.Repeat(glyph, width)
}

// ellipsis is appended to truncated text.
func (t theme) ellipsis() string {
	if t.ascii {
		return "..."
	}
	return "…"
}

// width reports the printed width of s in cells.
func (t theme) width(s string) int {
	return lipgloss.Width(s)
}

// cut truncates s to width cells, adding the ellipsis when it had to cut. Text is sanitized and
// unstyled here on purpose: widths are measured on what is printed.
func (t theme) cut(s string, width int) string {
	s = sanitize(s)
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	mark := t.ellipsis()
	if width <= lipgloss.Width(mark) {
		// Too narrow for the ellipsis: keep only the cells that fit.
		runes := []rune(s)
		for n := len(runes); n > 0; n-- {
			if lipgloss.Width(string(runes[:n])) <= width {
				return string(runes[:n])
			}
		}
		return ""
	}
	runes := []rune(s)
	for n := len(runes) - 1; n >= 0; n-- {
		candidate := string(runes[:n])
		if lipgloss.Width(candidate)+lipgloss.Width(mark) <= width {
			return candidate + mark
		}
	}
	return mark
}

// wrap breaks s at word boundaries into at most maxLines lines of width cells.
func (t theme) wrap(s string, width, maxLines int) []string {
	words := strings.Fields(sanitize(s))
	var lines []string
	current := ""
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		if lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		if len(lines) == maxLines {
			return lines
		}
		current = word
	}
	if current != "" && len(lines) < maxLines {
		lines = append(lines, current)
	}
	for i := range lines {
		lines[i] = t.cut(lines[i], width)
	}
	return lines
}
