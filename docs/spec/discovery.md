# discovery

Finding skills in a source tree and validating them.

## Behaviours

### Skill contract
A skill is a directory containing `SKILL.md`. Frontmatter must start the file (BOM allowed) with a
`---` line and contain non-empty string `name` and `description`. Unknown keys are tolerated;
`metadata.internal` (`true`, `"true"`, `"1"`) hides the skill unless `INSTALL_INTERNAL_SKILLS` is
set. Invalid skills are skipped with a warning, never fatal.
- Criteria: every failure mode skips one file; internal skills are hidden and still shadow nested
  trees.
- Tests: TestParseSkillMD, TestDiscoverInternalGating, TestDiscoverUnusableSkillShadowsSubtree

### Containers and depth
The root `SKILL.md` is tried first; a valid one makes the repository a single skill. Otherwise
`skills/`, `skills/.curated/`, `skills/.experimental/`, `skills/.system/`, agent project skill
directories, `skills.sh.json` containers (three levels) and plugin-manifest directories (one level)
are walked. A directory holding `SKILL.md` claims its subtree. When nothing is found the tree is
walked to five levels; `--full-depth` disables the root short-circuit and always runs that walk.
- Criteria: order, depth, shadowing, `SKIP_DIRS`, fallback and `--full-depth` match the documented
  algorithm; directory entries are sorted so results are deterministic.
- Tests: TestDiscoverContainerOrder, TestDiscoverContainerDepthThree, TestDiscoverFallbackToDepthFive, TestDiscoverFullDepth, TestE2EDiscoveryMatrix

### Extra containers and manifests
`skills.sh.json` `paths` must start with `./`, stay inside the source and name a directory. Claude
plugin manifests (`.claude-plugin/marketplace.json`, `plugin.json`) contribute declared skill
directories and a plugin grouping; broken manifests contribute nothing.
- Criteria: invalid entries are ignored; declared paths are discovered; grouping is recorded.
- Tests: TestDiscoverSkillsShPaths, TestDiscoverPluginManifests, TestDiscoverPluginManifestRootMustBeRelative, TestDiscoverBrokenPluginManifestsAreIgnored

### Installed copies and duplicates
A consumer repository's own lock excludes previously installed copies from discovery. Duplicate
names keep the first skill found, compared exactly and case-sensitively; lookups try a
case-insensitive display match, then a normalized name.
- Criteria: installed copies are not re-listed; duplicates resolve deterministically.
- Tests: TestDiscoverExcludesInstalledCopies, TestDiscoverInvalidSourceLockExcludesNothing, TestDiscoverContainerPrecedenceByDuplicateName, TestFilter, TestNormalizeName

### Rename
`--rename` accepts one selected skill and a name that is already sanitizer-stable. It rewrites the
installed folder, the lock key and the `name:` line in the installed `SKILL.md`, preserving BOM,
line endings, comments and other keys, and records the upstream name in `skillName`. The source is
never modified.
- Criteria: folder, lock key, frontmatter and `skillName` agree; CRLF/BOM survive.
- Tests: TestAddRenameRewritesInstalledName, TestSetFrontmatterName, TestE2ERename, TestE2ERenameRejected

## Not supported

- YAML multi-document frontmatter and `---js`/`---yaml` openers.
- `--rename` on more than one skill.
