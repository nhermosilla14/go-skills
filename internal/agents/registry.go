package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// registry is the ordered list of agents upstream supports (79 ids). Paths and detection rules
// mirror olddocs/behavior/05-agents-and-paths.md §2, §5 and §7; registry order is upstream's and
// is never sorted. One line per agent where possible, grouped by shared paths.
var registry = []Agent{
	{ID: "aider-desk", DisplayName: "AiderDesk", ProjectDir: ".aider-desk/skills", globalDir: homeDir(".aider-desk/skills"), detect: homeExists(".aider-desk")},
	{ID: "amp", DisplayName: "Amp", ProjectDir: ".agents/skills", globalDir: configDir("agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: configExists("amp")},
	{ID: "antigravity", DisplayName: "Antigravity", ProjectDir: ".agents/skills", globalDir: homeDir(".gemini/config/skills"), Universal: true, HiddenFromPrompt: true, detect: homeExists(".gemini/antigravity")},
	{ID: "antigravity-cli", DisplayName: "Antigravity CLI", ProjectDir: ".agents/skills", globalDir: homeDir(".gemini/config/skills"), Universal: true, HiddenFromPrompt: true, detect: homeExists(".gemini/antigravity-cli")},
	{ID: "astrbot", DisplayName: "AstrBot", ProjectDir: "data/skills", globalDir: homeDir(".astrbot/data/skills"), detect: astrbotDetected},
	{ID: "autohand-code", DisplayName: "Autohand Code CLI", ProjectDir: ".autohand/skills", globalDir: envDir("AUTOHAND_HOME", ".autohand"), detect: envHomeExists("AUTOHAND_HOME", ".autohand")},
	{ID: "augment", DisplayName: "Augment", ProjectDir: ".augment/skills", globalDir: homeDir(".augment/skills"), detect: homeExists(".augment")},
	{ID: "bob", DisplayName: "IBM Bob", ProjectDir: ".bob/skills", globalDir: homeDir(".bob/skills"), detect: homeExists(".bob")},
	{ID: "claude-code", DisplayName: "Claude Code", ProjectDir: ".claude/skills", globalDir: envDir("CLAUDE_CONFIG_DIR", ".claude"), CreateProjectDirByDefault: true, detect: envHomeExists("CLAUDE_CONFIG_DIR", ".claude")},
	{ID: "openclaw", DisplayName: "OpenClaw", ProjectDir: "skills", globalDir: openClawGlobalDir, detect: openClawDetected},
	{ID: "cline", DisplayName: "Cline", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".cline")},
	{ID: "codearts-agent", DisplayName: "CodeArts Agent", ProjectDir: ".codeartsdoer/skills", globalDir: homeDir(".codeartsdoer/skills"), detect: homeExists(".codeartsdoer")},
	{ID: "codebuddy", DisplayName: "CodeBuddy", ProjectDir: ".codebuddy/skills", globalDir: homeDir(".codebuddy/skills"), detect: projectOrHomeExists(".codebuddy")},
	{ID: "codemaker", DisplayName: "Codemaker", ProjectDir: ".codemaker/skills", globalDir: homeDir(".codemaker/skills"), detect: homeExists(".codemaker")},
	{ID: "codestudio", DisplayName: "Code Studio", ProjectDir: ".codestudio/skills", globalDir: homeDir(".codestudio/skills"), detect: homeExists(".codestudio")},
	{ID: "codex", DisplayName: "Codex", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: codexDetected},
	{ID: "command-code", DisplayName: "Command Code", ProjectDir: ".commandcode/skills", globalDir: homeDir(".commandcode/skills"), detect: homeExists(".commandcode")},
	{ID: "continue", DisplayName: "Continue", ProjectDir: ".continue/skills", globalDir: homeDir(".continue/skills"), detect: projectOrHomeExists(".continue")},
	{ID: "cortex", DisplayName: "Cortex Code", ProjectDir: ".cortex/skills", globalDir: homeDir(".snowflake/cortex/skills"), detect: homeExists(".snowflake/cortex")},
	{ID: "crush", DisplayName: "Crush", ProjectDir: ".crush/skills", globalDir: homeDir(".config/crush/skills"), detect: homeExists(".config/crush")},
	{ID: "cursor", DisplayName: "Cursor", ProjectDir: ".agents/skills", globalDir: homeDir(".cursor/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".cursor")},
	{ID: "deepagents", DisplayName: "Deep Agents", ProjectDir: ".agents/skills", globalDir: homeDir(".deepagents/agent/skills"), Universal: true, HiddenFromPrompt: true, ReadsCanonicalGlobally: true, detect: homeExists(".deepagents")},
	{ID: "devin", DisplayName: "Devin for Terminal", ProjectDir: ".devin/skills", globalDir: configDir("devin/skills"), detect: configExists("devin")},
	{ID: "dexto", DisplayName: "Dexto", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, HiddenFromPrompt: true, ReadsCanonicalGlobally: true, detect: homeExists(".dexto")},
	{ID: "droid", DisplayName: "Droid", ProjectDir: ".agents/skills", globalDir: homeDir(".factory/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".factory")},
	{ID: "eve", DisplayName: "Eve", ProjectDir: "agent/skills", ProjectOnly: true, detect: eveDetected},
	{ID: "firebender", DisplayName: "Firebender", ProjectDir: ".agents/skills", globalDir: homeDir(".firebender/skills"), Universal: true, HiddenFromPrompt: true, ReadsCanonicalGlobally: true, detect: homeExists(".firebender")},
	{ID: "forgecode", DisplayName: "ForgeCode", ProjectDir: ".forge/skills", globalDir: homeDir(".forge/skills"), detect: homeExists(".forge")},
	{ID: "fx", DisplayName: "fx", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".fx")},
	{ID: "gemini-cli", DisplayName: "Gemini CLI", ProjectDir: ".agents/skills", globalDir: homeDir(".gemini/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".gemini")},
	{ID: "github-copilot", DisplayName: "GitHub Copilot", ProjectDir: ".agents/skills", globalDir: homeDir(".copilot/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".copilot")},
	{ID: "goose", DisplayName: "Goose", ProjectDir: ".goose/skills", globalDir: configDir("goose/skills"), detect: configExists("goose")},
	{ID: "grok", DisplayName: "Grok Build", ProjectDir: ".grok/skills", globalDir: envDir("GROK_HOME", ".grok"), detect: envHomeExists("GROK_HOME", ".grok")},
	{ID: "hermes-agent", DisplayName: "Hermes Agent", ProjectDir: ".hermes/skills", globalDir: envDir("HERMES_HOME", ".hermes"), detect: envHomeExists("HERMES_HOME", ".hermes")},
	{ID: "inference-sh", DisplayName: "inference.sh", ProjectDir: ".inferencesh/skills", globalDir: homeDir(".inferencesh/skills"), detect: homeExists(".inferencesh")},
	{ID: "iflow-cli", DisplayName: "iFlow CLI", ProjectDir: ".iflow/skills", globalDir: homeDir(".iflow/skills"), detect: homeExists(".iflow")},
	{ID: "jazz", DisplayName: "Jazz", ProjectDir: ".jazz/skills", globalDir: homeDir(".jazz/skills"), detect: jazzDetected},
	{ID: "junie", DisplayName: "Junie", ProjectDir: ".junie/skills", globalDir: homeDir(".junie/skills"), detect: homeExists(".junie")},
	{ID: "kilo", DisplayName: "Kilo Code", ProjectDir: ".agents/skills", globalDir: homeDir(".kilo/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: kiloDetected},
	{ID: "kimchi", DisplayName: "Kimchi", ProjectDir: ".kimchi/skills", globalDir: homeDir(".config/kimchi/harness/skills"), detect: homeExists(".config/kimchi")},
	{ID: "kimi-code-cli", DisplayName: "Kimi Code CLI", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: kimiDetected},
	{ID: "kiro-cli", DisplayName: "Kiro CLI", ProjectDir: ".kiro/skills", globalDir: homeDir(".kiro/skills"), detect: homeExists(".kiro")},
	{ID: "kode", DisplayName: "Kode", ProjectDir: ".kode/skills", globalDir: homeDir(".kode/skills"), detect: homeExists(".kode")},
	{ID: "lingma", DisplayName: "Lingma", ProjectDir: ".lingma/skills", globalDir: homeDir(".lingma/skills"), detect: homeExists(".lingma")},
	{ID: "loaf", DisplayName: "Loaf", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, HiddenFromPrompt: true, ReadsCanonicalGlobally: true, detect: homeExists(".loaf")},
	{ID: "mcpjam", DisplayName: "MCPJam", ProjectDir: ".mcpjam/skills", globalDir: homeDir(".mcpjam/skills"), detect: homeExists(".mcpjam")},
	{ID: "minimax-code", DisplayName: "MiniMax Code", ProjectDir: ".minimax/skills", globalDir: homeDir(".minimax/skills"), detect: miniMaxDetected},
	{ID: "mistral-vibe", DisplayName: "Mistral Vibe", ProjectDir: ".vibe/skills", globalDir: envDir("VIBE_HOME", ".vibe"), detect: envHomeExists("VIBE_HOME", ".vibe")},
	{ID: "moxby", DisplayName: "Moxby", ProjectDir: ".moxby/skills", globalDir: homeDir(".moxby/skills"), detect: homeExists(".moxby")},
	{ID: "mux", DisplayName: "Mux", ProjectDir: ".mux/skills", globalDir: homeDir(".mux/skills"), detect: homeExists(".mux")},
	{ID: "neovate", DisplayName: "Neovate", ProjectDir: ".neovate/skills", globalDir: homeDir(".neovate/skills"), detect: homeExists(".neovate")},
	{ID: "opencode", DisplayName: "OpenCode", ProjectDir: ".agents/skills", globalDir: configDir("opencode/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: configExists("opencode")},
	{ID: "openhands", DisplayName: "OpenHands", ProjectDir: ".openhands/skills", globalDir: homeDir(".openhands/skills"), detect: homeExists(".openhands")},
	{ID: "ona", DisplayName: "Ona", ProjectDir: ".ona/skills", globalDir: homeDir(".ona/skills"), detect: homeExists(".ona")},
	{ID: "pi", DisplayName: "Pi", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".pi/agent")},
	{ID: "pochi", DisplayName: "Pochi", ProjectDir: ".pochi/skills", globalDir: homeDir(".pochi/skills"), detect: homeExists(".pochi")},
	{ID: "posit-assistant", DisplayName: "Posit Assistant", ProjectDir: ".posit/assistant/skills", globalDir: homeDir(".posit/assistant/skills"), detect: positDetected},
	{ID: "promptscript", DisplayName: "PromptScript", ProjectDir: ".agents/skills", ProjectOnly: true, Universal: true, HiddenFromPrompt: true, ReadsCanonicalGlobally: true, detect: promptScriptDetected},
	{ID: "qoder", DisplayName: "Qoder", ProjectDir: ".qoder/skills", globalDir: homeDir(".qoder/skills"), detect: homeExists(".qoder")},
	{ID: "qoder-cn", DisplayName: "Qoder CN", ProjectDir: ".qoder/skills", globalDir: homeDir(".qoder-cn/skills"), detect: homeExists(".qoder-cn")},
	{ID: "qwen-code", DisplayName: "Qwen Code", ProjectDir: ".qwen/skills", globalDir: homeDir(".qwen/skills"), detect: homeExists(".qwen")},
	{ID: "reasonix", DisplayName: "Reasonix", ProjectDir: ".reasonix/skills", globalDir: reasonixSkillsDir, detect: reasonixDetected},
	{ID: "replit", DisplayName: "Replit", ProjectDir: ".agents/skills", globalDir: configDir("agents/skills"), Universal: true, ReadsCanonicalGlobally: true, NotAutoAdded: true, detect: projectExists(".replit")},
	{ID: "rovodev", DisplayName: "Rovo Dev", ProjectDir: ".rovodev/skills", globalDir: homeDir(".rovodev/skills"), detect: homeExists(".rovodev")},
	{ID: "roo", DisplayName: "Roo Code", ProjectDir: ".roo/skills", globalDir: homeDir(".roo/skills"), detect: homeExists(".roo")},
	{ID: "sarvam-code", DisplayName: "Sarvam Code", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, HiddenFromPrompt: true, ReadsCanonicalGlobally: true, detect: envHomeExists("SARVAM_HOME", ".sarvam")},
	{ID: "tabnine-cli", DisplayName: "Tabnine CLI", ProjectDir: ".tabnine/agent/skills", globalDir: homeDir(".tabnine/agent/skills"), detect: homeExists(".tabnine")},
	{ID: "terramind", DisplayName: "Terramind", ProjectDir: ".terramind/skills", globalDir: homeDir(".terramind/skills"), detect: homeExists(".terramind")},
	{ID: "tinycloud", DisplayName: "Tinycloud", ProjectDir: ".tinycloud/skills", globalDir: homeDir(".tinycloud/skills"), detect: homeExists(".tinycloud")},
	{ID: "trae", DisplayName: "Trae", ProjectDir: ".trae/skills", globalDir: homeDir(".trae/skills"), detect: homeExists(".trae")},
	{ID: "trae-cn", DisplayName: "Trae CN", ProjectDir: ".trae/skills", globalDir: homeDir(".trae-cn/skills"), detect: homeExists(".trae-cn")},
	{ID: "universal", DisplayName: "Universal", ProjectDir: ".agents/skills", globalDir: configDir("agents/skills"), Universal: true, ReadsCanonicalGlobally: true, NotAutoAdded: true},
	{ID: "warp", DisplayName: "Warp", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: homeExists(".warp")},
	{ID: "windsurf", DisplayName: "Windsurf", ProjectDir: ".windsurf/skills", globalDir: homeDir(".codeium/windsurf/skills"), detect: homeExists(".codeium/windsurf")},
	{ID: "zed", DisplayName: "Zed", ProjectDir: ".agents/skills", globalDir: homeDir(".agents/skills"), Universal: true, ReadsCanonicalGlobally: true, detect: zedDetected},
	{ID: "zcode", DisplayName: "ZCode", ProjectDir: ".zcode/skills", globalDir: homeDir(".zcode/skills"), detect: zcodeDetected},
	{ID: "zencoder", DisplayName: "Zencoder", ProjectDir: ".zencoder/skills", globalDir: homeDir(".zencoder/skills"), detect: homeExists(".zencoder")},
	{ID: "zenflow", DisplayName: "Zenflow", ProjectDir: ".zencoder/skills", globalDir: homeDir(".zencoder/skills"), detect: homeExists(".zencoder")},
	{ID: "adal", DisplayName: "AdaL", ProjectDir: ".adal/skills", globalDir: homeDir(".adal/skills"), detect: homeExists(".adal")},
}

// envHomeExists detects $NAME, falling back to ~/fallback.
func envHomeExists(name, fallback string) func(Environment) bool {
	return func(env Environment) bool {
		if v := env.getenv(name); v != "" {
			return pathExists(v)
		}
		return pathExists(join(env.Home, fallback))
	}
}

// openClawGlobalDir prefers the existing legacy config directory and defaults to ~/.openclaw.
func openClawGlobalDir(env Environment) string {
	for _, name := range []string{".openclaw", ".clawdbot", ".moltbot"} {
		if pathExists(join(env.Home, name)) {
			return join(env.Home, name+"/skills")
		}
	}
	return join(env.Home, ".openclaw/skills")
}

func openClawDetected(env Environment) bool {
	for _, name := range []string{".openclaw", ".clawdbot", ".moltbot"} {
		if pathExists(join(env.Home, name)) {
			return true
		}
	}
	return false
}

// reasonixHome is the only platform-dependent agent home. On Windows it defaults to
// %APPDATA%/reasonix, else ~/AppData/Roaming/reasonix.
func reasonixHome(env Environment) string {
	if v := env.getenv("REASONIX_HOME"); v != "" {
		return v
	}
	if env.GOOS == "windows" {
		if appData := env.getenv("APPDATA"); appData != "" {
			return join(appData, "reasonix")
		}
		return join(env.Home, "AppData/Roaming/reasonix")
	}
	return join(env.Home, ".reasonix")
}

func reasonixSkillsDir(env Environment) string {
	return join(reasonixHome(env), "skills")
}

func reasonixDetected(env Environment) bool {
	return pathExists(reasonixHome(env))
}

func codexDetected(env Environment) bool {
	home := join(env.Home, ".codex")
	if v := env.getenv("CODEX_HOME"); v != "" {
		home = v
	}
	return pathExists(home) || pathExists(join(env.etcDir(), "codex"))
}

func kiloDetected(env Environment) bool {
	return pathExists(join(env.Home, ".kilo")) || pathExists(join(env.Home, ".kilocode"))
}

func kimiDetected(env Environment) bool {
	return pathExists(join(env.Home, ".kimi-code")) || pathExists(join(env.Home, ".kimi"))
}

func positDetected(env Environment) bool {
	return pathExists(join(env.Home, ".posit/assistant")) || pathExists(join(env.Home, ".positai"))
}

func jazzDetected(env Environment) bool {
	return pathExists(join(env.Home, ".jazz")) || pathExists(join(env.Cwd, ".jazz"))
}

func astrbotDetected(env Environment) bool {
	return pathExists(join(env.Cwd, "data/skills")) || pathExists(join(env.Home, ".astrbot"))
}

func promptScriptDetected(env Environment) bool {
	return pathExists(join(env.Cwd, ".promptscript")) || pathExists(join(env.Cwd, "promptscript.yaml"))
}

func zcodeDetected(env Environment) bool {
	return pathExists(join(env.Home, ".zcode")) || pathExists(join(env.applicationsDir(), "ZCode.app"))
}

func miniMaxDetected(env Environment) bool {
	return pathExists(join(env.Home, ".minimax")) || pathExists(join(env.applicationsDir(), "MiniMax Code.app"))
}

func zedDetected(env Environment) bool {
	if pathExists(join(env.ConfigHome(), "zed")) {
		return true
	}
	if appData := env.getenv("APPDATA"); appData != "" && pathExists(join(appData, "Zed")) {
		return true
	}
	if flatpak := env.getenv("FLATPAK_XDG_CONFIG_HOME"); flatpak != "" && pathExists(join(flatpak, "zed")) {
		return true
	}
	return false
}

// eveDetected needs both the agent directory and an eve dependency in package.json.
func eveDetected(env Environment) bool {
	if !pathExists(join(env.Cwd, "agent")) {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(env.Cwd, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Dependencies    map[string]json.RawMessage `json:"dependencies"`
		DevDependencies map[string]json.RawMessage `json:"devDependencies"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return false
	}
	return hasDependency(pkg.Dependencies, "eve") || hasDependency(pkg.DevDependencies, "eve")
}

// hasDependency reports a declared dependency; a null value does not count, as upstream's
// truthiness check.
func hasDependency(deps map[string]json.RawMessage, name string) bool {
	raw, ok := deps[name]
	if !ok {
		return false
	}
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null"
}
