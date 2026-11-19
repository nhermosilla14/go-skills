// Package skill parses SKILL.md files and discovers the skills in a source directory.
package skill

import (
	"regexp"
	"strings"
)

const (
	maxNameLen   = 255
	fallbackName = "unnamed-skill"
)

var unsafeNameChars = regexp.MustCompile(`[^a-z0-9._]+`)

// SanitizeName turns a skill name into the directory name used for installs: lowercase, runs of
// unsafe characters become one hyphen, leading and trailing dots and hyphens are removed.
func SanitizeName(name string) string {
	s := unsafeNameChars.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, ".-")
	if len(s) > maxNameLen {
		s = s[:maxNameLen]
	}
	if s == "" {
		return fallbackName
	}
	return s
}
