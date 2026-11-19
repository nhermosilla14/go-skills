package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/lockfile"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// matrixAgent builds the only facts the planner reads from an agent.
func matrixAgent(id string, universal bool) agents.Agent {
	return agents.Agent{
		ID:                     id,
		DisplayName:            id,
		ProjectDir:             "." + id + "/skills",
		Universal:              universal,
		ReadsCanonicalGlobally: universal,
	}
}

func matrixSkills(n int) []skill.Skill {
	out := make([]skill.Skill, 0, n)
	for _, name := range []string{"alpha", "beta", "gamma"}[:n] {
		out = append(out, skill.Skill{Name: name, Description: "d", RelDir: "skills/" + name})
	}
	return out
}

func singleDetected() []agents.Agent { return []agents.Agent{matrixAgent("cursor", true)} }

func baseFacts(n int) Facts {
	detected := singleDetected()
	return Facts{
		Source:      "owner/repo",
		Skills:      matrixSkills(n),
		Detected:    detected,
		Candidates:  detected,
		All:         []agents.Agent{matrixAgent("cursor", true), matrixAgent("aider-desk", false)},
		CanGoGlobal: true,
		Mode:        install.ModeSymlink,
	}
}

func TestPlanStepsMatchesThePromptMatrix(t *testing.T) {
	eveFacts := func() Facts {
		f := baseFacts(1)
		f.EveDetected = true
		f.Detected = []agents.Agent{matrixAgent("eve", false)}
		f.Candidates = f.Detected
		f.CanGoGlobal = false
		return f
	}
	nothingDetected := func() Facts {
		f := baseFacts(1)
		f.Detected = nil
		f.Candidates = []agents.Agent{matrixAgent("cursor", true), matrixAgent("aider-desk", false)}
		return f
	}
	oneSkill := func() Facts { return baseFacts(1) }
	manySkills := func() Facts { return baseFacts(3) }

	tests := []struct {
		name   string
		opts   AddOptions
		facts  Facts
		expect []StepID
	}{
		{
			name:  "-y never prompts",
			opts:  AddOptions{Yes: true},
			facts: manySkills(),
		},
		{
			name:  "-y with Eve detected never prompts",
			opts:  AddOptions{Yes: true},
			facts: eveFacts(),
		},
		{
			name:   "--list never prompts",
			opts:   AddOptions{List: true},
			facts:  manySkills(),
			expect: nil,
		},
		{
			name:   "--json never prompts",
			opts:   AddOptions{JSON: true, Yes: true},
			facts:  manySkills(),
			expect: nil,
		},
		{
			name:   "one skill is auto-selected",
			opts:   AddOptions{},
			facts:  oneSkill(),
			expect: []StepID{StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "several skills prompt for selection first",
			opts:   AddOptions{},
			facts:  manySkills(),
			expect: []StepID{StepSkills, StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "explicit --skill skips the skills prompt",
			opts:   AddOptions{Skills: []string{"alpha"}},
			facts:  withRequested(manySkills(), "alpha"),
			expect: []StepID{StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "the source @skill selector skips the skills prompt",
			opts:   AddOptions{},
			facts:  withRequested(manySkills(), "alpha"),
			expect: []StepID{StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "explicit --agent skips Eve and the agent picker",
			opts:   AddOptions{Agents: []string{"cursor"}},
			facts:  withEveDetected(withRequested(oneSkill(), "alpha")),
			expect: []StepID{StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "Eve detected asks its confirmation before the picker",
			opts:   AddOptions{},
			facts:  eveFacts(),
			expect: []StepID{StepEve, StepAgents, StepMethod, StepConfirm},
		},
		{
			name:   "an Eve project with subagents asks the placement",
			opts:   AddOptions{},
			facts:  withSubagents(eveFacts(), "reviewer"),
			expect: []StepID{StepEve, StepAgents, StepSubagents, StepMethod, StepConfirm},
		},
		{
			name:   "--subagent skips the agent and placement prompts",
			opts:   AddOptions{Subagents: []string{"root"}},
			facts:  withSubagents(eveFacts(), "reviewer"),
			expect: []StepID{StepMethod, StepConfirm},
		},
		{
			name:   "nothing detected asks the agent picker",
			opts:   AddOptions{},
			facts:  nothingDetected(),
			expect: []StepID{StepAgents, StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "exactly one detected agent is auto-selected",
			opts:   AddOptions{},
			facts:  oneSkill(),
			expect: []StepID{StepScope, StepMethod, StepConfirm},
		},
		{
			name:   "-g skips the scope prompt",
			opts:   AddOptions{Global: true},
			facts:  oneSkill(),
			expect: []StepID{StepMethod, StepConfirm},
		},
		{
			name:   "--copy skips the method prompt",
			opts:   AddOptions{Copy: true},
			facts:  withMode(oneSkill(), install.ModeCopy),
			expect: []StepID{StepScope, StepConfirm},
		},
		{
			name:   "a derived copy mode skips the method prompt",
			opts:   AddOptions{},
			facts:  withMode(oneSkill(), install.ModeCopy),
			expect: []StepID{StepScope, StepConfirm},
		},
		{
			name:   "no global directory skips the scope prompt",
			opts:   AddOptions{},
			facts:  withGlobal(oneSkill(), false),
			expect: []StepID{StepMethod, StepConfirm},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PlanSteps(tc.opts, tc.facts)
			if strings.Join(ids(got), ",") != strings.Join(ids(tc.expect), ",") {
				t.Fatalf("PlanSteps = %v, want %v", got, tc.expect)
			}
			questions := PlanQuestions(tc.opts, tc.facts)
			if strings.Join(ids(questions.Steps), ",") != strings.Join(ids(tc.expect), ",") {
				t.Fatalf("PlanQuestions steps = %v, want %v", questions.Steps, tc.expect)
			}
		})
	}
}

func withRequested(f Facts, names ...string) Facts {
	f.RequestedSkills = names
	return f
}

func withEveDetected(f Facts) Facts {
	f.EveDetected = true
	return f
}

func withMode(f Facts, mode install.Mode) Facts {
	f.Mode = mode
	return f
}

// withSubagents marks the run as targeting Eve and discovers the given subagents.
func withSubagents(f Facts, names ...string) Facts {
	f.EveTargeted = true
	f.Subagents = names
	return f
}

func withGlobal(f Facts, ok bool) Facts {
	f.CanGoGlobal = ok
	return f
}

func ids(steps []StepID) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, string(s))
	}
	return out
}

func TestPlanQuestionsChoices(t *testing.T) {
	f := baseFacts(3)
	f.Skills = []skill.Skill{
		{Name: "zeta", Description: "z", PluginName: "", RelDir: "skills/zeta"},
		{Name: "Beta", Description: "b", PluginName: "group-b", RelDir: "skills/beta"},
		{Name: "alpha", Description: "a", PluginName: "", RelDir: "skills/alpha"},
		{Name: "Gamma", Description: "g", PluginName: "group-b", RelDir: "skills/gamma"},
	}
	f.All = []agents.Agent{
		matrixAgent("cursor", true),
		{ID: "eve", DisplayName: "Eve", ProjectDir: "agent/skills", ProjectOnly: true},
		{ID: "hidden", DisplayName: "Hidden", ProjectDir: ".agents/skills", Universal: true,
			ReadsCanonicalGlobally: true, HiddenFromPrompt: true},
		matrixAgent("aider-desk", false),
	}
	f.Detected = []agents.Agent{matrixAgent("aider-desk", false)}
	f.Candidates = f.Detected
	q := PlanQuestions(AddOptions{}, f)

	var names []string
	for _, s := range q.Skills {
		names = append(names, s.Name)
	}
	want := []string{"alpha", "zeta", "Beta", "Gamma"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("skill order = %v, want %v", names, want)
	}
	if q.Skills[0].Group != "General" {
		t.Errorf("ungrouped skill group = %q, want General", q.Skills[0].Group)
	}
	if q.Skills[2].Path != "skills/beta/SKILL.md" {
		t.Errorf("skill path = %q", q.Skills[2].Path)
	}

	var chooser, locked, hidden bool
	for _, a := range q.Agents {
		switch a.ID {
		case "eve":
			t.Error("Eve must not be offered by the picker")
		case "aider-desk":
			chooser = a.Detected
		case "cursor":
			locked = a.Universal
		case "hidden":
			hidden = a.Hidden && a.Universal
		}
	}
	if !chooser || !locked || !hidden {
		t.Errorf("agent choices: chooser=%v locked=%v hidden=%v", chooser, locked, hidden)
	}
	if got := strings.Join(q.Defaults.Agents, ","); got != "aider-desk" {
		t.Errorf("default agents = %q, want the detected candidate", got)
	}
}

// interactiveEnv isolates detection from the machine running the tests.
func interactiveEnv(t *testing.T, project string) Env {
	t.Helper()
	env := testEnv(project)
	env.AgentEnv = agents.Environment{
		GOOS: "linux", Home: t.TempDir(), Cwd: project, Vars: map[string]string{},
		ApplicationsDir: t.TempDir(), EtcDir: t.TempDir(),
	}
	return env
}

func twoSkillSource(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeSourceFile(t, src, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	writeSourceFile(t, src, "skills/beta/SKILL.md", "---\nname: beta\ndescription: b\n---\n")
	return src
}

func TestAddAsksOnlyTheQuestionsTheMatrixCallsFor(t *testing.T) {
	tests := []struct {
		name string
		opts AddOptions
		want []StepID
	}{
		{
			name: "-y asks nothing",
			opts: AddOptions{Yes: true, Agents: []string{"opencode"}, Copy: true},
		},
		{
			name: "--all asks nothing",
			opts: AddOptions{Yes: true, Skills: []string{"*"}, Agents: []string{"*"}},
		},
		{
			name: "--skill skips skill selection",
			opts: AddOptions{Skills: []string{"alpha"}},
			want: []StepID{StepAgents, StepScope, StepMethod, StepConfirm},
		},
		{
			name: "--agent skips agent selection",
			opts: AddOptions{Agents: []string{"opencode"}},
			want: []StepID{StepSkills, StepScope, StepConfirm},
		},
		{
			name: "--copy skips the method",
			opts: AddOptions{Copy: true, Agents: []string{"opencode"}},
			want: []StepID{StepSkills, StepScope, StepConfirm},
		},
		{
			name: "explicit flags leave only scope and confirmation",
			opts: AddOptions{Agents: []string{"opencode"}, Skills: []string{"alpha"}},
			want: []StepID{StepScope, StepConfirm},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			script := NewScripted(t, Answers{
				Skills:  []string{"alpha"},
				Agents:  []string{"opencode"},
				Proceed: true,
			})
			opts := tc.opts
			opts.Source = twoSkillSource(t)
			if opts.Yes && len(opts.Agents) == 0 {
				opts.Agents = []string{"opencode"}
			}
			rep, err := Add(opts, interactiveEnv(t, project), script)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}
			if rep.Failed() {
				t.Fatalf("results failed: %+v", rep.Results)
			}
			asked := script.Asked()
			if len(tc.want) == 0 {
				if len(asked) != 0 {
					t.Fatalf("prompted %v, want none", asked[0].Steps)
				}
				return
			}
			if len(asked) != 1 {
				t.Fatalf("asked %d times, want once", len(asked))
			}
			if strings.Join(ids(asked[0].Steps), ",") != strings.Join(ids(tc.want), ",") {
				t.Fatalf("steps = %v, want %v", asked[0].Steps, tc.want)
			}
		})
	}
}

func TestAddJSONNeverStartsTheFlow(t *testing.T) {
	project := t.TempDir()
	script := NewScripted(t)
	rep, err := Add(AddOptions{
		Source: twoSkillSource(t), JSON: true, Yes: true, Agents: []string{"opencode"},
	}, interactiveEnv(t, project), script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(script.Asked()) != 0 {
		t.Fatalf("--json asked %v", script.Asked()[0].Steps)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %+v, want both skills", rep.Results)
	}
}

func TestAddAppliesScriptedSelection(t *testing.T) {
	project := t.TempDir()
	script := NewScripted(t, Answers{
		Skills:  []string{"beta"},
		Agents:  []string{"opencode"},
		Scope:   ScopeProject,
		Mode:    install.ModeSymlink,
		Proceed: true,
	})
	rep, err := Add(AddOptions{Source: twoSkillSource(t)}, interactiveEnv(t, project), script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Skill != "beta" {
		t.Fatalf("results = %+v, want only beta", rep.Results)
	}
}

func TestAddCancelInstallsNothing(t *testing.T) {
	project := t.TempDir()
	rep, err := Add(AddOptions{Source: twoSkillSource(t)}, interactiveEnv(t, project), cancelledPrompter{})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if len(rep.Results) != 0 {
		t.Fatalf("results = %+v, want none", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(project, lockfile.FileName)); !os.IsNotExist(err) {
		t.Fatalf("cancel wrote a lock: %v", err)
	}
}

// cancelledPrompter is the smallest double of a user pressing ctrl+c.
type cancelledPrompter struct{}

func (cancelledPrompter) Ask(Questions) (Answers, error) { return Answers{}, ErrCancelled }

func TestAddDeclinedConfirmationCancels(t *testing.T) {
	project := t.TempDir()
	script := NewScripted(t, Answers{Skills: []string{"alpha"}, Agents: []string{"opencode"}})
	_, err := Add(AddOptions{Source: twoSkillSource(t)}, interactiveEnv(t, project), script)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestAddGlobalInstallsIntoTheHome(t *testing.T) {
	project := t.TempDir()
	env := interactiveEnv(t, project)
	script := NewScripted(t, Answers{
		Skills: []string{"alpha"}, Agents: []string{"opencode"}, Proceed: true,
	})
	rep, err := Add(AddOptions{Source: twoSkillSource(t), Global: true}, env, script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if rep.Scope != string(ScopeGlobal) {
		t.Errorf("scope = %q, want global", rep.Scope)
	}
	if _, err := os.Stat(filepath.Join(env.AgentEnv.Home, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("global skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.AgentEnv.Home, ".agents", ".skill-lock.json")); err != nil {
		t.Errorf("global lock missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, lockfile.FileName)); !os.IsNotExist(err) {
		t.Errorf("global add wrote the project lock: %v", err)
	}
	asked := script.Asked()
	if len(asked) != 1 || strings.Contains(strings.Join(ids(asked[0].Steps), ","), string(StepScope)) {
		t.Fatalf("steps = %v, want no scope prompt", asked)
	}
}

func TestAddWithoutPrompterReturnsErrPromptRequired(t *testing.T) {
	_, err := Add(AddOptions{Source: twoSkillSource(t)}, interactiveEnv(t, t.TempDir()), nil)
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("err = %v, want ErrPromptRequired", err)
	}
	_, err = Add(AddOptions{Source: twoSkillSource(t)}, interactiveEnv(t, t.TempDir()), NoPrompts{})
	if !errors.Is(err, ErrPromptRequired) {
		t.Fatalf("NoPrompts err = %v, want ErrPromptRequired", err)
	}
	if !strings.Contains(err.Error(), "Interactive prompt required but stdin is not a TTY") {
		t.Fatalf("err = %v, want the documented non-TTY diagnostic", err)
	}
}

func TestEveAnswerTargetsOnlyEve(t *testing.T) {
	project := t.TempDir()
	src := t.TempDir()
	writeSourceFile(t, src, "skills/alpha/SKILL.md", "---\nname: alpha\ndescription: a\n---\n")
	writeSourceFile(t, project, "agent/keep", "x")
	writeSourceFile(t, project, "package.json", `{"dependencies":{"eve":"1.0.0"}}`)

	env := interactiveEnv(t, project)
	env.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	script := NewScripted(t, Answers{Agents: []string{"eve"}, Proceed: true})
	rep, err := Add(AddOptions{Source: src}, env, script)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(rep.Results) != 1 || rep.Results[0].Agent != "eve" {
		t.Fatalf("results = %+v, want only Eve", rep.Results)
	}
	steps := script.Asked()[0].Steps
	if strings.Join(ids(steps), ",") != "eve,agents,confirm" {
		t.Fatalf("steps = %v, want the Eve confirmation and the picker", steps)
	}
}
