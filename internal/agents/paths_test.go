package agents

import (
	"path/filepath"
	"strings"
	"testing"
)

// pathRow is one line of olddocs/behavior/05-agents-and-paths.md §2. Templates use {home},
// {config} and the variables' resolved base directories.
type pathRow struct {
	id      string
	project string
	global  string
}

var pathRows = []pathRow{
	{"aider-desk", ".aider-desk/skills", "{home}/.aider-desk/skills"},
	{"amp", ".agents/skills", "{config}/agents/skills"},
	{"antigravity", ".agents/skills", "{home}/.gemini/config/skills"},
	{"antigravity-cli", ".agents/skills", "{home}/.gemini/config/skills"},
	{"astrbot", "data/skills", "{home}/.astrbot/data/skills"},
	{"autohand-code", ".autohand/skills", "{autohand}/skills"},
	{"augment", ".augment/skills", "{home}/.augment/skills"},
	{"bob", ".bob/skills", "{home}/.bob/skills"},
	{"claude-code", ".claude/skills", "{claude}/skills"},
	{"openclaw", "skills", "{home}/.openclaw/skills"},
	{"cline", ".agents/skills", "{home}/.agents/skills"},
	{"codearts-agent", ".codeartsdoer/skills", "{home}/.codeartsdoer/skills"},
	{"codebuddy", ".codebuddy/skills", "{home}/.codebuddy/skills"},
	{"codemaker", ".codemaker/skills", "{home}/.codemaker/skills"},
	{"codestudio", ".codestudio/skills", "{home}/.codestudio/skills"},
	{"codex", ".agents/skills", "{home}/.agents/skills"},
	{"command-code", ".commandcode/skills", "{home}/.commandcode/skills"},
	{"continue", ".continue/skills", "{home}/.continue/skills"},
	{"cortex", ".cortex/skills", "{home}/.snowflake/cortex/skills"},
	{"crush", ".crush/skills", "{home}/.config/crush/skills"},
	{"cursor", ".agents/skills", "{home}/.cursor/skills"},
	{"deepagents", ".agents/skills", "{home}/.deepagents/agent/skills"},
	{"devin", ".devin/skills", "{config}/devin/skills"},
	{"dexto", ".agents/skills", "{home}/.agents/skills"},
	{"droid", ".agents/skills", "{home}/.factory/skills"},
	{"eve", "agent/skills", ""},
	{"firebender", ".agents/skills", "{home}/.firebender/skills"},
	{"forgecode", ".forge/skills", "{home}/.forge/skills"},
	{"fx", ".agents/skills", "{home}/.agents/skills"},
	{"gemini-cli", ".agents/skills", "{home}/.gemini/skills"},
	{"github-copilot", ".agents/skills", "{home}/.copilot/skills"},
	{"goose", ".goose/skills", "{config}/goose/skills"},
	{"grok", ".grok/skills", "{grok}/skills"},
	{"hermes-agent", ".hermes/skills", "{hermes}/skills"},
	{"inference-sh", ".inferencesh/skills", "{home}/.inferencesh/skills"},
	{"iflow-cli", ".iflow/skills", "{home}/.iflow/skills"},
	{"jazz", ".jazz/skills", "{home}/.jazz/skills"},
	{"junie", ".junie/skills", "{home}/.junie/skills"},
	{"kilo", ".agents/skills", "{home}/.kilo/skills"},
	{"kimchi", ".kimchi/skills", "{home}/.config/kimchi/harness/skills"},
	{"kimi-code-cli", ".agents/skills", "{home}/.agents/skills"},
	{"kiro-cli", ".kiro/skills", "{home}/.kiro/skills"},
	{"kode", ".kode/skills", "{home}/.kode/skills"},
	{"lingma", ".lingma/skills", "{home}/.lingma/skills"},
	{"loaf", ".agents/skills", "{home}/.agents/skills"},
	{"mcpjam", ".mcpjam/skills", "{home}/.mcpjam/skills"},
	{"minimax-code", ".minimax/skills", "{home}/.minimax/skills"},
	{"mistral-vibe", ".vibe/skills", "{vibe}/skills"},
	{"moxby", ".moxby/skills", "{home}/.moxby/skills"},
	{"mux", ".mux/skills", "{home}/.mux/skills"},
	{"neovate", ".neovate/skills", "{home}/.neovate/skills"},
	{"opencode", ".agents/skills", "{config}/opencode/skills"},
	{"openhands", ".openhands/skills", "{home}/.openhands/skills"},
	{"ona", ".ona/skills", "{home}/.ona/skills"},
	{"pi", ".agents/skills", "{home}/.agents/skills"},
	{"pochi", ".pochi/skills", "{home}/.pochi/skills"},
	{"posit-assistant", ".posit/assistant/skills", "{home}/.posit/assistant/skills"},
	{"promptscript", ".agents/skills", ""},
	{"qoder", ".qoder/skills", "{home}/.qoder/skills"},
	{"qoder-cn", ".qoder/skills", "{home}/.qoder-cn/skills"},
	{"qwen-code", ".qwen/skills", "{home}/.qwen/skills"},
	{"reasonix", ".reasonix/skills", "{reasonix}/skills"},
	{"replit", ".agents/skills", "{config}/agents/skills"},
	{"rovodev", ".rovodev/skills", "{home}/.rovodev/skills"},
	{"roo", ".roo/skills", "{home}/.roo/skills"},
	{"sarvam-code", ".agents/skills", "{home}/.agents/skills"},
	{"tabnine-cli", ".tabnine/agent/skills", "{home}/.tabnine/agent/skills"},
	{"terramind", ".terramind/skills", "{home}/.terramind/skills"},
	{"tinycloud", ".tinycloud/skills", "{home}/.tinycloud/skills"},
	{"trae", ".trae/skills", "{home}/.trae/skills"},
	{"trae-cn", ".trae/skills", "{home}/.trae-cn/skills"},
	{"universal", ".agents/skills", "{config}/agents/skills"},
	{"warp", ".agents/skills", "{home}/.agents/skills"},
	{"windsurf", ".windsurf/skills", "{home}/.codeium/windsurf/skills"},
	{"zed", ".agents/skills", "{home}/.agents/skills"},
	{"zcode", ".zcode/skills", "{home}/.zcode/skills"},
	{"zencoder", ".zencoder/skills", "{home}/.zencoder/skills"},
	{"zenflow", ".zencoder/skills", "{home}/.zencoder/skills"},
	{"adal", ".adal/skills", "{home}/.adal/skills"},
}

func testEnvironments() map[string]Environment {
	return map[string]Environment{
		"linux":  {GOOS: "linux", Home: "/home/u", Cwd: "/work", Vars: map[string]string{}},
		"darwin": {GOOS: "darwin", Home: "/Users/u", Cwd: "/Users/u/project", Vars: map[string]string{}},
		"windows": {GOOS: "windows", Home: "C:/Users/u", Cwd: "C:/work", Vars: map[string]string{
			"APPDATA": "C:/Users/u/AppData/Roaming",
		}},
		"overrides": {GOOS: "linux", Home: "/home/u", Cwd: "/work", Vars: map[string]string{
			"XDG_CONFIG_HOME":         "/xdg",
			"APPDATA":                 "/appdata",
			"FLATPAK_XDG_CONFIG_HOME": "/flatpak",
			"CODEX_HOME":              "/codex",
			"CLAUDE_CONFIG_DIR":       "/claude",
			"VIBE_HOME":               "/vibe",
			"HERMES_HOME":             "/hermes",
			"AUTOHAND_HOME":           "/autohand",
			"GROK_HOME":               "/grok",
			"SARVAM_HOME":             "/sarvam",
			"REASONIX_HOME":           "/reasonix",
		}},
	}
}

func expandTemplate(template string, env Environment) string {
	orHome := func(name, fallback string) string {
		if v := strings.TrimSpace(env.Vars[name]); v != "" {
			return v
		}
		return join(env.Home, fallback)
	}
	reasonix := join(env.Home, ".reasonix")
	if v := strings.TrimSpace(env.Vars["REASONIX_HOME"]); v != "" {
		reasonix = v
	} else if env.GOOS == "windows" {
		if appData := strings.TrimSpace(env.Vars["APPDATA"]); appData != "" {
			reasonix = join(appData, "reasonix")
		} else {
			reasonix = join(env.Home, "AppData/Roaming/reasonix")
		}
	}
	replacer := strings.NewReplacer(
		"{home}", env.Home,
		"{config}", env.ConfigHome(),
		"{claude}", orHome("CLAUDE_CONFIG_DIR", ".claude"),
		"{vibe}", orHome("VIBE_HOME", ".vibe"),
		"{hermes}", orHome("HERMES_HOME", ".hermes"),
		"{autohand}", orHome("AUTOHAND_HOME", ".autohand"),
		"{grok}", orHome("GROK_HOME", ".grok"),
		"{reasonix}", reasonix,
	)
	return filepath.FromSlash(replacer.Replace(template))
}

func TestPathsEveryAgent(t *testing.T) {
	envs := testEnvironments()
	for name, env := range envs {
		t.Run(name, func(t *testing.T) {
			canonicalProject := filepath.Join(env.Cwd, ".agents", "skills")
			canonicalGlobal := filepath.Join(env.Home, ".agents", "skills")
			for _, row := range pathRows {
				a, ok := lookup(row.id)
				if !ok {
					t.Errorf("%s: missing from the registry", row.id)
					continue
				}
				if a.ProjectDir != row.project {
					t.Errorf("%s: ProjectDir = %q, want %q", row.id, a.ProjectDir, row.project)
				}
				if a.ProjectOnly != (row.global == "") {
					t.Errorf("%s: ProjectOnly = %v, global template %q", row.id, a.ProjectOnly, row.global)
				}
				wantProject := filepath.Join(env.Cwd, filepath.FromSlash(row.project))
				if got := a.ProjectSkillsDir(env); got != wantProject {
					t.Errorf("%s: ProjectSkillsDir = %q, want %q", row.id, got, wantProject)
				}
				wantGlobal := ""
				if row.global != "" {
					wantGlobal = expandTemplate(row.global, env)
				}
				if got := a.GlobalSkillsDir(env); got != wantGlobal {
					t.Errorf("%s: GlobalSkillsDir = %q, want %q", row.id, got, wantGlobal)
				}

				wantProjectBase := wantProject
				if a.Universal {
					wantProjectBase = canonicalProject
				}
				if got := a.BaseDir(env, false); got != wantProjectBase {
					t.Errorf("%s: BaseDir(project) = %q, want %q", row.id, got, wantProjectBase)
				}
				wantGlobalBase := wantGlobal
				switch {
				case a.ReadsCanonicalGlobally:
					wantGlobalBase = canonicalGlobal
				case wantGlobal == "":
					wantGlobalBase = filepath.Join(env.Home, filepath.FromSlash(row.project))
				}
				if got := a.BaseDir(env, true); got != wantGlobalBase {
					t.Errorf("%s: BaseDir(global) = %q, want %q", row.id, got, wantGlobalBase)
				}
			}
		})
	}
}

func TestRegistryOrderMatchesDocumentedTable(t *testing.T) {
	ids := IDs()
	if len(ids) != len(pathRows) {
		t.Fatalf("IDs() has %d entries, table has %d", len(ids), len(pathRows))
	}
	for i, row := range pathRows {
		if ids[i] != row.id {
			t.Errorf("position %d = %q, want %q", i, ids[i], row.id)
		}
	}
}

func TestOpenClawPrefersExistingLegacyHome(t *testing.T) {
	home := t.TempDir()
	env := Environment{GOOS: "linux", Home: home, Cwd: t.TempDir(), Vars: map[string]string{}}
	a, _ := lookup("openclaw")
	if got, want := a.GlobalSkillsDir(env), filepath.Join(home, ".openclaw", "skills"); got != want {
		t.Errorf("default = %q, want %q", got, want)
	}
	if err := mkdir(t, filepath.Join(home, ".moltbot")); err != nil {
		t.Fatal(err)
	}
	if got, want := a.GlobalSkillsDir(env), filepath.Join(home, ".moltbot", "skills"); got != want {
		t.Errorf("moltbot = %q, want %q", got, want)
	}
	if err := mkdir(t, filepath.Join(home, ".clawdbot")); err != nil {
		t.Fatal(err)
	}
	if got, want := a.GlobalSkillsDir(env), filepath.Join(home, ".clawdbot", "skills"); got != want {
		t.Errorf("clawdbot = %q, want %q", got, want)
	}
	if err := mkdir(t, filepath.Join(home, ".openclaw")); err != nil {
		t.Fatal(err)
	}
	if got, want := a.GlobalSkillsDir(env), filepath.Join(home, ".openclaw", "skills"); got != want {
		t.Errorf("openclaw = %q, want %q", got, want)
	}
}
