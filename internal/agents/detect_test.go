package agents

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// detectMarkers points each agent's detector at one marker to create. The main branch of every
// detector is covered here; TestDetectionAlternateBranches covers the legacy and secondary ones.
var detectMarkers = map[string]string{
	"aider-desk":      "home:.aider-desk",
	"amp":             "config:amp",
	"antigravity":     "home:.gemini/antigravity",
	"antigravity-cli": "home:.gemini/antigravity-cli",
	"astrbot":         "cwd:data/skills",
	"autohand-code":   "var:AUTOHAND_HOME",
	"augment":         "home:.augment",
	"bob":             "home:.bob",
	"claude-code":     "var:CLAUDE_CONFIG_DIR",
	"openclaw":        "home:.openclaw",
	"cline":           "home:.cline",
	"codearts-agent":  "home:.codeartsdoer",
	"codebuddy":       "cwd:.codebuddy",
	"codemaker":       "home:.codemaker",
	"codestudio":      "home:.codestudio",
	"codex":           "var:CODEX_HOME",
	"command-code":    "home:.commandcode",
	"continue":        "cwd:.continue",
	"cortex":          "home:.snowflake/cortex",
	"crush":           "home:.config/crush",
	"cursor":          "home:.cursor",
	"deepagents":      "home:.deepagents",
	"devin":           "config:devin",
	"dexto":           "home:.dexto",
	"droid":           "home:.factory",
	"eve":             "pkg:eve",
	"firebender":      "home:.firebender",
	"forgecode":       "home:.forge",
	"fx":              "home:.fx",
	"gemini-cli":      "home:.gemini",
	"github-copilot":  "home:.copilot",
	"goose":           "config:goose",
	"grok":            "var:GROK_HOME",
	"hermes-agent":    "var:HERMES_HOME",
	"inference-sh":    "home:.inferencesh",
	"iflow-cli":       "home:.iflow",
	"jazz":            "home:.jazz",
	"junie":           "home:.junie",
	"kilo":            "home:.kilo",
	"kimchi":          "home:.config/kimchi",
	"kimi-code-cli":   "home:.kimi-code",
	"kiro-cli":        "home:.kiro",
	"kode":            "home:.kode",
	"lingma":          "home:.lingma",
	"loaf":            "home:.loaf",
	"mcpjam":          "home:.mcpjam",
	"minimax-code":    "home:.minimax",
	"mistral-vibe":    "var:VIBE_HOME",
	"moxby":           "home:.moxby",
	"mux":             "home:.mux",
	"neovate":         "home:.neovate",
	"opencode":        "config:opencode",
	"openhands":       "home:.openhands",
	"ona":             "home:.ona",
	"pi":              "home:.pi/agent",
	"pochi":           "home:.pochi",
	"posit-assistant": "home:.posit/assistant",
	"promptscript":    "cwd:.promptscript",
	"qoder":           "home:.qoder",
	"qoder-cn":        "home:.qoder-cn",
	"qwen-code":       "home:.qwen",
	"reasonix":        "var:REASONIX_HOME",
	"replit":          "cwd:.replit",
	"rovodev":         "home:.rovodev",
	"roo":             "home:.roo",
	"sarvam-code":     "var:SARVAM_HOME",
	"tabnine-cli":     "home:.tabnine",
	"terramind":       "home:.terramind",
	"tinycloud":       "home:.tinycloud",
	"trae":            "home:.trae",
	"trae-cn":         "home:.trae-cn",
	"universal":       "",
	"warp":            "home:.warp",
	"windsurf":        "home:.codeium/windsurf",
	"zed":             "config:zed",
	"zcode":           "home:.zcode",
	"zencoder":        "home:.zencoder",
	"zenflow":         "home:.zencoder",
	"adal":            "home:.adal",
}

func newFakeEnv(t *testing.T) Environment {
	t.Helper()
	return Environment{
		GOOS:            "linux",
		Home:            t.TempDir(),
		Cwd:             t.TempDir(),
		Vars:            map[string]string{},
		ApplicationsDir: t.TempDir(),
		EtcDir:          t.TempDir(),
	}
}

func mkdir(t *testing.T, path string) error {
	t.Helper()
	return os.MkdirAll(path, 0o755)
}

// setupMarker creates the directory or file named by marker. Kinds: home:rel, config:rel, cwd:rel,
// etc:rel, apps:rel, var:NAME (the variable points at a new directory), varrel:NAME:rel (the
// variable points at a new directory and rel exists below it), pkg:eve, pkgdev:eve.
func setupMarker(t *testing.T, env *Environment, marker string) {
	t.Helper()
	kind, rest, _ := strings.Cut(marker, ":")
	create := func(base, rel string) {
		t.Helper()
		if err := mkdir(t, filepath.Join(base, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	switch kind {
	case "":
	case "home":
		create(env.Home, rest)
	case "config":
		create(env.ConfigHome(), rest)
	case "cwd":
		create(env.Cwd, rest)
	case "etc":
		create(env.EtcDir, rest)
	case "apps":
		create(env.ApplicationsDir, rest)
	case "var":
		dir := t.TempDir()
		env.Vars[rest] = dir
	case "varrel":
		name, rel, ok := strings.Cut(rest, ":")
		if !ok {
			t.Fatalf("bad varrel marker %q", marker)
		}
		dir := t.TempDir()
		env.Vars[name] = dir
		create(dir, rel)
	case "pkg":
		create(env.Cwd, "agent")
		writePackageJSON(t, env.Cwd, `{"dependencies":{"`+rest+`":"1.0.0"}}`)
	case "pkgdev":
		create(env.Cwd, "agent")
		writePackageJSON(t, env.Cwd, `{"devDependencies":{"`+rest+`":"1.0.0"}}`)
	default:
		t.Fatalf("unknown marker %q", marker)
	}
}

func writePackageJSON(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectionMarkers(t *testing.T) {
	for id, marker := range detectMarkers {
		t.Run(id, func(t *testing.T) {
			env := newFakeEnv(t)
			setupMarker(t, &env, marker)
			a, ok := lookup(id)
			if !ok {
				t.Fatalf("missing registry entry %s", id)
			}
			if got := a.Detected(env); got != (marker != "") {
				t.Errorf("Detected = %v with marker %q, want %v", got, marker, marker != "")
			}
		})
	}
	if len(detectMarkers) != len(registry) {
		t.Errorf("markers cover %d agents, registry has %d", len(detectMarkers), len(registry))
	}
}

func TestDetectionAlternateBranches(t *testing.T) {
	tests := []struct {
		id      string
		markers []string
	}{
		{"openclaw", []string{"home:.clawdbot"}},
		{"openclaw", []string{"home:.moltbot"}},
		{"astrbot", []string{"home:.astrbot"}},
		{"codebuddy", []string{"home:.codebuddy"}},
		{"continue", []string{"home:.continue"}},
		{"eve", []string{"pkgdev:eve"}},
		{"jazz", []string{"cwd:.jazz"}},
		{"kilo", []string{"home:.kilocode"}},
		{"kimi-code-cli", []string{"home:.kimi"}},
		{"posit-assistant", []string{"home:.positai"}},
		{"promptscript", []string{"cwd:promptscript.yaml"}},
		{"zed", []string{"varrel:APPDATA:Zed"}},
		{"zed", []string{"varrel:FLATPAK_XDG_CONFIG_HOME:zed"}},
		{"zcode", []string{"apps:ZCode.app"}},
		{"minimax-code", []string{"apps:MiniMax Code.app"}},
		{"codex", []string{"etc:codex"}},
	}
	for _, tc := range tests {
		t.Run(tc.id+"/"+strings.Join(tc.markers, "+"), func(t *testing.T) {
			env := newFakeEnv(t)
			for _, marker := range tc.markers {
				setupMarker(t, &env, marker)
			}
			a, _ := lookup(tc.id)
			if !a.Detected(env) {
				t.Errorf("%s not detected with %v", tc.id, tc.markers)
			}
		})
	}
}

func TestDetectionIgnoresTheRealOSAndErrors(t *testing.T) {
	env := newFakeEnv(t)
	// Nothing exists in any of the fake roots: every detector must say no, including the
	// /Applications and /etc probes and the project markers.
	for _, a := range registry {
		if a.Detected(env) {
			t.Errorf("%s detected in an empty environment", a.ID)
		}
	}
}

func TestDetectionEveRequiresPackageDependency(t *testing.T) {
	tests := []struct {
		name  string
		pkg   string
		agent bool
		want  bool
	}{
		{"dependency", `{"dependencies":{"eve":"1"}}`, true, true},
		{"devDependency", `{"devDependencies":{"eve":"1"}}`, true, true},
		{"no dependency", `{"dependencies":{"other":"1"}}`, true, false},
		{"no package.json", "", true, false},
		{"invalid package.json", `{`, true, false},
		{"no agent dir", `{"dependencies":{"eve":"1"}}`, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newFakeEnv(t)
			if tc.agent {
				if err := mkdir(t, filepath.Join(env.Cwd, "agent")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.pkg != "" {
				writePackageJSON(t, env.Cwd, tc.pkg)
			}
			a, _ := lookup("eve")
			if got := a.Detected(env); got != tc.want {
				t.Errorf("Detected = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectPreservesRegistryOrder(t *testing.T) {
	env := newFakeEnv(t)
	for _, marker := range []string{"home:.qoder", "home:.cursor", "home:.claude"} {
		setupMarker(t, &env, marker)
	}
	var got []string
	for _, a := range Detect(env) {
		got = append(got, a.ID)
	}
	want := []string{"claude-code", "cursor", "qoder"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect = %v, want %v", got, want)
	}
}

func TestSelectMatrix(t *testing.T) {
	autoUniversal := func() []string {
		var ids []string
		for _, a := range registry {
			if a.Universal && a.ReadsCanonicalGlobally && !a.NotAutoAdded {
				ids = append(ids, a.ID)
			}
		}
		return ids
	}
	withUniversal := func(first ...string) []string {
		out := append([]string(nil), first...)
		for _, id := range autoUniversal() {
			if !contains(out, id) {
				out = append(out, id)
			}
		}
		return out
	}
	allExceptEve := func() []string {
		var ids []string
		for _, a := range registry {
			if a.ID != "eve" {
				ids = append(ids, a.ID)
			}
		}
		return ids
	}

	tests := []struct {
		name    string
		yes     bool
		markers []string
		wantIDs []string
	}{
		{"nothing detected, yes, all agents", true, nil, IDs()},
		{"nothing detected, interactive, all but Eve", false, nil, allExceptEve()},
		{"one detected, universal added", true, []string{"home:.cursor"}, withUniversal("cursor")},
		{"two detected, universal added", true, []string{"home:.zencoder"}, withUniversal("zencoder", "zenflow")},
		{"Eve alone wins", true, []string{"pkg:eve"}, []string{"eve"}},
		{"Eve with another agent still wins", true, []string{"pkg:eve", "home:.cursor"}, []string{"eve"}},
		{"one detected, interactive, universal added", false, []string{"home:.cursor"}, withUniversal("cursor")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newFakeEnv(t)
			for _, marker := range tc.markers {
				setupMarker(t, &env, marker)
			}
			var got []string
			for _, a := range Select(env, tc.yes) {
				got = append(got, a.ID)
			}
			if !reflect.DeepEqual(got, tc.wantIDs) {
				t.Errorf("Select ids = %v, want %v", got, tc.wantIDs)
			}
		})
	}
}

func TestAutoUniversalExcludesNamedOnlyAndAntigravity(t *testing.T) {
	auto := ensureUniversalAgents(nil)
	if len(auto) != 20 {
		t.Fatalf("auto-added universal = %d, want 20", len(auto))
	}
	have := map[string]bool{}
	for _, a := range auto {
		have[a.ID] = true
	}
	for _, id := range []string{"replit", "universal", "antigravity", "antigravity-cli"} {
		if have[id] {
			t.Errorf("%s must not be auto-added", id)
		}
	}
	for _, id := range []string{"deepagents", "dexto", "firebender", "loaf", "sarvam-code", "promptscript"} {
		if !have[id] {
			t.Errorf("hidden universal agent %s must still be auto-added", id)
		}
	}
}
