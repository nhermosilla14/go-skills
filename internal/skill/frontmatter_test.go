package skill

import "testing"

func TestSetFrontmatterName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		to   string
		want string
	}{
		{
			name: "replaces the name line",
			in:   "---\nname: Alpha\ndescription: d\nlicense: MIT\n---\nbody\n",
			to:   "beta",
			want: "---\nname: beta\ndescription: d\nlicense: MIT\n---\nbody\n",
		},
		{
			name: "keeps CRLF",
			in:   "---\r\nname: Alpha\r\ndescription: d\r\n---\r\nbody",
			to:   "beta",
			want: "---\r\nname: beta\r\ndescription: d\r\n---\r\nbody",
		},
		{
			name: "keeps the BOM",
			in:   "\uFEFF---\nname: Alpha\ndescription: d\n---\nbody",
			to:   "beta",
			want: "\uFEFF---\nname: beta\ndescription: d\n---\nbody",
		},
		{
			name: "keeps comments and other keys",
			in:   "---\n# a comment\nlicense: MIT\nname: Alpha\nmetadata:\n  internal: true\n---\nbody\n",
			to:   "beta",
			want: "---\n# a comment\nlicense: MIT\nname: beta\nmetadata:\n  internal: true\n---\nbody\n",
		},
		{
			name: "drops block scalar continuations",
			in:   "---\nname: >\n  alpha\n  skill\ndescription: d\n---\nbody\n",
			to:   "beta",
			want: "---\nname: beta\ndescription: d\n---\nbody\n",
		},
		{
			name: "replaces a quoted name",
			in:   "---\nname: \"Alpha Skill\"\ndescription: d\n---\n",
			to:   "beta",
			want: "---\nname: beta\ndescription: d\n---\n",
		},
		{
			name: "prepends when missing",
			in:   "---\ndescription: d\n---\nbody\n",
			to:   "beta",
			want: "---\nname: beta\ndescription: d\n---\nbody\n",
		},
		{
			name: "quotes values YAML would coerce",
			in:   "---\nname: Alpha\ndescription: d\n---\n",
			to:   "123",
			want: "---\nname: \"123\"\ndescription: d\n---\n",
		},
		{
			name: "leaves a nested name alone",
			in:   "---\ndescription: d\nmetadata:\n  name: nested\n---\n",
			to:   "beta",
			want: "---\nname: beta\ndescription: d\nmetadata:\n  name: nested\n---\n",
		},
		{
			name: "no frontmatter is unchanged",
			in:   "# body only\n",
			to:   "beta",
			want: "# body only\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(SetFrontmatterName([]byte(tc.in), tc.to)); got != tc.want {
				t.Errorf("SetFrontmatterName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetFrontmatterNameIsIdempotent(t *testing.T) {
	in := []byte("\uFEFF---\r\nname: Alpha\r\ndescription: d\r\n---\r\nbody")
	once := SetFrontmatterName(in, "beta")
	twice := SetFrontmatterName(once, "beta")
	if string(once) != string(twice) {
		t.Errorf("second rewrite changed the file: %q then %q", once, twice)
	}
}
