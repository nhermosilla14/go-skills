package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func internalMD(name string) string {
	return "---\nname: " + name + "\ndescription: x\nmetadata:\n  internal: true\n---\n"
}

func TestDiscoverContainerOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/skill-a/SKILL.md", skillMD("skills-a"))
	writeFile(t, root, "skills/.curated/skill-b/SKILL.md", skillMD("skills-b"))
	writeFile(t, root, "skills/.experimental/skill-c/SKILL.md", skillMD("skills-c"))
	writeFile(t, root, "skills/.system/skill-d/SKILL.md", skillMD("skills-d"))
	writeFile(t, root, ".agents/skills/skill-e/SKILL.md", skillMD("agent-e"))
	writeFile(t, root, ".claude/skills/skill-f/SKILL.md", skillMD("agent-f"))
	writeFile(t, root, "declared/skill-g/SKILL.md", skillMD("config-g"))
	writeFile(t, root, "skills.sh.json", `{"paths": ["./declared"]}`)
	writeFile(t, root, ".claude-plugin/marketplace.json", `{
		"metadata": {"pluginRoot": "./plugins"},
		"plugins": [{"name": "Acme", "source": "./pack", "skills": ["./custom/one"]}]
	}`)
	writeFile(t, root, "plugins/pack/custom/one/SKILL.md", skillMD("plugin-h"))
	writeFile(t, root, "plugins/pack/skills/skill-i/SKILL.md", skillMD("plugin-i"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// The skills/ walk is depth-first in sorted order, so its dot directories come first.
	want := []string{"skills-b", "skills-c", "skills-d", "skills-a", "agent-e", "agent-f", "config-g", "plugin-h", "plugin-i"}
	if got := names(res.Skills); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestDiscoverContainerPrecedenceByDuplicateName(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".agents/skills/dup/SKILL.md", skillMD("dup"))
	writeFile(t, root, ".claude/skills/agent-dup/SKILL.md", skillMD("agent-dup"))
	writeFile(t, root, "declared/config-dup/SKILL.md", skillMD("config-dup"))
	writeFile(t, root, "skills.sh.json", `{"paths": ["./declared"]}`)
	writeFile(t, root, ".claude-plugin/marketplace.json", `{
		"metadata": {"pluginRoot": "./plugins"},
		"plugins": [{"name": "Acme", "source": "./pack", "skills": ["./skills/plugin-dup"]}]
	}`)
	writeFile(t, root, "plugins/pack/skills/plugin-dup/SKILL.md", skillMD("plugin-dup"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{"dup", "agent-dup", "config-dup", "plugin-dup"}
	if got := names(res.Skills); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	if got := res.Skills[3].RelDir; got != "plugins/pack/skills/plugin-dup" {
		t.Errorf("plugin winner = %q", got)
	}
}

func TestDiscoverContainerDepthThree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/a/b/c/SKILL.md", skillMD("depth-three"))
	writeFile(t, root, "skills/a/b/c/d/SKILL.md", skillMD("inside-claimed"))
	writeFile(t, root, "skills/x1/x2/x3/SKILL.md", skillMD("chain-three"))
	writeFile(t, root, "skills/y1/y2/y3/y4/SKILL.md", skillMD("chain-four"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{"depth-three", "chain-three"}
	if got := names(res.Skills); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestDiscoverUnusableSkillShadowsSubtree(t *testing.T) {
	tests := []struct {
		name     string
		outer    string
		opts     Options
		withOpts []string
	}{
		{
			name:  "invalid skill",
			outer: "---\ndescription: no name\n---\n",
		},
		{
			name:  "duplicate name",
			outer: skillMD("good"),
		},
		{
			name:  "internal and gated",
			outer: internalMD("outer"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "skills/good/SKILL.md", skillMD("good"))
			writeFile(t, root, "skills/outer/SKILL.md", tc.outer)
			writeFile(t, root, "skills/outer/inner/SKILL.md", skillMD("inner"))

			res, err := Discover(root, Options{})
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if got := names(res.Skills); !reflect.DeepEqual(got, []string{"good"}) {
				t.Errorf("names = %v, want only good: the outer directory claims its subtree", got)
			}
		})
	}
}

func TestDiscoverInternalShadowsAndCanBeIncluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/good/SKILL.md", skillMD("good"))
	writeFile(t, root, "skills/outer/SKILL.md", internalMD("outer"))
	writeFile(t, root, "skills/outer/inner/SKILL.md", skillMD("inner"))

	res, err := Discover(root, Options{IncludeInternal: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"good", "outer"}) {
		t.Errorf("names = %v, want good and outer without inner", got)
	}
}

func TestDiscoverSkipsSkipDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/real/SKILL.md", skillMD("real"))
	for _, name := range []string{"node_modules", ".git", "dist", "build", "__pycache__"} {
		writeFile(t, root, "skills/"+name+"/nested/SKILL.md", skillMD("from-"+name))
	}
	writeFile(t, root, "skills/build/SKILL.md", skillMD("build-itself"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"real"}) {
		t.Errorf("names = %v, want only real", got)
	}
}

func TestDiscoverFallbackToDepthFive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "q1/q2/q3/q4/q5/SKILL.md", skillMD("depth-five"))
	writeFile(t, root, "r1/r2/r3/r4/r5/r6/SKILL.md", skillMD("depth-six"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"depth-five"}) {
		t.Errorf("names = %v, want depth-five only", got)
	}
}

func TestDiscoverFullDepth(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "SKILL.md", skillMD("root-skill"))
	writeFile(t, root, "skills/a/b/c/d/SKILL.md", skillMD("deep-skill"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"root-skill"}) {
		t.Errorf("names = %v, want only the root skill", got)
	}

	res, err = Discover(root, Options{FullDepth: true})
	if err != nil {
		t.Fatalf("Discover full depth: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"root-skill", "deep-skill"}) {
		t.Errorf("names = %v, want root and deep skills", got)
	}
}

func TestDiscoverSkillsShPaths(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   []string
	}{
		{"valid", `{"paths": ["./declared"]}`, []string{"base", "declared"}},
		{"missing ./ is ignored", `{"paths": ["declared"]}`, []string{"base"}},
		{"escape is ignored", `{"paths": ["./../outside"]}`, []string{"base"}},
		{"absolute is ignored", `{"paths": ["/etc"]}`, []string{"base"}},
		{"backslash is ignored", `{"paths": ["./decl\\ared"]}`, []string{"base"}},
		{"file is ignored", `{"paths": ["./declared/file.txt"]}`, []string{"base"}},
		{"duplicates are removed", `{"paths": ["./declared", "./declared/"]}`, []string{"base", "declared"}},
		{"non-string entries are ignored", `{"paths": [1, "./declared"]}`, []string{"base", "declared"}},
		{"broken json is ignored", `{`, []string{"base"}},
		{"missing paths is ignored", `{}`, []string{"base"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
			writeFile(t, root, "declared/target/SKILL.md", skillMD("declared"))
			writeFile(t, root, "declared/file.txt", "x")
			writeFile(t, root, "skills.sh.json", tc.config)

			res, err := Discover(root, Options{})
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if got := names(res.Skills); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("names = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDiscoverPluginManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
	writeFile(t, root, ".claude-plugin/marketplace.json", `{
		"metadata": {"pluginRoot": "./plugins"},
		"plugins": [
			{"name": "Acme", "source": "./pack", "skills": ["./custom/one"]},
			{"name": "Remote", "source": {"source": "github", "repo": "o/r"}},
			{"name": "Bad", "source": "pack"}
		]
	}`)
	writeFile(t, root, ".claude-plugin/plugin.json", `{"name": "Solo", "skills": ["./custom/here"]}`)
	writeFile(t, root, "plugins/pack/custom/one/SKILL.md", skillMD("plugin-one"))
	writeFile(t, root, "plugins/pack/skills/two/SKILL.md", skillMD("plugin-two"))
	writeFile(t, root, "custom/here/SKILL.md", skillMD("plugin-here"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{"base", "plugin-one", "plugin-two", "plugin-here"}
	if got := names(res.Skills); !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for _, s := range res.Skills {
		switch s.Name {
		case "plugin-one":
			if s.PluginName != "Acme" {
				t.Errorf("plugin-one grouping = %q, want Acme", s.PluginName)
			}
		case "plugin-here":
			if s.PluginName != "Solo" {
				t.Errorf("plugin-here grouping = %q, want Solo", s.PluginName)
			}
		case "plugin-two":
			if s.PluginName != "" {
				t.Errorf("plugin-two grouping = %q, want none: only declared paths are grouped", s.PluginName)
			}
		}
	}
}

func TestDiscoverPluginManifestRootMustBeRelative(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
	writeFile(t, root, ".claude-plugin/marketplace.json", `{
		"metadata": {"pluginRoot": "plugins"},
		"plugins": [{"name": "Acme", "source": "./pack", "skills": ["./custom/one"]}]
	}`)
	writeFile(t, root, "plugins/pack/custom/one/SKILL.md", skillMD("plugin-one"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"base"}) {
		t.Errorf("names = %v, want base only: a bad pluginRoot discards the block", got)
	}
}

func TestDiscoverBrokenPluginManifestsAreIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
	writeFile(t, root, ".claude-plugin/marketplace.json", `{not json`)
	writeFile(t, root, ".claude-plugin/plugin.json", `{"skills": "./nope"}`)

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"base"}) {
		t.Errorf("names = %v, want base only", got)
	}
}

func TestDiscoverExcludesInstalledCopies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
	writeFile(t, root, ".agents/skills/copied/SKILL.md", skillMD("Copy Me"))
	writeFile(t, root, ".claude/skills/another/SKILL.md", skillMD("Different"))
	writeFile(t, root, ".claude/skills/kept/SKILL.md", skillMD("kept"))
	writeFile(t, root, "skills/copied/SKILL.md", skillMD("copy-me"))
	writeFile(t, root, "skills-lock.json", `{"version": 2, "skills": {"copy-me": {}, "another": {}}}`)

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{"base", "copy-me", "kept"}
	if got := names(res.Skills); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

func TestDiscoverInvalidSourceLockExcludesNothing(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
	writeFile(t, root, ".claude/skills/copied/SKILL.md", skillMD("copied"))
	writeFile(t, root, "skills-lock.json", `{not json`)

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := names(res.Skills); !reflect.DeepEqual(got, []string{"base", "copied"}) {
		t.Errorf("names = %v, want base and copied", got)
	}
}

func TestDiscoverUnreadableSourceLockIsAnError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/base/SKILL.md", skillMD("base"))
	if err := os.Mkdir(filepath.Join(root, "skills-lock.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(root, Options{}); err == nil {
		t.Error("Discover with an unreadable lock: want error")
	}
}

func TestDiscoverAgentContainerOrderDecidesDuplicates(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".agents/skills/dup/SKILL.md", skillMD("dup"))
	writeFile(t, root, ".claude/skills/dup/SKILL.md", skillMD("dup"))

	res, err := Discover(root, Options{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(res.Skills) != 1 || res.Skills[0].RelDir != ".agents/skills/dup" {
		t.Errorf("skills = %+v, want only .agents/skills/dup", res.Skills)
	}
}

func TestFilter(t *testing.T) {
	all := []Skill{{Name: "Alpha Skill"}, {Name: "alpha-skill"}, {Name: "Beta"}}
	tests := []struct {
		query string
		want  []string
	}{
		{"alpha skill", []string{"Alpha Skill"}},
		{"ALPHA SKILL", []string{"Alpha Skill"}},
		{"alpha-skill", []string{"alpha-skill"}},
		{"Alpha_Skill", []string{"Alpha Skill", "alpha-skill"}},
		{"missing", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			if got := names(Filter(all, tc.query)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Filter(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tests := map[string]string{
		"Convex Best Practices": "convex-best-practices",
		"my_skill":              "my-skill",
		"Mixed  Name":           "mixed-name",
		"already-hyphen":        "already-hyphen",
	}
	for in, want := range tests {
		if got := NormalizeName(in); got != want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeFixedPointRename(t *testing.T) {
	tests := map[string]bool{
		"good-name":   true,
		"v2.0":        true,
		"123":         true,
		"Bad Name":    false,
		"UPPER":       false,
		"trailing-":   false,
		"..leading":   false,
		"has space":   false,
		"has/slash":   false,
		"under_score": true,
	}
	for name, want := range tests {
		if got := SanitizeName(name) == name; got != want {
			t.Errorf("SanitizeName(%q) fixed point = %v, want %v", name, got, want)
		}
	}
}
