package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nhermosilla14/go-skills/internal/agents"
	"github.com/nhermosilla14/go-skills/internal/install"
	"github.com/nhermosilla14/go-skills/internal/skill"
)

// ErrCancelled reports that the user cancelled the flow before anything was installed. The CLI
// prints Installation cancelled and exits 0 on a terminal, 1 otherwise.
var ErrCancelled = errors.New("installation cancelled")

// Scope is where an install writes.
type Scope string

const (
	// ScopeProject installs into the project directory.
	ScopeProject Scope = "project"
	// ScopeGlobal installs into the user's home directory.
	ScopeGlobal Scope = "global"
	// ScopeBoth updates both scopes. Not supported yet.
	ScopeBoth Scope = "both"
)

// StepID names one prompt of the add flow. The values are stable because the flow is compiled
// from them and the tests assert the compiled list.
type StepID string

const (
	// StepSkills is the grouped multi-select of discovered skills.
	StepSkills StepID = "skills"
	// StepEve confirms installing for a detected Eve project.
	StepEve StepID = "eve"
	// StepAgents is the agent multi-select.
	StepAgents StepID = "agents"
	// StepScope selects the installation scope.
	StepScope StepID = "scope"
	// StepUpdateScope selects the update scope: project, global or both.
	StepUpdateScope StepID = "update-scope"
	// StepMethod selects symlink or copy.
	StepMethod StepID = "method"
	// StepConfirm is the final yes/no.
	StepConfirm StepID = "confirm"
	// StepRemoveSkills is the multi-select of installed skills to remove.
	StepRemoveSkills StepID = "remove-skills"
	// StepRemoveConfirm is the yes/no before removing the selected skills.
	StepRemoveConfirm StepID = "remove-confirm"
	// StepFindSkillsOffer is the one-time suggestion to install find-skills after an install.
	StepFindSkillsOffer StepID = "find-skills-offer"
)

// Answers are the decisions of one interactive run. Every field is plain data so the use case
// reads them back after the prompt session ends.
type Answers struct {
	Scope   Scope
	Skills  []string // selected skill names
	Agents  []string // selected agent identifiers
	Mode    install.Mode
	Proceed bool
}

// Prompter asks the questions of one flow and returns the answers at once. A prompter that cannot
// ask returns ErrPromptRequired; a cancelled flow returns ErrCancelled.
type Prompter interface {
	Ask(Questions) (Answers, error)
}

// SkillChoice is one skill as the selector presents it.
type SkillChoice struct {
	Name        string // sanitized skill name; the selected identifier
	Description string
	Group       string // plugin name, or General when groups exist
	Path        string // SKILL.md path relative to the source root
}

// AgentChoice is one agent as the picker presents it.
type AgentChoice struct {
	ID        string
	Name      string
	Dir       string // project skills directory, the picker's hint
	Universal bool   // part of the locked Universal (.agents/skills) section
	Hidden    bool   // counted, not listed, inside the locked section
	Detected  bool   // a detected agent is preselected
}

// Questions is everything the flow needs to ask, compiled by PlanQuestions.
type Questions struct {
	Steps    []StepID      // prompts to show, in order
	Source   string        // source as the user wrote it
	Skills   []SkillChoice // every discovered skill, in display order
	Agents   []AgentChoice // every pickable agent, in registry order
	Message  string        // optional confirmation text, used by the remove flow
	Defaults Answers       // answers for the steps that are not asked
}

// Facts are the discovery facts the prompt matrix and the selector need.
type Facts struct {
	Source          string
	Skills          []skill.Skill
	RequestedSkills []string       // --skill names merged with the source's @skill selector
	Detected        []agents.Agent // agents detected on this machine
	Candidates      []agents.Agent // agents the run targets without a picker answer
	All             []agents.Agent // every registry agent, in registry order
	EveDetected     bool           // an Eve project was detected
	CanGoGlobal     bool           // at least one candidate has a global skills directory
	Mode            install.Mode   // mode the run resolves to before any answer
}

// PlanSteps compiles the documented prompt matrix into the ordered list of prompts to show. An
// empty list means the run is fully determined by flags and facts and no prompter is needed.
func PlanSteps(opts AddOptions, facts Facts) []StepID {
	if !promptsWanted(opts) {
		return nil
	}
	var steps []StepID
	if len(facts.Skills) > 1 && len(facts.RequestedSkills) == 0 {
		steps = append(steps, StepSkills)
	}
	if len(opts.Agents) == 0 {
		if facts.EveDetected {
			steps = append(steps, StepEve)
		}
		if !singleNonEveDetected(facts.Detected) {
			steps = append(steps, StepAgents)
		}
	}
	if !opts.Global && facts.CanGoGlobal {
		steps = append(steps, StepScope)
	}
	if !opts.Copy && facts.Mode == install.ModeSymlink {
		steps = append(steps, StepMethod)
	}
	return append(steps, StepConfirm)
}

// PlanQuestions builds the questions of a run from its flags and discovery facts. It is pure:
// the same inputs always yield the same steps and choices.
func PlanQuestions(opts AddOptions, facts Facts) Questions {
	q := Questions{
		Steps:  PlanSteps(opts, facts),
		Source: facts.Source,
		Skills: skillChoices(facts.Skills),
		Agents: agentChoices(facts.All, facts.Detected),
		Defaults: Answers{
			Scope:  ScopeProject,
			Agents: agentIDs(facts.Candidates),
			Mode:   facts.Mode,
		},
	}
	names := make([]string, 0, len(facts.Skills))
	for _, s := range facts.Skills {
		names = append(names, s.Name)
	}
	q.Defaults.Skills = names
	return q
}

// promptsWanted reports whether the run may ask anything at all.
func promptsWanted(opts AddOptions) bool {
	return !opts.Yes && !opts.List && !opts.JSON
}

// singleNonEveDetected reports the documented auto-selection: exactly one installed agent that is
// not Eve means no agent picker.
func singleNonEveDetected(detected []agents.Agent) bool {
	return len(detected) == 1 && detected[0].ID != "eve"
}

// skillChoices orders skills by group and then display name, as upstream does, and names the
// ungrouped ones General when the source declares plugin groups.
func skillChoices(skills []skill.Skill) []SkillChoice {
	grouped := false
	for _, s := range skills {
		if s.PluginName != "" {
			grouped = true
			break
		}
	}
	out := make([]SkillChoice, 0, len(skills))
	for _, s := range skills {
		group := s.PluginName
		if group == "" && grouped {
			group = "General"
		}
		out = append(out, SkillChoice{
			Name:        s.Name,
			Description: s.Description,
			Group:       group,
			Path:        s.SkillPath(),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// agentChoices marks the locked universal agents and the detected ones. Eve is never offered by
// the picker: it is handled by its own confirmation step or by an explicit --agent.
func agentChoices(all, detected []agents.Agent) []AgentChoice {
	found := make(map[string]bool, len(detected))
	for _, a := range detected {
		found[a.ID] = true
	}
	out := make([]AgentChoice, 0, len(all))
	for _, a := range all {
		if a.ID == "eve" {
			continue
		}
		out = append(out, AgentChoice{
			ID:        a.ID,
			Name:      a.DisplayName,
			Dir:       a.ProjectDir,
			Universal: lockedUniversal(a),
			Hidden:    a.HiddenFromPrompt,
			Detected:  found[a.ID],
		})
	}
	return out
}

// lockedUniversal reports whether an agent belongs to the always-included, non-toggleable section
// of the picker, matching the universal agents a non-interactive run adds.
func lockedUniversal(a agents.Agent) bool {
	return a.Universal && a.ReadsCanonicalGlobally && !a.NotAutoAdded
}

func agentIDs(list []agents.Agent) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

// NoPrompts is the production guard for runs that must not prompt. The use case turns its error
// into the documented non-terminal diagnostic.
type NoPrompts struct{}

// Ask always reports that a prompt was required.
func (NoPrompts) Ask(Questions) (Answers, error) {
	return Answers{}, ErrPromptRequired
}

// Failer is the subset of testing.TB the scripted prompter uses to fail on a script mismatch.
type Failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

// Scripted is a Prompter test double: it replays queued answers and fails the test when the flow
// asks for a prompt the queue does not contain.
type Scripted struct {
	t     Failer
	queue []Answers
	asked []Questions
}

// NewScripted returns a scripted prompter with the given answers queued in order.
func NewScripted(t Failer, answers ...Answers) *Scripted {
	return &Scripted{t: t, queue: answers}
}

// Ask records the question and returns the next scripted answer.
func (s *Scripted) Ask(q Questions) (Answers, error) {
	s.t.Helper()
	s.asked = append(s.asked, q)
	if len(s.queue) == 0 {
		msg := fmt.Sprintf("unexpected prompt: steps %v", q.Steps)
		s.t.Fatalf("%s", msg)
		return Answers{}, errors.New(msg)
	}
	answer := s.queue[0]
	s.queue = s.queue[1:]
	return answer, nil
}

// Asked returns the questions the flow asked, in order.
func (s *Scripted) Asked() []Questions {
	return s.asked
}

// OfferScripted is an OfferPrompter test double: it answers the one-time find-skills question with
// a fixed answer and records that it was asked.
type OfferScripted struct {
	Accept bool
	Err    error
	asked  int
}

// AskFindSkillsOffer answers the offer.
func (o *OfferScripted) AskFindSkillsOffer() (bool, error) {
	o.asked++
	if o.Err != nil {
		return false, o.Err
	}
	return o.Accept, nil
}

// Asked reports how many times the offer was asked.
func (o *OfferScripted) Asked() int {
	return o.asked
}
