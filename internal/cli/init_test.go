package cli

import (
	"bytes"
	"testing"

	"github.com/nhermosilla14/go-skills/internal/app"
)

func TestWriteInit(t *testing.T) {
	tests := []struct {
		name string
		rep  app.InitReport
		want string
	}{
		{
			name: "created in the working directory",
			rep:  app.InitReport{Name: "demo", DisplayPath: "SKILL.md"},
			want: "Initialized skill: demo\n\n" +
				"Created:\n  SKILL.md\n\n" +
				"Next steps:\n" +
				"  1. Edit SKILL.md to define your skill instructions\n" +
				"  2. Add supporting files next to SKILL.md and reference them from the instructions.\n",
		},
		{
			name: "created in a subdirectory",
			rep:  app.InitReport{Name: "my-skill", DisplayPath: "my-skill/SKILL.md"},
			want: "Initialized skill: my-skill\n\n" +
				"Created:\n  my-skill/SKILL.md\n\n" +
				"Next steps:\n" +
				"  1. Edit my-skill/SKILL.md to define your skill instructions\n" +
				"  2. Add supporting files next to SKILL.md and reference them from the instructions.\n",
		},
		{
			name: "already exists",
			rep:  app.InitReport{Name: "demo", DisplayPath: "SKILL.md", Exists: true},
			want: "Skill already exists at SKILL.md\n",
		},
		{
			name: "control characters are stripped",
			rep:  app.InitReport{Name: "de\x1bmo", DisplayPath: "de\x1bmo/SKILL.md"},
			want: "Initialized skill: demo\n\n" +
				"Created:\n  demo/SKILL.md\n\n" +
				"Next steps:\n" +
				"  1. Edit demo/SKILL.md to define your skill instructions\n" +
				"  2. Add supporting files next to SKILL.md and reference them from the instructions.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			WriteInit(&out, tc.rep)
			if got := out.String(); got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}
