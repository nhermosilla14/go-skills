package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// isTreeHash reports whether a global folder hash is a 40-hex Git tree SHA.
func isTreeHash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// globalStatus compares an installed global skill with its lock entry. A tree hash needs the
// recorded commit, so the upstream folder is cloned and both folders are content-hashed. A hash
// that cannot be verified counts as modified, like every other unverifiable entry.
func globalStatus(key, dir string, entry lockfile.GlobalEntry) Status {
	if entry.SkillFolderHash == "" {
		// A well-known entry deliberately records an empty folder hash. There is nothing to compare
		// against locally; check re-fetches the provider and compares the content instead.
		if entry.SourceType == string(source.KindWellKnown) {
			return StatusTracked
		}
		return StatusModified
	}
	if isTreeHash(entry.SkillFolderHash) && entry.SourceType == string(source.KindGitHub) {
		if treeContentMatches(key, dir, entry) {
			return StatusTracked
		}
		return StatusModified
	}
	digest, err := hash.Dir(dir)
	if err != nil || !strings.EqualFold(digest, entry.SkillFolderHash) {
		return StatusModified
	}
	return StatusTracked
}

// treeContentMatches hashes the upstream folder at the recorded commit and compares it with the
// installed folder.
func treeContentMatches(key, dir string, entry lockfile.GlobalEntry) bool {
	upstream, err := upstreamContentHash(key, entry)
	if err != nil {
		return false
	}
	installed, err := hash.Dir(dir)
	return err == nil && upstream == installed
}

// upstreamContentHash clones the recorded commit and returns the content hash of the recorded
// skill folder.
func upstreamContentHash(key string, entry lockfile.GlobalEntry) (string, error) {
	src, err := source.Parse(globalEntrySource(entry))
	if err != nil {
		return "", err
	}
	if entry.Commit != "" {
		src.Ref = entry.Commit
	}
	tmp, err := os.MkdirTemp("", "skills-global-check-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	tree, err := source.Acquire(context.Background(), src, tmp)
	if err != nil {
		return "", err
	}
	found, err := discover(tree.Dir, src, true, true)
	if err != nil {
		return "", err
	}
	upstream, ok := findUpstreamSkill(found.Skills, recordedName(key, entry.SkillName), entry.SkillPath)
	if !ok {
		return "", errors.New("the recorded skill was not found in the source")
	}
	return hash.Dir(upstream.Dir)
}

// findUpstreamSkill finds a skill by normalized name, falling back to its recorded SKILL.md path.
// Several same-name candidates are ambiguous and are not used.
func findUpstreamSkill(all []skill.Skill, name, skillPath string) (skill.Skill, bool) {
	want := skill.NormalizeName(name)
	var candidates []skill.Skill
	for _, s := range all {
		if skill.NormalizeName(s.Name) == want {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	if len(candidates) > 1 {
		return skill.Skill{}, false
	}
	wantPath := normalizeSkillPath(skillPath)
	if wantPath == "" {
		return skill.Skill{}, false
	}
	for _, s := range all {
		if normalizeSkillPath(s.SkillPath()) == wantPath {
			return s, true
		}
	}
	return skill.Skill{}, false
}

// recordedName is the upstream name a lock entry was installed from.
func recordedName(key, skillName string) string {
	if skillName != "" {
		return skillName
	}
	return key
}

// globalEntrySource is the location a global entry is fetched from.
func globalEntrySource(e lockfile.GlobalEntry) string {
	if e.SourceURL != "" {
		return e.SourceURL
	}
	return e.Source
}
