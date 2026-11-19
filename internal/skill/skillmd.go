package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// frontmatterPattern is the published delimiter contract: an optional BOM, a "---" line, then the
// first following "---" line closes the block. Other openers (---js, ---yaml) are not frontmatter.
var frontmatterPattern = regexp.MustCompile(`^\x{FEFF}?---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$`)

var lineBreaks = regexp.MustCompile(`[\r\n]+`)

// Meta is the validated frontmatter of a SKILL.md file.
type Meta struct {
	Name        string
	Description string
	Internal    bool
}

// ParseSkillMD validates the frontmatter of a SKILL.md file. It requires string fields name and
// description, both non-empty after sanitizing.
func ParseSkillMD(raw []byte) (Meta, error) {
	m := frontmatterPattern.FindSubmatch(raw)
	if m == nil {
		return Meta{}, errors.New("missing required frontmatter field(s): name, description")
	}
	fm, err := decodeFrontmatter(m[1])
	if err != nil {
		return Meta{}, err
	}
	name, err := requiredText(fm, "name")
	if err != nil {
		return Meta{}, err
	}
	description, err := requiredText(fm, "description")
	if err != nil {
		return Meta{}, err
	}
	return Meta{Name: name, Description: description, Internal: isInternal(fm)}, nil
}

// decodeFrontmatter parses one YAML mapping. Multiple documents are refused, as upstream does.
func decodeFrontmatter(block []byte) (map[string]any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(block))
	var fm map[string]any
	if err := dec.Decode(&fm); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid frontmatter: multiple YAML documents")
	}
	return fm, nil
}

// requiredText returns the sanitized string value of a required frontmatter field.
func requiredText(fm map[string]any, key string) (string, error) {
	v, ok := fm[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required frontmatter field(s): %s", key)
	}
	s, isString := v.(string)
	if !isString {
		return "", fmt.Errorf("frontmatter field(s) must be strings: %s", key)
	}
	text := sanitizeText(s)
	if text == "" {
		return "", fmt.Errorf("empty required frontmatter field(s): %s", key)
	}
	return text, nil
}

// isInternal reports whether metadata.internal is true, the string "true" or the string "1".
// Other spellings are not internal, matching upstream.
func isInternal(fm map[string]any) bool {
	meta, ok := fm["metadata"].(map[string]any)
	if !ok {
		return false
	}
	switch v := meta["internal"].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	}
	return false
}

// sanitizeText makes frontmatter text safe to print: line breaks become spaces, control characters
// (including the escape character of terminal sequences) are dropped, and the ends are trimmed.
func sanitizeText(s string) string {
	s = lineBreaks.ReplaceAllString(s, " ")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}
