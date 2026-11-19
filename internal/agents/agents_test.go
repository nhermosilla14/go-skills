package agents

import (
	"reflect"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name        string
		ids         []string
		wantIDs     []string
		wantInvalid []string
	}{
		{"single", []string{"claude-code"}, []string{"claude-code"}, nil},
		{"registry order regardless of input", []string{"claude-code", "opencode"}, []string{"opencode", "claude-code"}, nil},
		{"duplicates collapse", []string{"opencode", "opencode"}, []string{"opencode"}, nil},
		{"wildcard", []string{"*"}, []string{"opencode", "claude-code"}, nil},
		{"unknown reported", []string{"claude_code", "opencode"}, []string{"opencode"}, []string{"claude_code"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, invalid := Expand(tc.ids)
			var gotIDs []string
			for _, a := range got {
				gotIDs = append(gotIDs, a.ID)
			}
			if !reflect.DeepEqual(gotIDs, tc.wantIDs) {
				t.Errorf("ids = %v, want %v", gotIDs, tc.wantIDs)
			}
			if !reflect.DeepEqual(invalid, tc.wantInvalid) {
				t.Errorf("invalid = %v, want %v", invalid, tc.wantInvalid)
			}
		})
	}
}

func TestProjectDirs(t *testing.T) {
	want := map[string]string{"opencode": ".agents/skills", "claude-code": ".claude/skills"}
	for _, a := range All() {
		if want[a.ID] != a.ProjectDir {
			t.Errorf("%s ProjectDir = %q, want %q", a.ID, a.ProjectDir, want[a.ID])
		}
	}
}
