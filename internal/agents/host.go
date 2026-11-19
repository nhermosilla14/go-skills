package agents

import "strings"

// Host is the coding agent the CLI runs inside, detected from its environment markers.
type Host struct {
	ID   string // registry id the host installs for
	Name string // display name of that agent, for the informational line
}

// hostMarker is one environment variable that identifies a host agent. A non-empty Value limits
// the marker to that exact value. Weak markers appear inside a terminal a host agent started and
// never identify one on their own.
type hostMarker struct {
	Variable string
	Value    string
	Agent    string
	Weak     bool
}

// hostMarkers is the host-agent table of the ADR: the markers of every supported host, evaluated
// once per run from the injected environment. Order only decides which host wins when several
// markers are present.
var hostMarkers = []hostMarker{
	// Claude Code.
	{Variable: "CLAUDECODE", Agent: "claude-code"},
	{Variable: "CLAUDE_CODE_ENTRYPOINT", Agent: "claude-code"},
	{Variable: "CLAUDE_CODE", Agent: "claude-code"},
	{Variable: "CLAUDE_CODE_IS_COWORK", Agent: "claude-code"},
	// Codex.
	{Variable: "CODEX_SANDBOX", Agent: "codex"},
	{Variable: "CODEX_HOME", Agent: "codex"},
	{Variable: "CODEX_CI", Agent: "codex"},
	{Variable: "CODEX_THREAD_ID", Agent: "codex"},
	// OpenCode.
	{Variable: "OPENCODE", Agent: "opencode"},
	{Variable: "OPENCODE_BIN", Agent: "opencode"},
	{Variable: "OPENCODE_CLIENT", Agent: "opencode"},
	// Gemini CLI.
	{Variable: "GEMINI_CLI", Agent: "gemini-cli"},
	// GitHub Copilot.
	{Variable: "COPILOT_AGENT", Agent: "github-copilot"},
	{Variable: "COPILOT_ALLOW_ALL", Agent: "github-copilot"},
	{Variable: "COPILOT_GITHUB_TOKEN", Agent: "github-copilot"},
	{Variable: "COPILOT_MODEL", Agent: "github-copilot"},
	// Cursor: the trace id alone is its integrated terminal, not an agent run.
	{Variable: "CURSOR_AGENT", Agent: "cursor"},
	{Variable: "CURSOR_EXTENSION_HOST_ROLE", Value: "agent-exec", Agent: "cursor"},
	{Variable: "CURSOR_TRACE_ID", Agent: "cursor", Weak: true},
	// The remaining hosts, including Replit's documented marker.
	{Variable: "ANTIGRAVITY_AGENT", Agent: "antigravity"},
	{Variable: "AUGMENT_AGENT", Agent: "augment"},
	{Variable: "DEVIN_AGENT", Agent: "universal"},
	{Variable: "REPL_ID", Agent: "replit"},
}

// hostNames maps the generic AI_AGENT value, and the host names upstream reports, to registry ids.
// A name this table does not list falls back to ordinary detection.
var hostNames = map[string]string{
	"claude":         "claude-code",
	"claude-code":    "claude-code",
	"cowork":         "claude-code",
	"codex":          "codex",
	"opencode":       "opencode",
	"gemini":         "gemini-cli",
	"gemini-cli":     "gemini-cli",
	"github-copilot": "github-copilot",
	"copilot":        "github-copilot",
	"cursor":         "cursor",
	"cursor-cli":     "cursor",
	"antigravity":    "antigravity",
	"augment":        "augment",
	"augment-cli":    "augment",
	"devin":          "universal",
	"replit":         "replit",
}

// DetectHost reports the coding agent the process runs inside and the agent id it installs for. A
// host whose name maps to no registry agent, and a Cursor terminal without a strong marker, are
// reported as "not an agent" so the caller falls back to the ordinary detection path.
func DetectHost(env Environment) (Host, bool) {
	id := ""
	for _, m := range hostMarkers {
		if m.Weak {
			continue
		}
		value := env.getenv(m.Variable)
		if value == "" || (m.Value != "" && value != m.Value) {
			continue
		}
		id = m.Agent
		break
	}
	if id == "" {
		id = hostNames[strings.ToLower(env.getenv("AI_AGENT"))]
	}
	if id == "" {
		return Host{}, false
	}
	if id == "cursor" && !cursorAgent(env) {
		return Host{}, false
	}
	agent, ok := lookup(id)
	if !ok {
		return Host{}, false
	}
	return Host{ID: agent.ID, Name: agent.DisplayName}, true
}

// cursorAgent reports Cursor's strong markers: CURSOR_TRACE_ID alone is only its terminal.
func cursorAgent(env Environment) bool {
	if env.getenv("CURSOR_AGENT") != "" {
		return true
	}
	return env.getenv("CURSOR_EXTENSION_HOST_ROLE") == "agent-exec"
}

// HostTargets is the target list of an add started by a host agent without --agent: the agent the
// host maps to plus every universal agent that reads the canonical directory. An id outside the
// registry selects nothing.
func HostTargets(id string) []Agent {
	selected, invalid := Expand([]string{id})
	if len(invalid) > 0 || len(selected) == 0 {
		return nil
	}
	return ensureUniversalAgents(selected)
}

// HostVariables lists every variable host detection reads, so a caller that spawns a child process
// can keep the developer's own host markers out of the run.
func HostVariables() []string {
	seen := map[string]bool{"AI_AGENT": true}
	out := []string{"AI_AGENT"}
	for _, m := range hostMarkers {
		if seen[m.Variable] {
			continue
		}
		seen[m.Variable] = true
		out = append(out, m.Variable)
	}
	return out
}
