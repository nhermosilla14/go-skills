package skill

import (
	"regexp"
	"strings"
)

var nameSeparators = regexp.MustCompile(`[\s_]+`)

// NormalizeName is the lookup form of a skill name: lowercase, with whitespace and underscores
// turned into hyphens.
func NormalizeName(name string) string {
	return nameSeparators.ReplaceAllString(strings.ToLower(name), "-")
}

// Filter returns the skills named by query, in discovery order. It first matches the display name
// case-insensitively; only when that finds nothing does it compare normalized names.
func Filter(all []Skill, query string) []Skill {
	var display []Skill
	for _, s := range all {
		if strings.EqualFold(s.Name, query) {
			display = append(display, s)
		}
	}
	if len(display) > 0 {
		return display
	}
	var normalized []Skill
	want := NormalizeName(query)
	for _, s := range all {
		if NormalizeName(s.Name) == want {
			normalized = append(normalized, s)
		}
	}
	return normalized
}
