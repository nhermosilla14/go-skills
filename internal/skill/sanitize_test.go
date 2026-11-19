package skill

import (
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := map[string]string{
		"MySkill":             "myskill",
		"my   skill":          "my-skill",
		"bun.sh":              "bun.sh",
		"v2.0":                "v2.0",
		"my_skill":            "my_skill",
		"skill@#$name":        "skill-name",
		"../etc/passwd":       "etc-passwd",
		"../../secret":        "secret",
		"C:\\Windows\\System": "c-windows-system",
		".hidden":             "hidden",
		"..skill":             "skill",
		"skill..":             "skill",
		"--skill":             "skill",
		"émoji🎉skill":         "moji-skill",
		"vercel/next.js":      "vercel-next.js",
		"":                    "unnamed-skill",
		"...":                 "unnamed-skill",
		"@#$%":                "unnamed-skill",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := SanitizeName(in); got != want {
				t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestSanitizeNameCapsLength(t *testing.T) {
	got := SanitizeName(strings.Repeat("a", 300))
	if len(got) != maxNameLen {
		t.Errorf("len = %d, want %d", len(got), maxNameLen)
	}
}
