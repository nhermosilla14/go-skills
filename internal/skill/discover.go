package skill

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
)

const fileName = "SKILL.md"

// skillsContainer is the directory whose children (and grandchildren, one category level) are
// searched for skills.
const skillsContainer = "skills"

// skipDirs are never searched and never treated as skills.
var skipDirs = map[string]bool{"node_modules": true, ".git": true, "dist": true, "build": true, "__pycache__": true}

// Options controls discovery.
type Options struct {
	// IncludeInternal keeps skills marked metadata.internal. Without it they are hidden.
	IncludeInternal bool
}

// Skill is a discovered skill.
type Skill struct {
	Name        string // sanitized frontmatter name; identity of the skill
	Description string
	Dir         string // absolute path of the directory holding SKILL.md
	RelDir      string // slash-separated path of Dir relative to the source root; "" for the root
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

// Discover finds skills in the source root: the root itself, skills/<name>/ and skills/<category>/<name>/.
// A SKILL.md at the root makes the whole repository one skill and stops the search, as upstream
// does; otherwise shallower matches win over deeper ones with the same name, and directory order
// is sorted.
func Discover(root string, opts Options) (Result, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", root)
	}
	d := &discoverer{root: root, opts: opts, names: map[string]bool{}}
	if hasSkillFile(root) {
		// A valid root SKILL.md makes the whole repository one skill. When it fails to parse or
		// is gated, upstream keeps scanning the containers below.
		if d.load(root, "") {
			return d.result, nil
		}
	}
	d.scanContainer(filepath.Join(root, skillsContainer), skillsContainer)
	return d.result, nil
}

type discoverer struct {
	root   string
	opts   Options
	names  map[string]bool
	result Result
}

// visit loads the skill in dir when it has a SKILL.md file.
func (d *discoverer) visit(dir, rel string) {
	if hasSkillFile(dir) {
		d.load(dir, rel)
	}
}

// scanContainer searches the children of dir, then the grandchildren of the children that are
// not skills themselves. The two passes make shallower skills shadow deeper ones.
func (d *discoverer) scanContainer(dir, rel string) {
	var categories []string
	for _, name := range subdirs(dir) {
		childRel := path.Join(rel, name)
		if hasSkillFile(filepath.Join(dir, name)) {
			d.load(filepath.Join(dir, name), childRel)
			continue
		}
		categories = append(categories, name)
	}
	for _, category := range categories {
		for _, name := range subdirs(filepath.Join(dir, category)) {
			grandRel := path.Join(rel, category, name)
			d.visit(filepath.Join(dir, category, name), grandRel)
		}
	}
}

// load parses the SKILL.md in dir and records the skill, reporting whether one was added. Invalid,
// gated or duplicate skills are skipped; invalid ones are reported as warnings.
func (d *discoverer) load(dir, rel string) bool {
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
	if d.names[meta.Name] {
		return false
	}
	d.names[meta.Name] = true
	d.result.Skills = append(d.result.Skills, Skill{
		Name:        meta.Name,
		Description: meta.Description,
		Dir:         dir,
		RelDir:      rel,
	})
	return true
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
