package skill

import "testing"

func TestReduceFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "keeps the whitelist as nested YAML",
			in:   "---\nname: Alpha\ndescription: d\nlicense: MIT\ncompatibility: \">=1\"\nversion: 2\nallowed-tools: bash\nmetadata:\n  internal: false\n---\n\nbody\n",
			want: "---\nname: Alpha\ndescription: d\nlicense: MIT\ncompatibility: '>=1'\nversion: 2\nmetadata:\n    internal: false\n---\nbody\n",
		},
		{
			name: "drops every other key",
			in:   "---\nname: Alpha\ndescription: d\nallowed-tools: bash\ncontext: [a]\n---\nbody\n",
			want: "---\nname: Alpha\ndescription: d\n---\nbody\n",
		},
		{
			name: "removes the block when nothing survives",
			in:   "---\nallowed-tools: bash\n---\n\nbody\n",
			want: "body\n",
		},
		{
			name: "keeps non-string compatibility",
			in:   "---\nname: Alpha\ncompatibility:\n  - a\n  - b\n---\nbody\n",
			want: "---\nname: Alpha\ncompatibility:\n    - a\n    - b\n---\nbody\n",
		},
		{
			name: "drops non-string name and metadata arrays",
			in:   "---\nname: 3\nmetadata: [a]\ndescription: d\n---\nbody\n",
			want: "---\ndescription: d\n---\nbody\n",
		},
		{
			name: "file without frontmatter is unchanged",
			in:   "body only\n",
			want: "body only\n",
		},
		{
			name: "empty frontmatter leaves the body",
			in:   "---\n\n---\n\nbody\n",
			want: "body\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(ReduceFrontmatter([]byte(tc.in))); got != tc.want {
				t.Errorf("ReduceFrontmatter = %q, want %q", got, tc.want)
			}
		})
	}
}
