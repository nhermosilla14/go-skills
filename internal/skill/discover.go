package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
)

const fileName = "SKILL.md"

// containerDepth is how many directory levels below a catalog container are searched.
const containerDepth = 3

// fallbackDepth is how many directory levels below the source root the recursive fallback searches.
const fallbackDepth = 5

// skillsContainer is the built-in catalog container.
const skillsContainer = "skills"

// containerDirs are the fixed containers searched below the source root, in walk order. The root
// itself is walked separately at depth 1.
var containerDirs = []string{skillsContainer, "skills/.curated", "skills/.experimental", "skills/.system"}

// skipDirs are never searched and never treated as skills.
var skipDirs = map[string]bool{"node_modules": true, ".git": true, "dist": true, "build": true, "__pycache__": true}

// Options controls discovery.
type Options struct {
	// IncludeInternal keeps skills marked metadata.internal. Without it they are hidden.
	IncludeInternal bool
	// FullDepth disables the root short-circuit and always runs the recursive fallback.
	FullDepth bool
}

// Skill is a discovered skill.
type Skill struct {
	Name        string // sanitized frontmatter name; identity of the skill
	Description string
	Dir         string // absolute path of the directory holding SKILL.md
	RelDir      string // slash-separated path of Dir relative to the source root; "" for the root
	PluginName  string // plugin that declares the skill, for grouping; empty when undeclared
}

// SkillPath returns the slash-separated path of the SKILL.md file relative to the source root.
func (s Skill) SkillPath() string {
	return path.Join(s.RelDir, fileName)
}

// Result is the outcome of a discovery run. Skills that fail to load are reported in Warnings and
// never abort the run.
type Result struct {
	Skills   []Skill
	Warnings []string
}

// Discover finds the skills of a source tree. The root SKILL.md is tried first and, when valid,
// ends the search unless FullDepth is set. Every catalog container is then walked to three levels,
// plugin-declared directories to one, and if nothing was found the whole tree is walked to five
// levels. A directory holding SKILL.md claims its subtree even when the skill is unusable.
func Discover(root string, opts Options) (Result, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", root)
	}
	root = filepath.Clean(root)
	d := &discoverer{
		root:     root,
		opts:     opts,
		names:    map[string]bool{},
		visited:  map[string]bool{},
		agentDir: agents.ProjectSkillDirs(),
	}
	if err := d.loadSourceLock(); err != nil {
		return Result{}, err
	}
	pluginDirs, groupings := readPluginManifests(root)
	d.groupings = groupings

	if hasSkillFile(root) {
		if d.load(root, "") && !opts.FullDepth {
			return d.result, nil
		}
	}
	for _, c := range d.containers(pluginDirs) {
		d.walk(c.dir, c.rel, 1, c.depth)
	}
	if len(d.result.Skills) == 0 || opts.FullDepth {
		d.walkFallback(root, "", 0)
	}
	return d.result, nil
}

type discoverer struct {
	root      string
	opts      Options
	names     map[string]bool   // skill names already found
	visited   map[string]bool   // directories whose SKILL.md was already parsed
	lockKeys  map[string]bool   // normalized names of skills installed in this source
	agentDir  []string          // project skill directories that hold installed copies
	groupings map[string]string // absolute skill directory to plugin name
	result    Result
}

// loadSourceLock reads the source's own lock so its installed copies are not listed again.
func (d *discoverer) loadSourceLock() error {
	p := filepath.Join(d.root, lockfile.FileName)
	lock, err := lockfile.Load(p)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return err
		}
		return nil // an invalid lock means no exclusions
	}
	d.lockKeys = map[string]bool{}
	for name := range lock.Skills {
		d.lockKeys[NormalizeName(name)] = true
	}
	return nil
}

type container struct {
	dir   string // absolute path
	rel   string // slash-separated path relative to the source root
	depth int    // directory levels below dir that are searched
}

// containers returns every directory to walk, in discovery order.
func (d *discoverer) containers(pluginDirs []string) []container {
	list := []container{{dir: d.root, rel: "", depth: 1}}
	for _, rel := range containerDirs {
		list = append(list, container{dir: filepath.Join(d.root, filepath.FromSlash(rel)), rel: rel, depth: containerDepth})
	}
	for _, rel := range d.agentDir {
		list = append(list, container{dir: filepath.Join(d.root, filepath.FromSlash(rel)), rel: rel, depth: containerDepth})
	}
	for _, rel := range skillsShDirs(d.root) {
		list = append(list, container{dir: filepath.Join(d.root, filepath.FromSlash(rel)), rel: rel, depth: containerDepth})
	}
	for _, dir := range pluginDirs {
		rel, err := filepath.Rel(d.root, dir)
		if err != nil {
			continue
		}
		list = append(list, container{dir: dir, rel: filepath.ToSlash(rel), depth: 1})
	}
	return dedupeContainers(list)
}

// dedupeContainers keeps the first occurrence of each directory.
func dedupeContainers(list []container) []container {
	seen := map[string]bool{}
	out := list[:0]
	for _, c := range list {
		key := filepath.Clean(c.dir)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// walk searches the children of dir down to depth levels. A child holding SKILL.md is claimed and
// its subtree is never searched.
func (d *discoverer) walk(dir, rel string, depth, maxDepth int) {
	for _, name := range subdirs(dir) {
		child := filepath.Join(dir, name)
		childRel := path.Join(rel, name)
		if d.claim(child, childRel) {
			continue
		}
		if depth < maxDepth {
			d.walk(child, childRel, depth+1, maxDepth)
		}
	}
}

// walkFallback searches the whole tree depth-first, adding a directory before its children.
func (d *discoverer) walkFallback(dir, rel string, depth int) {
	if hasSkillFile(dir) {
		d.load(dir, rel)
	}
	if depth >= fallbackDepth {
		return
	}
	for _, name := range subdirs(dir) {
		d.walkFallback(filepath.Join(dir, name), path.Join(rel, name), depth+1)
	}
}

// claim reports whether dir holds a SKILL.md, in which case its subtree is never searched. The
// skill itself may still be skipped as invalid, gated, duplicate or already installed.
func (d *discoverer) claim(dir, rel string) bool {
	if !hasSkillFile(dir) {
		return false
	}
	d.load(dir, rel)
	return true
}

// load parses the SKILL.md in dir and records the skill, reporting whether one was added.
func (d *discoverer) load(dir, rel string) bool {
	if d.visited[dir] {
		return false
	}
	d.visited[dir] = true
	skillPath := path.Join(rel, fileName)
	raw, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		d.warn(skillPath, err)
		return false
	}
	meta, err := ParseSkillMD(raw)
	if err != nil {
		d.warn(skillPath, err)
		return false
	}
	if meta.Internal && !d.opts.IncludeInternal {
		return false
	}
	if d.names[meta.Name] || d.isInstalledCopy(rel, meta.Name) {
		return false
	}
	d.names[meta.Name] = true
	d.result.Skills = append(d.result.Skills, Skill{
		Name:        meta.Name,
		Description: meta.Description,
		Dir:         dir,
		RelDir:      rel,
		PluginName:  d.groupings[filepath.Clean(dir)],
	})
	return true
}

// isInstalledCopy reports whether a skill in rel is a copy this source installed itself, which is
// tracked by its own lock.
func (d *discoverer) isInstalledCopy(rel, name string) bool {
	if len(d.lockKeys) == 0 {
		return false
	}
	inAgentDir := false
	for _, dir := range d.agentDir {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			inAgentDir = true
			break
		}
	}
	if !inAgentDir {
		return false
	}
	return d.lockKeys[NormalizeName(name)] || d.lockKeys[NormalizeName(path.Base(rel))]
}

func (d *discoverer) warn(skillPath string, err error) {
	d.result.Warnings = append(d.result.Warnings,
		fmt.Sprintf("skipped %s: %s", sanitizeText(skillPath), sanitizeText(err.Error())))
}

// hasSkillFile reports whether dir contains a regular file named SKILL.md.
func hasSkillFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, fileName))
	return err == nil && info.Mode().IsRegular()
}

// subdirs returns the sorted names of the directories in dir, skipping skipDirs. A missing or
// unreadable dir yields nothing.
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !skipDirs[e.Name()] {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
