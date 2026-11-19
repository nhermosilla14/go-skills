package main

import "testing"

func TestInternalSkillsEnabled(t *testing.T) {
	tests := map[string]bool{
		"1":    true,
		"true": true,
		"TRUE": false,
		"yes":  false,
		"0":    false,
		"":     false,
	}
	for value, want := range tests {
		t.Run(value, func(t *testing.T) {
			t.Setenv("INSTALL_INTERNAL_SKILLS", value)
			if got := internalSkillsEnabled(); got != want {
				t.Errorf("internalSkillsEnabled() with %q = %v, want %v", value, got, want)
			}
		})
	}
}
