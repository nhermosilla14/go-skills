package agents

import (
	"reflect"
	"testing"
)

// hostEnv is a fake environment with the given variables set.
func hostEnv(t *testing.T, vars map[string]string) Environment {
	t.Helper()
	env := newFakeEnv(t)
	for name, value := range vars {
		env.Vars[name] = value
	}
	return env
}

// TestDetectHostTableRowPerMarker walks the table: every strong marker alone identifies its agent,
// and every weak one identifies nothing.
func TestDetectHostTableRowPerMarker(t *testing.T) {
	for _, marker := range hostMarkers {
		t.Run(marker.Variable+"/"+marker.Value, func(t *testing.T) {
			value := marker.Value
			if value == "" {
				value = "1"
			}
			host, ok := DetectHost(hostEnv(t, map[string]string{marker.Variable: value}))
			if marker.Weak {
				if ok {
					t.Fatalf("weak marker %s detected %+v", marker.Variable, host)
				}
				return
			}
			if !ok || host.ID != marker.Agent {
				t.Fatalf("host = %+v, ok = %v, want %s", host, ok, marker.Agent)
			}
			if host.Name == "" {
				t.Errorf("host %s has no display name", host.ID)
			}
		})
	}
}

// TestDetectHostNamedMarkers covers the values the table deliberately limits.
func TestDetectHostNamedMarkers(t *testing.T) {
	tests := []struct {
		vars map[string]string
		want string // empty means no host
	}{
		{map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "agent-exec"}, "cursor"},
		{map[string]string{"CURSOR_EXTENSION_HOST_ROLE": "terminal"}, ""},
		{map[string]string{"CURSOR_EXTENSION_HOST_ROLE": ""}, ""},
		{map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, "claude-code"},
		{map[string]string{"CODEX_SANDBOX": "danger-full-access"}, "codex"},
		{map[string]string{"DEVIN_AGENT": "1"}, "universal"},
		{map[string]string{"REPL_ID": "abc"}, "replit"},
		{map[string]string{"OPENCODE": " "}, ""},
	}
	for _, tc := range tests {
		host, ok := DetectHost(hostEnv(t, tc.vars))
		if tc.want == "" {
			if ok {
				t.Errorf("%v detected %+v, want no host", tc.vars, host)
			}
			continue
		}
		if !ok || host.ID != tc.want {
			t.Errorf("%v host = %+v, ok = %v, want %s", tc.vars, host, ok, tc.want)
		}
	}
}

// TestDetectHostCursorDemotion proves the integrated terminal is not an agent run.
func TestDetectHostCursorDemotion(t *testing.T) {
	alone, ok := DetectHost(hostEnv(t, map[string]string{"CURSOR_TRACE_ID": "trace"}))
	if ok {
		t.Fatalf("a bare CURSOR_TRACE_ID detected %+v", alone)
	}
	strong, ok := DetectHost(hostEnv(t, map[string]string{
		"CURSOR_TRACE_ID": "trace", "CURSOR_AGENT": "1",
	}))
	if !ok || strong.ID != "cursor" {
		t.Fatalf("host = %+v, ok = %v, want cursor", strong, ok)
	}
	naming, ok := DetectHost(hostEnv(t, map[string]string{"CURSOR_TRACE_ID": "trace", "AI_AGENT": "cursor"}))
	if ok {
		t.Fatalf("AI_AGENT=cursor with only a trace id detected %+v", naming)
	}
}

// TestDetectHostUnmappedHostFallsBack covers the generic marker: a name this build maps is
// detected, and one it cannot map leaves the run on the ordinary detection path.
func TestDetectHostUnmappedHostFallsBack(t *testing.T) {
	for name, want := range map[string]string{
		"opencode": "opencode", "claude": "claude-code", "augment-cli": "augment",
		"devin": "universal", "gemini": "gemini-cli",
		"": "", "unknown-agent": "", "  ": "", "cursor": "",
	} {
		host, ok := DetectHost(hostEnv(t, map[string]string{"AI_AGENT": name}))
		if want == "" {
			if ok {
				t.Errorf("AI_AGENT=%q detected %+v, want the fallback", name, host)
			}
			continue
		}
		if !ok || host.ID != want {
			t.Errorf("AI_AGENT=%q host = %+v, ok = %v, want %s", name, host, ok, want)
		}
	}
}

// TestDetectHostTargets proves the add target set: the host agent plus every universal agent that
// reads the canonical directory, and nothing that is named-only or reads its own global home.
func TestDetectHostTargets(t *testing.T) {
	targets := HostTargets("opencode")
	if len(targets) == 0 || targets[0].ID != "opencode" {
		t.Fatalf("targets = %v, want opencode first", idsOf(targets))
	}
	have := map[string]bool{}
	for _, a := range targets {
		have[a.ID] = true
	}
	for _, id := range []string{"replit", "universal", "antigravity", "antigravity-cli", "eve"} {
		if have[id] {
			t.Errorf("%s in the host target set", id)
		}
	}
	for _, id := range []string{"amp", "codex", "deepagents"} {
		if !have[id] {
			t.Errorf("universal agent %s missing from the host target set", id)
		}
	}
	if got := HostTargets("not-an-agent"); got != nil {
		t.Errorf("Targets of an unknown id = %v, want none", idsOf(got))
	}
}

// TestHostVariablesCoverEveryMarker keeps the child-process filter in step with the table.
func TestHostVariablesCoverEveryMarker(t *testing.T) {
	listed := map[string]bool{}
	for _, name := range HostVariables() {
		if listed[name] {
			t.Errorf("%s listed twice", name)
		}
		listed[name] = true
	}
	if !listed["AI_AGENT"] {
		t.Error("AI_AGENT is not listed")
	}
	for _, marker := range hostMarkers {
		if !listed[marker.Variable] {
			t.Errorf("%s is not listed", marker.Variable)
		}
	}
}

func idsOf(list []Agent) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

// TestHostTargetsAreTheAutoUniversalSet pins the exact target list shape.
func TestHostTargetsAreTheAutoUniversalSet(t *testing.T) {
	base, _ := Expand([]string{"opencode"})
	want := idsOf(ensureUniversalAgents(base))
	got := idsOf(HostTargets("opencode"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("targets = %v, want %v", got, want)
	}
}
