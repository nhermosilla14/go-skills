package skill

import (
	"bytes"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// frontmatterLines splits a frontmatter body into lines, whatever its line endings.
var frontmatterLines = regexp.MustCompile(`\r?\n`)

// nameKey matches a top-level name key. Indented nested keys are left alone.
var nameKey = regexp.MustCompile(`^name\s*:`)

// SetFrontmatterName replaces the frontmatter name of raw with name and returns the new content.
// Everything outside the name value, including the BOM, the line endings, comments and other keys,
// is preserved. A file without frontmatter is returned unchanged.
func SetFrontmatterName(raw []byte, name string) []byte {
	m := frontmatterPattern.FindSubmatchIndex(raw)
	if m == nil {
		return raw
	}
	prefix, body, suffix := raw[:m[2]], raw[m[2]:m[3]], raw[m[3]:]
	eol := "\n"
	if bytes.HasSuffix(prefix, []byte("\r\n")) {
		eol = "\r\n"
	}
	lines := frontmatterLines.Split(string(body), -1)
	line := "name: " + yamlScalar(name)
	if idx := nameLineIndex(lines); idx >= 0 {
		end := idx + 1
		for end < len(lines) && (strings.HasPrefix(lines[end], " ") || strings.HasPrefix(lines[end], "\t")) {
			end++
		}
		lines = append(append(lines[:idx:idx], line), lines[end:]...)
	} else {
		lines = append([]string{line}, lines...)
	}
	out := make([]byte, 0, len(raw)+len(line))
	out = append(out, prefix...)
	out = append(out, strings.Join(lines, eol)...)
	out = append(out, suffix...)
	return out
}

// nameLineIndex returns the index of the first top-level name key, or -1.
func nameLineIndex(lines []string) int {
	for i, line := range lines {
		if nameKey.MatchString(line) {
			return i
		}
	}
	return -1
}

// yamlScalar encodes a value as a YAML scalar, quoting it when YAML would otherwise coerce it.
func yamlScalar(value string) string {
	out, err := yaml.Marshal(map[string]string{"name": value})
	if err != nil {
		return value
	}
	return strings.TrimPrefix(strings.TrimSuffix(string(out), "\n"), "name: ")
}
