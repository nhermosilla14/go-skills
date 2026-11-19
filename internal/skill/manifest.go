package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/fsutil"
)

// pluginManifestDir holds the Claude Code plugin manifests read by discovery.
const pluginManifestDir = ".claude-plugin"

type marketplaceManifest struct {
	Metadata struct {
		PluginRoot string `json:"pluginRoot"`
	} `json:"metadata"`
	Plugins []struct {
		Name   string            `json:"name"`
		Source json.RawMessage   `json:"source"`
		Skills []json.RawMessage `json:"skills"`
	} `json:"plugins"`
}

type pluginManifest struct {
	Name   string            `json:"name"`
	Skills []json.RawMessage `json:"skills"`
}

// readPluginManifests returns the directories declared by the plugin manifests, to be walked one
// level deep, and the plugin name of every exactly declared skill directory. Broken manifests
// contribute nothing.
func readPluginManifests(root string) ([]string, map[string]string) {
	dir := filepath.Join(root, pluginManifestDir)
	var dirs []string
	groupings := map[string]string{}
	dirs = append(dirs, marketplaceDirs(dir, root, groupings)...)
	dirs = append(dirs, pluginJSONDirs(dir, root, groupings)...)
	return dedupePaths(dirs), groupings
}

// marketplaceDirs reads marketplace.json. A pluginRoot that does not start with "./" discards the
// whole block.
func marketplaceDirs(manifestDir, root string, groupings map[string]string) []string {
	data, err := os.ReadFile(filepath.Join(manifestDir, "marketplace.json"))
	if err != nil {
		return nil
	}
	var doc marketplaceManifest
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	pluginRoot := doc.Metadata.PluginRoot
	if pluginRoot != "" && !strings.HasPrefix(pluginRoot, "./") {
		return nil
	}
	var dirs []string
	for _, plugin := range doc.Plugins {
		source, ok := pluginSource(plugin.Source)
		if !ok {
			continue
		}
		base := filepath.Join(root, filepath.FromSlash(pluginRoot), filepath.FromSlash(source))
		if !fsutil.Within(root, base) {
			continue
		}
		for _, raw := range plugin.Skills {
			declared, ok := declaredPath(base, raw)
			if !ok {
				continue
			}
			dirs = append(dirs, filepath.Dir(declared))
			if plugin.Name != "" {
				if _, seen := groupings[declared]; !seen {
					groupings[declared] = plugin.Name
				}
			}
		}
		dirs = append(dirs, filepath.Join(base, skillsContainer))
	}
	return dirs
}

// pluginSource reads plugins[].source: absent means the plugin root, a "./" string is relative to
// it, and anything else (remote objects included) rejects the plugin.
func pluginSource(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", true
	}
	var source string
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", false
	}
	if source == "" {
		return "", false
	}
	rel, ok := cleanRelativePath(source)
	if !ok {
		return "", false
	}
	return rel, true
}

// pluginJSONDirs reads plugin.json. Its declared paths are relative to the source root and the
// conventional skills directory is always searched.
func pluginJSONDirs(manifestDir, root string, groupings map[string]string) []string {
	data, err := os.ReadFile(filepath.Join(manifestDir, "plugin.json"))
	if err != nil {
		return nil
	}
	var doc pluginManifest
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	dirs := []string{filepath.Join(root, skillsContainer)}
	for _, raw := range doc.Skills {
		declared, ok := declaredPath(root, raw)
		if !ok {
			continue
		}
		dirs = append(dirs, filepath.Dir(declared))
		if doc.Name != "" {
			if _, seen := groupings[declared]; !seen {
				groupings[declared] = doc.Name
			}
		}
	}
	return dirs
}

// declaredPath resolves one skills[] entry below base and checks containment.
func declaredPath(base string, raw json.RawMessage) (string, bool) {
	var entry string
	if err := json.Unmarshal(raw, &entry); err != nil {
		return "", false
	}
	rel, ok := cleanRelativePath(entry)
	if !ok {
		return "", false
	}
	p := filepath.Join(base, filepath.FromSlash(rel))
	if !fsutil.Within(base, p) {
		return "", false
	}
	return filepath.Clean(p), true
}

// dedupePaths keeps the first occurrence of each path.
func dedupePaths(paths []string) []string {
	seen := map[string]bool{}
	out := paths[:0]
	for _, p := range paths {
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
