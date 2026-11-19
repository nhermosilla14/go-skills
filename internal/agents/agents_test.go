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
		{"registry order regardless of input", []string{"opencode", "claude-code"}, []string{"claude-code", "opencode"}, nil},
		{"duplicates collapse", []string{"opencode", "opencode"}, []string{"opencode"}, nil},
		{"wildcard", []string{"*"}, IDs(), nil},
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

func TestRegistryCounts(t *testing.T) {
	all := All()
	if len(all) != 79 {
		t.Fatalf("registry has %d agents, want 79", len(all))
	}
	var universal, autoUniversal, hidden int
	for _, a := range all {
		if a.Universal {
			universal++
		}
		if a.Universal && a.ReadsCanonicalGlobally && !a.NotAutoAdded {
			autoUniversal++
		}
		if a.HiddenFromPrompt {
			hidden++
		}
	}
	if universal != 24 {
		t.Errorf("universal = %d, want 24", universal)
	}
	if autoUniversal != 20 {
		t.Errorf("auto-added universal = %d, want 20", autoUniversal)
	}
	if hidden != 8 {
		t.Errorf("hidden from prompt = %d, want 8", hidden)
	}
	seen := map[string]bool{}
	for _, a := range all {
		if seen[a.ID] {
			t.Errorf("duplicate id %s", a.ID)
		}
		seen[a.ID] = true
	}
}

func TestProjectSkillDirs(t *testing.T) {
	want := map[string]bool{}
	for _, a := range registry {
		if a.ProjectDir != "skills" {
			want[a.ProjectDir] = true
		}
	}
	for _, dir := range legacyProjectSkillDirs {
		want[dir] = true
	}
	got := ProjectSkillDirs()
	if len(got) != len(want) {
		t.Errorf("got %d dirs, want %d", len(got), len(want))
	}
	for i, dir := range got {
		if !want[dir] {
			t.Errorf("unexpected dir %q", dir)
		}
		if i > 0 && got[i-1] >= dir {
			t.Errorf("dirs are not sorted and unique: %q then %q", got[i-1], dir)
		}
	}
	for _, legacy := range legacyProjectSkillDirs {
		if !contains(got, legacy) {
			t.Errorf("legacy dir %q missing", legacy)
		}
	}
	if contains(got, "skills") {
		t.Error(`the bare "skills" dir must not be returned`)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
