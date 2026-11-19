// Package app holds the use cases. It orchestrates discovery, planning, installing and lock
// recording, and owns no command syntax or output formatting.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/hash"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
	"github.com/nhermosilla14/go-skills/internal/source"
)

// ErrUsage marks a request that cannot be honoured because of how it was invoked. The CLI maps it
// to exit code 2.
var ErrUsage = errors.New("usage error")

// ErrPromptRequired reports that the install needs an answer to a prompt and no interactive flow
// can run, so -y is required.
var ErrPromptRequired = errors.New("prompt required")

// AddOptions are the inputs of the add use case, as parsed from the command line.
type AddOptions struct {
	Source    string   // local directory containing skills
	Agents    []string // agent identifiers, or "*"; empty selects every agent
	Skills    []string // skill names, or "*"; empty selects every skill
	Yes       bool     // proceed without prompting
	Copy      bool     // copy instead of linking
	List      bool     // list the skills and install nothing
	JSON      bool     // machine-readable output; no prompts
	FullDepth bool     // search every subdirectory, even below a root SKILL.md
	Rename    string   // install one skill under this name
	Subagents []string // Eve subagent targets; parsed but not installed yet
	Global    bool     // global scope; only reachable from the library, the parser rejects -g
}

// Env carries what the use case needs from the process. Now must not be nil.
type Env struct {
	WorkDir         string // project directory; the source and lock are resolved from it
	IsTTY           bool   // whether stdin is a terminal
	IncludeInternal bool   // install skills marked metadata.internal
	Now             func() time.Time
	AgentEnv        agents.Environment // home, OS and variables used by detection and paths
}

// Report is the outcome of an add run, for the CLI to render.
type Report struct {
	Listed   []skill.Skill     // set by --list
	Warnings []string          // skills that were skipped while discovering
	Results  []install.Result  // one per skill and agent
	LockPath string            // set when the lock file was written
	Source   string            // source as resolved from the command line
	Ref      string            // git ref, empty for local sources
	Scope    string            // project or global
	Hashes   map[string]string // installed name → content hash, for --json
}

// Failed reports whether any skill failed for any agent.
func (r Report) Failed() bool {
	for _, res := range r.Results {
		if res.Status == install.Failed {
			return true
		}
	}
	return false
}

// Add installs the skills of a source into the project. The flow is: validate agents, parse and
// acquire the source, discover, stop for --list, ask the questions the prompt matrix calls for,
// select, plan, apply, record. The prompter is optional: without one, a run that needs an answer
// returns ErrPromptRequired instead of prompting.
func Add(opts AddOptions, env Env, prompters ...Prompter) (Report, error) {
	var prompter Prompter
	if len(prompters) > 0 {
		prompter = prompters[0]
	}
	agentEnv := env.agentsEnv()
	explicit, err := selectAgents(opts.Agents, agentEnv, opts.Yes)
	if err != nil {
		return Report{}, err
	}
	src, err := source.Parse(opts.Source)
	if err != nil {
		return Report{}, err
	}
	tmp := ""
	if src.Kind != source.KindLocal {
		tmp, err = os.MkdirTemp("", "skills-*")
		if err != nil {
			return Report{}, fmt.Errorf("creating a temporary directory: %w", err)
		}
		defer os.RemoveAll(tmp)
	}
	tree, err := source.Acquire(context.Background(), src, tmp)
	if err != nil {
		return Report{}, err
	}
	found, err := discover(tree.Dir, src, env.IncludeInternal, opts.FullDepth)
	if err != nil {
		return Report{}, err
	}
	rep := Report{
		Warnings: found.Warnings,
		Source:   src.Source,
		Ref:      src.Ref,
		Scope:    string(ScopeProject),
	}
	if len(found.Skills) == 0 {
		return rep, noSkillsError(src, tree)
	}
	if opts.List {
		rep.Listed = found.Skills
		return rep, nil
	}
	questions := questionsFor(opts, src, found.Skills, explicit, agentEnv)
	answers, err := ask(questions, env, prompter)
	if err != nil {
		return rep, err
	}
	if answers.Scope == ScopeGlobal {
		return rep, errors.New("global installs are not implemented yet; install into the project instead")
	}
	requested := requestedSkills(opts.Skills, src.SkillSelector)
	chosen, err := chooseSkills(opts, tree, src, env.IncludeInternal, &found, requested, questions, answers)
	if err != nil {
		return rep, err
	}
	rep.Warnings = found.Warnings
	if err := validateRename(opts.Rename, chosen); err != nil {
		return rep, err
	}
	selected, err := chooseAgents(explicit, answers)
	if err != nil {
		return rep, err
	}
	if hasStep(questions, StepConfirm) && !answers.Proceed {
		return rep, ErrCancelled
	}
	lockPath := filepath.Join(env.WorkDir, lockfile.FileName)
	lock, err := lockfile.Load(lockPath)
	if err != nil {
		return rep, err
	}
	entries := install.Plan(env.WorkDir, chosen, selected, install.PlanOptions{
		Env:       agentEnv,
		ForceCopy: opts.Copy || answers.Mode == install.ModeCopy,
		Global:    opts.Global,
		Rename:    opts.Rename,
	})
	rep.Results = install.Apply(env.WorkDir, entries)
	rep.Hashes = map[string]string{}
	saved, err := recordInstalled(lock, lockPath, recordInput{
		project: env.WorkDir,
		source:  src,
		root:    tree.Dir,
		commit:  tree.Commit,
		entries: entries,
		results: rep.Results,
		hashes:  rep.Hashes,
	})
	if err != nil {
		return rep, err
	}
	if saved {
		rep.LockPath = lockPath
	}
	return rep, nil
}

// questionsFor builds the questions of one run from its flags and what discovery found.
func questionsFor(opts AddOptions, src source.Source, skills []skill.Skill,
	candidates []agents.Agent, agentEnv agents.Environment) Questions {
	detected := agents.Detect(agentEnv)
	eve := false
	for _, a := range detected {
		if a.ID == "eve" {
			eve = true
		}
	}
	requested := requestedSkills(opts.Skills, src.SkillSelector)
	return PlanQuestions(opts, Facts{
		Source:          src.Source,
		Skills:          skills,
		RequestedSkills: requested,
		Detected:        detected,
		Candidates:      candidates,
		All:             agents.All(),
		EveDetected:     eve,
		CanGoGlobal:     anyGlobalDir(candidates, agentEnv),
		Mode:            install.ResolveMode(candidates, opts.Copy),
	})
}

// ask runs the prompts the matrix calls for. No steps mean no prompter is needed; any failure to
// ask becomes the documented ErrPromptRequired.
func ask(questions Questions, env Env, prompter Prompter) (Answers, error) {
	if len(questions.Steps) == 0 {
		return Answers{}, nil
	}
	if prompter == nil {
		return Answers{}, promptRequired(env.IsTTY)
	}
	answers, err := prompter.Ask(questions)
	if errors.Is(err, ErrPromptRequired) {
		return Answers{}, promptRequired(env.IsTTY)
	}
	if err != nil {
		return Answers{}, err
	}
	return answers, nil
}

// chooseSkills resolves the skills of the run: the answers of the selector when it was shown, and
// the flags otherwise, with upstream's one retry at full depth for a named skill.
func chooseSkills(opts AddOptions, tree source.Tree, src source.Source, includeInternal bool,
	found *skill.Result, requested []string, questions Questions, answers Answers) ([]skill.Skill, error) {
	if hasStep(questions, StepSkills) {
		return selectByNames(found.Skills, answers.Skills)
	}
	chosen, err := selectSkills(found.Skills, requested)
	if err != nil && !opts.FullDepth {
		// A requested skill may sit below discovery's normal depth, so retry once, as upstream does.
		deeper, deeperErr := discover(tree.Dir, src, includeInternal, true)
		if deeperErr == nil {
			if deeperChosen, selectErr := selectSkills(deeper.Skills, requested); selectErr == nil {
				*found, chosen, err = deeper, deeperChosen, nil
			}
		}
	}
	return chosen, err
}

// chooseAgents resolves the agents of the run from the selector answers when it ran, and from the
// command line and detection otherwise.
func chooseAgents(explicit []agents.Agent, answers Answers) ([]agents.Agent, error) {
	if len(answers.Agents) == 0 {
		return explicit, nil
	}
	selected, invalid := agents.Expand(answers.Agents)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("%w: invalid agents: %s (valid: %s)", ErrUsage,
			strings.Join(invalid, ", "), strings.Join(agents.IDs(), ", "))
	}
	return selected, nil
}

// selectByNames picks the skills named by a selector answer, in discovery order.
func selectByNames(all []skill.Skill, names []string) ([]skill.Skill, error) {
	if len(names) == 0 {
		return nil, errors.New("no skills selected")
	}
	want := map[string]bool{}
	for _, name := range names {
		want[name] = true
	}
	var chosen []skill.Skill
	for _, s := range all {
		if want[s.Name] {
			chosen = append(chosen, s)
			delete(want, s.Name)
		}
	}
	for name := range want {
		return nil, fmt.Errorf("no skill named %q in source", name)
	}
	return chosen, nil
}

// hasStep reports whether the flow asks step.
func hasStep(questions Questions, step StepID) bool {
	for _, s := range questions.Steps {
		if s == step {
			return true
		}
	}
	return false
}

// anyGlobalDir reports whether at least one candidate agent can install globally, which is when
// the scope prompt appears.
func anyGlobalDir(candidates []agents.Agent, env agents.Environment) bool {
	for _, a := range candidates {
		if a.GlobalSkillsDir(env) != "" {
			return true
		}
	}
	return false
}

// discover finds the skills of an acquired tree. Remote subpaths are folded into each skill's
// relative directory so the skill path is relative to the source root, as the lock records it.
func discover(root string, src source.Source, includeInternal, fullDepth bool) (skill.Result, error) {
	res, err := skill.Discover(root, skill.Options{IncludeInternal: includeInternal, FullDepth: fullDepth})
	if err != nil {
		return skill.Result{}, err
	}
	if src.Subpath != "" {
		for i := range res.Skills {
			res.Skills[i].RelDir = path.Join(src.Subpath, res.Skills[i].RelDir)
		}
	}
	return res, nil
}

// noSkillsError names the source as the user wrote it, without the temporary checkout path.
func noSkillsError(src source.Source, tree source.Tree) error {
	if src.Kind == source.KindLocal {
		return fmt.Errorf("no valid skills found in %s", tree.Dir)
	}
	return fmt.Errorf("no valid skills found in %s", src.Source)
}

// requestedSkills merges the --skill names with the skill named by the source's "@name" suffix.
func requestedSkills(names []string, selector string) []string {
	out := append([]string(nil), names...)
	if selector != "" {
		out = append(out, selector)
	}
	return out
}

// selectAgents resolves the requested agent identifiers. No identifiers uses detection when
// installing non-interactively, and the interactive starting set otherwise.
func selectAgents(ids []string, env agents.Environment, yes bool) ([]agents.Agent, error) {
	if len(ids) == 0 {
		return agents.Select(env, yes), nil
	}
	selected, invalid := agents.Expand(ids)
	if len(invalid) > 0 {
		return nil, fmt.Errorf("%w: invalid agents: %s (valid: %s)", ErrUsage,
			strings.Join(invalid, ", "), strings.Join(agents.IDs(), ", "))
	}
	return selected, nil
}

// agentsEnv fills the environment from the process when the caller passed none, so detection sees
// the real home and variables. A partially filled environment is used as given.
func (e Env) agentsEnv() agents.Environment {
	env := e.AgentEnv
	if env.Home == "" && env.Vars == nil && env.GOOS == "" &&
		env.ApplicationsDir == "" && env.EtcDir == "" {
		env = agents.FromOS(e.WorkDir)
	}
	if env.Cwd == "" {
		env.Cwd = e.WorkDir
	}
	return env
}

// nonTTYPrompt is the documented diagnostic a refused prompt prints. It reads as a sentence, so
// it is kept as data instead of an error-string literal.
const nonTTYPrompt = "Interactive prompt required but stdin is not a TTY. Nothing was installed. " +
	"Use --agent <name> (or --agent '*') and -y to run non-interactively."

// promptError carries a documented diagnostic while staying ErrPromptRequired for callers.
type promptError struct {
	msg string
}

func (e *promptError) Error() string { return e.msg }

func (e *promptError) Unwrap() error { return ErrPromptRequired }

func promptRequired(isTTY bool) error {
	if isTTY {
		return &promptError{msg: "no interactive prompt is available; pass -y to install without prompts"}
	}
	return &promptError{msg: nonTTYPrompt}
}

// selectSkills picks the skills named on the command line. No names, or "*", selects all.
func selectSkills(all []skill.Skill, names []string) ([]skill.Skill, error) {
	if len(names) == 0 {
		return all, nil
	}
	var chosen []skill.Skill
	for _, name := range names {
		if name == "*" {
			return all, nil
		}
		matches := skill.Filter(all, name)
		if len(matches) == 0 {
			return nil, fmt.Errorf("no skill named %q in source", name)
		}
		chosen = append(chosen, matches...)
	}
	return chosen, nil
}

// validateRename checks the --rename restrictions: the name must already be sanitized and exactly
// one skill must be selected.
func validateRename(name string, chosen []skill.Skill) error {
	if name == "" {
		return nil
	}
	if skill.SanitizeName(name) != name {
		return fmt.Errorf("--rename must be a skill name of lowercase letters, digits, \".\", \"_\" and \"-\"")
	}
	if len(chosen) != 1 {
		return fmt.Errorf("--rename installs one skill under a new name, but %d skills were selected", len(chosen))
	}
	return nil
}

type recordInput struct {
	project string
	source  source.Source
	root    string
	commit  string
	entries []install.Entry
	results []install.Result
	hashes  map[string]string
}

// recordInstalled adds every skill that landed for at least one agent and failed for none to the
// lock, then saves the lock if anything was recorded. It reports whether the file was written.
func recordInstalled(lock *lockfile.File, lockPath string, in recordInput) (bool, error) {
	recorded := false
	for _, e := range in.entries {
		loc, ok := installedPath(in.results, e.Name)
		if !ok || failedAny(in.results, e.Name) {
			continue
		}
		digest, err := hashInstalled(loc)
		if err != nil {
			return false, fmt.Errorf("hashing %s: %w", e.Name, err)
		}
		if in.hashes != nil {
			in.hashes[e.Name] = digest
		}
		lock.Put(e.Name, lockEntry(in, e, digest))
		recorded = true
	}
	if !recorded {
		return false, nil
	}
	if err := lock.Save(lockPath); err != nil {
		return false, err
	}
	return true, nil
}

// lockEntry builds the lock record for one installed skill. installedAt keeps the value of an
// earlier install of the same name.
func lockEntry(in recordInput, e install.Entry, digest string) lockfile.Entry {
	entry := lockfile.Entry{ComputedHash: digest}
	if in.source.Kind == source.KindLocal {
		entry.Source = localSource(in.project, in.root)
		entry.SourceType = string(source.KindLocal)
	} else {
		entry.Source = in.source.Source
		entry.SourceType = lockSourceType(in.source.Kind)
		// Upstream records the fetch URL for generic git and GitLab sources, and omits it for
		// GitHub, whose shorthand source is enough to re-derive the clone URL.
		if in.source.SourceURL != "" && in.source.Kind != source.KindGitHub {
			entry.SourceURL = in.source.SourceURL
		}
		entry.Ref = in.source.Ref
		entry.Commit = in.commit
		entry.SkillPath = e.Skill.SkillPath()
	}
	if e.RenameTo != "" && e.RenameTo != e.Skill.Name {
		entry.SkillName = e.Skill.Name
	}
	if e.ForcedCopy {
		entry.InstallMode = "copy"
	}
	return entry
}

// lockSourceType maps a source kind to the lock vocabulary. Azure repositories are recorded as
// "git", the only spelling the lock schema defines for them.
func lockSourceType(kind source.Kind) string {
	if kind == source.KindAzure {
		return string(source.KindGit)
	}
	return string(kind)
}

// hashInstalled hashes the installed skill: a directory normally, and the flat markdown file Eve
// writes.
func hashInstalled(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return hash.Dir(path)
	}
	return hash.SingleFile(path)
}

// installedPath returns where the skill was first installed successfully.
func installedPath(results []install.Result, name string) (string, bool) {
	for _, r := range results {
		if r.Skill == name && r.Status == install.Installed {
			return r.Path, true
		}
	}
	return "", false
}

func failedAny(results []install.Result, name string) bool {
	for _, r := range results {
		if r.Skill == name && r.Status == install.Failed {
			return true
		}
	}
	return false
}

// localSource writes a local source path relative to the project when it lies inside it, so the
// lock stays valid after the project moves.
func localSource(project, source string) string {
	rel, err := filepath.Rel(project, source)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(source)
	}
	if rel == "." {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}
