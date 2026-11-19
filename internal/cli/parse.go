package cli

import (
	"strings"

	"github.com/nhermosilla14/go-skills/internal/app"
)

// Kind is the kind of command line.
type Kind int

const (
	KindHelp Kind = iota
	KindVersion
	KindAdd
	KindList
	KindCheck
	KindRemove
	KindUpdate
	KindRestore
	KindUse
	KindInit
)

// Command is a parsed command line.
type Command struct {
	Kind    Kind
	Add     app.AddOptions     // set when Kind is KindAdd
	List    app.ListOptions    // set when Kind is KindList
	Check   app.CheckOptions   // set when Kind is KindCheck
	Remove  app.RemoveOptions  // set when Kind is KindRemove
	Update  app.UpdateOptions  // set when Kind is KindUpdate
	Restore app.RestoreOptions // set when Kind is KindRestore
	Use     UseOptions         // set when Kind is KindUse
	Init    app.InitOptions    // set when Kind is KindInit
	UseHelp bool               // use asked for its own help
	JSON    bool               // --json, for list
}

// UseOptions is the parsed `use` command line before validation. Skills and Agents keep every
// value; the CLI rejects more than one of either.
type UseOptions struct {
	Source    string
	Skills    []string
	Agents    []string
	FullDepth bool
}

// addNames are the spellings of the add command. install and i are add with a source.
var addNames = map[string]bool{"add": true, "a": true, "install": true, "i": true}

// Parse turns the arguments after the program name into a command. Unknown commands, unknown
// options, extra sources and malformed values are UsageErrors.
func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Kind: KindHelp}, nil
	}
	name := args[0]
	switch {
	case name == "-h" || name == "--help" || name == "help":
		return Command{Kind: KindHelp}, nil
	case name == "--version" || name == "version":
		return Command{Kind: KindVersion}, nil
	case addNames[name]:
		return parseAdd(name, args[1:])
	case name == "experimental_install":
		return parseRestore(args[1:])
	case name == "list" || name == "ls":
		return parseList(args[1:])
	case name == "check":
		return parseCheck(args[1:])
	case name == "update" || name == "upgrade":
		return parseUpdate(args[1:])
	case name == "remove" || name == "rm" || name == "r":
		return parseRemove(args[1:])
	case name == "use":
		return parseUse(args[1:])
	case name == "init":
		return parseInit(args[1:])
	case strings.HasPrefix(name, "-"):
		return Command{}, usagef("unknown option '%s'", name)
	default:
		return Command{}, usagef("unknown command '%s'", name)
	}
}

// addParser accumulates the options of add while the arguments are read.
type addParser struct {
	opts     app.AddOptions
	help     bool
	global   bool
	all      bool
	restore  bool   // install or i: no source means restore
	force    bool   // --force, restore only
	dryRun   bool   // --dry-run, restore only
	frozen   bool   // --frozen-lockfile, restore only
	noFrozen bool   // --no-frozen-lockfile, restore only
	addOnly  string // the first add-only option that cannot apply to a restore
}

// shortNames maps the single-letter switches of add to their long names.
var shortNames = map[byte]string{'a': "agent", 's': "skill", 'y': "yes", 'l': "list", 'g': "global", 'h': "help"}

func parseAdd(name string, args []string) (Command, error) {
	p := &addParser{restore: name == "install" || name == "i"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch {
		case arg == "--":
			for _, rest := range args[i+1:] {
				if err = p.addSource(rest); err != nil {
					break
				}
			}
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			i, err = p.long(args, i)
		case strings.HasPrefix(arg, "-") && arg != "-":
			i, err = p.short(args, i)
		default:
			err = p.addSource(arg)
		}
		if err != nil {
			return Command{}, err
		}
	}
	if p.help {
		return Command{Kind: KindHelp}, nil
	}
	if p.all {
		p.opts.Skills = []string{"*"}
		p.opts.Agents = []string{"*"}
		p.opts.Yes = true
	}
	p.opts.Global = p.global
	if p.opts.Source == "" {
		if !p.restore {
			return Command{}, usagef("missing required argument: source")
		}
		if err := p.restoreError(); err != nil {
			return Command{}, err
		}
		return Command{Kind: KindRestore, Restore: app.RestoreOptions{
			Agents: p.opts.Agents, Force: p.force, DryRun: p.dryRun,
			Frozen: resolveFrozen(p.frozen, p.noFrozen),
		}}, nil
	}
	if p.force {
		return Command{}, usagef("--force only applies when restoring from skills-lock.json")
	}
	if p.dryRun {
		return Command{}, usagef("--dry-run only applies when restoring from skills-lock.json")
	}
	if flag := frozenFlagName(p.frozen, p.noFrozen); flag != "" {
		return Command{}, usagef("%s only applies when restoring from skills-lock.json", flag)
	}
	return Command{Kind: KindAdd, Add: p.opts}, nil
}

// resolveFrozen maps the two flags to the mode: --frozen-lockfile wins over --no-frozen-lockfile,
// and neither flag follows the environment.
func resolveFrozen(frozen, noFrozen bool) app.FrozenMode {
	if frozen {
		return app.FrozenEnabled
	}
	if noFrozen {
		return app.FrozenDisabled
	}
	return app.FrozenFromEnvironment
}

// frozenFlagName names the frozen option on the command line, or "" when neither was given.
func frozenFlagName(frozen, noFrozen bool) string {
	if frozen {
		return "--frozen-lockfile"
	}
	if noFrozen {
		return "--no-frozen-lockfile"
	}
	return ""
}

// restoreError rejects the options that cannot apply to a restore from the lock.
func (p *addParser) restoreError() error {
	if p.global {
		return usagef("--global cannot be used when restoring from skills-lock.json")
	}
	if p.addOnly != "" {
		return usagef("%s does not apply when restoring from skills-lock.json", p.addOnly)
	}
	return nil
}

func (p *addParser) addSource(arg string) error {
	if p.opts.Source != "" {
		return usagef("unexpected argument '%s': only one source is allowed", arg)
	}
	p.opts.Source = arg
	return nil
}

// long parses --name, --name=value and the list options that take following arguments.
func (p *addParser) long(args []string, i int) (int, error) {
	name, value, hasValue := strings.Cut(args[i][2:], "=")
	switch name {
	case "agent", "skill", "subagent":
		return p.repeatable(args, i, name, value, hasValue)
	case "rename":
		return p.value(args, i, name, value, hasValue)
	case "force", "dry-run", "frozen-lockfile", "no-frozen-lockfile":
		if !p.restore {
			return i, usagef("unknown option '--%s'", name)
		}
		if hasValue {
			return i, usagef("--%s does not take a value", name)
		}
		switch name {
		case "force":
			p.force = true
		case "dry-run":
			p.dryRun = true
		case "frozen-lockfile":
			p.frozen = true
		default:
			p.noFrozen = true
		}
		return i, nil
	case "copy", "yes", "list", "global", "help", "all", "full-depth", "json":
		if hasValue {
			return i, usagef("--%s does not take a value", name)
		}
		p.boolean(name)
		return i, nil
	}
	return i, usagef("unknown option '--%s'", name)
}

// short parses -x, grouped switches such as -gy, and -a/-s which may end a group.
func (p *addParser) short(args []string, i int) (int, error) {
	group, value, hasValue := strings.Cut(args[i][1:], "=")
	for k := 0; k < len(group); k++ {
		letter := group[k]
		name, ok := shortNames[letter]
		if !ok {
			return i, usagef("unknown option '-%c'", letter)
		}
		if name == "agent" || name == "skill" {
			if k != len(group)-1 {
				return i, usagef("-%c takes a value and must be last in its group", letter)
			}
			return p.repeatable(args, i, name, value, hasValue)
		}
		if hasValue && k == len(group)-1 {
			return i, usagef("-%c does not take a value", letter)
		}
		p.boolean(name)
	}
	return i, nil
}

// collectValues reads the values of a repeatable option. A value given with "=" is the only one;
// otherwise the values are the following arguments up to the next option.
func collectValues(args []string, i int, inline string, hasInline bool) ([]string, int) {
	if hasInline {
		return []string{inline}, i
	}
	var values []string
	last := i
	for last+1 < len(args) && !strings.HasPrefix(args[last+1], "-") {
		last++
		values = append(values, args[last])
	}
	return values, last
}

// repeatable reads the values of -a/--agent or -s/--skill. Values accumulate.
func (p *addParser) repeatable(args []string, i int, name, inline string, hasInline bool) (int, error) {
	values, last := collectValues(args, i, inline, hasInline)
	if len(values) == 0 || (hasInline && inline == "") {
		noun := "an agent name"
		switch name {
		case "skill":
			noun = "a skill name"
		case "subagent":
			noun = "a subagent name"
		}
		return i, usagef("--%s requires %s", name, noun)
	}
	switch name {
	case "agent":
		p.opts.Agents = append(p.opts.Agents, values...)
	case "skill":
		p.opts.Skills = append(p.opts.Skills, values...)
		p.markAddOnly("--skill")
	case "subagent":
		p.opts.Subagents = append(p.opts.Subagents, values...)
		p.markAddOnly("--subagent")
	}
	return last, nil
}

// value reads the single value of --rename. The last occurrence wins.
func (p *addParser) value(args []string, i int, name, inline string, hasInline bool) (int, error) {
	if hasInline {
		if inline == "" {
			return i, usagef("--%s requires a skill name", name)
		}
		p.opts.Rename = inline
		p.markAddOnly("--rename")
		return i, nil
	}
	if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
		return i, usagef("--%s requires a skill name", name)
	}
	p.opts.Rename = args[i+1]
	p.markAddOnly("--rename")
	return i + 1, nil
}

func (p *addParser) boolean(name string) {
	switch name {
	case "copy":
		p.opts.Copy = true
		p.markAddOnly("--copy")
	case "yes":
		p.opts.Yes = true
	case "list":
		p.opts.List = true
		p.markAddOnly("--list")
	case "global":
		p.global = true
	case "help":
		p.help = true
	case "all":
		p.all = true
		p.markAddOnly("--all")
	case "full-depth":
		p.opts.FullDepth = true
		p.markAddOnly("--full-depth")
	case "json":
		p.opts.JSON = true
		p.markAddOnly("--json")
	}
}

// markAddOnly remembers the first option that cannot apply to a restore from the lock.
func (p *addParser) markAddOnly(option string) {
	if p.addOnly == "" {
		p.addOnly = option
	}
}

// restoreParser accumulates the options of experimental_install.
type restoreParser struct {
	opts     app.RestoreOptions
	help     bool
	frozen   bool // --frozen-lockfile
	noFrozen bool // --no-frozen-lockfile
}

// parseRestore parses `experimental_install`, which always restores: it takes no source, and -g is
// a usage error because restore is project-only.
func parseRestore(args []string) (Command, error) {
	p := &restoreParser{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch {
		case arg == "--":
			if len(args) > i+1 {
				err = usagef("unexpected argument '%s'", args[i+1])
			}
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			i, err = p.long(args, i)
		case strings.HasPrefix(arg, "-") && arg != "-":
			i, err = p.short(args, i)
		default:
			err = usagef("unexpected argument '%s'", arg)
		}
		if err != nil {
			return Command{}, err
		}
	}
	if p.help {
		return Command{Kind: KindHelp}, nil
	}
	p.opts.Frozen = resolveFrozen(p.frozen, p.noFrozen)
	return Command{Kind: KindRestore, Restore: p.opts}, nil
}

func (p *restoreParser) long(args []string, i int) (int, error) {
	name, value, hasValue := strings.Cut(args[i][2:], "=")
	switch name {
	case "agent":
		return p.agent(args, i, value, hasValue)
	case "force", "dry-run", "frozen-lockfile", "no-frozen-lockfile", "help":
		if hasValue {
			return i, usagef("--%s does not take a value", name)
		}
		switch name {
		case "force":
			p.opts.Force = true
		case "dry-run":
			p.opts.DryRun = true
		case "frozen-lockfile":
			p.frozen = true
		case "no-frozen-lockfile":
			p.noFrozen = true
		case "help":
			p.help = true
		}
		return i, nil
	case "global":
		return i, usagef("--global cannot be used when restoring from skills-lock.json")
	}
	return i, usagef("unknown option '--%s'", name)
}

func (p *restoreParser) short(args []string, i int) (int, error) {
	group, value, hasValue := strings.Cut(args[i][1:], "=")
	for k := 0; k < len(group); k++ {
		letter := group[k]
		switch letter {
		case 'a':
			if k != len(group)-1 {
				return i, usagef("-a takes a value and must be last in its group")
			}
			return p.agent(args, i, value, hasValue)
		case 'g':
			return i, usagef("-g cannot be used when restoring from skills-lock.json")
		case 'h':
			if hasValue && k == len(group)-1 {
				return i, usagef("-%c does not take a value", letter)
			}
			p.help = true
		default:
			return i, usagef("unknown option '-%c'", letter)
		}
	}
	return i, nil
}

func (p *restoreParser) agent(args []string, i int, value string, hasValue bool) (int, error) {
	values, last := collectValues(args, i, value, hasValue)
	if len(values) == 0 || (hasValue && value == "") {
		return i, usagef("--agent requires an agent name")
	}
	p.opts.Agents = append(p.opts.Agents, values...)
	return last, nil
}

// listParser accumulates the options of list.
type listParser struct {
	opts   app.ListOptions
	json   bool
	global bool
	help   bool
}

// parseList parses `list` / `ls`: repeated --agent values, --json, and the rejected --global.
func parseList(args []string) (Command, error) {
	p := &listParser{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch {
		case arg == "--":
			for _, rest := range args[i+1:] {
				if err = p.unexpected(rest); err != nil {
					break
				}
			}
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			i, err = p.long(args, i)
		case strings.HasPrefix(arg, "-") && arg != "-":
			i, err = p.short(args, i)
		default:
			err = p.unexpected(arg)
		}
		if err != nil {
			return Command{}, err
		}
	}
	if p.help {
		return Command{Kind: KindHelp}, nil
	}
	p.opts.Global = p.global
	return Command{Kind: KindList, List: p.opts, JSON: p.json}, nil
}

func (p *listParser) long(args []string, i int) (int, error) {
	name, value, hasValue := strings.Cut(args[i][2:], "=")
	switch name {
	case "agent":
		return p.agent(args, i, value, hasValue)
	case "json", "global", "help":
		if hasValue {
			return i, usagef("--%s does not take a value", name)
		}
		switch name {
		case "json":
			p.json = true
		case "global":
			p.global = true
		case "help":
			p.help = true
		}
		return i, nil
	}
	return i, usagef("unknown option '--%s'", name)
}

func (p *listParser) short(args []string, i int) (int, error) {
	group, value, hasValue := strings.Cut(args[i][1:], "=")
	for k := 0; k < len(group); k++ {
		letter := group[k]
		switch letter {
		case 'a':
			if k != len(group)-1 {
				return i, usagef("-a takes a value and must be last in its group")
			}
			return p.agent(args, i, value, hasValue)
		case 'g', 'h':
			if hasValue && k == len(group)-1 {
				return i, usagef("-%c does not take a value", letter)
			}
			if letter == 'g' {
				p.global = true
			} else {
				p.help = true
			}
		default:
			return i, usagef("unknown option '-%c'", letter)
		}
	}
	return i, nil
}

func (p *listParser) agent(args []string, i int, value string, hasValue bool) (int, error) {
	values, last := collectValues(args, i, value, hasValue)
	if len(values) == 0 || (hasValue && value == "") {
		return i, usagef("--agent requires an agent name")
	}
	p.opts.Agents = append(p.opts.Agents, values...)
	return last, nil
}

func (p *listParser) unexpected(arg string) error {
	return usagef("unexpected argument '%s'", arg)
}

// checkParser accumulates the optional skill names of check.
type checkParser struct {
	opts   app.CheckOptions
	global bool
	help   bool
}

// parseCheck parses `check [skill...]`: positional skill names limit the check, --global is
// rejected until global scope arrives.
func parseCheck(args []string) (Command, error) {
	p := &checkParser{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			p.opts.Skills = append(p.opts.Skills, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			name, _, hasValue := strings.Cut(arg[2:], "=")
			if name != "global" && name != "help" {
				return Command{}, usagef("unknown option '--%s'", name)
			}
			if hasValue {
				return Command{}, usagef("--%s does not take a value", name)
			}
			if name == "global" {
				p.global = true
			} else {
				p.help = true
			}
		case strings.HasPrefix(arg, "-") && arg != "-":
			group, _, hasValue := strings.Cut(arg[1:], "=")
			for k := 0; k < len(group); k++ {
				letter := group[k]
				if letter != 'g' && letter != 'h' {
					return Command{}, usagef("unknown option '-%c'", letter)
				}
				if hasValue && k == len(group)-1 {
					return Command{}, usagef("-%c does not take a value", letter)
				}
				if letter == 'g' {
					p.global = true
				} else {
					p.help = true
				}
			}
		default:
			p.opts.Skills = append(p.opts.Skills, arg)
		}
	}
	if p.help {
		return Command{Kind: KindHelp}, nil
	}
	p.opts.Global = p.global
	return Command{Kind: KindCheck, Check: p.opts}, nil
}

// updateParser accumulates the options of update.
type updateParser struct {
	opts   app.UpdateOptions
	global bool
	help   bool
}

// parseUpdate parses `update` / `upgrade`: positional skill names, -p and -y, and the rejected
// -g until global scope arrives.
func parseUpdate(args []string) (Command, error) {
	p := &updateParser{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			p.opts.Skills = append(p.opts.Skills, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			name, _, hasValue := strings.Cut(arg[2:], "=")
			if name != "project" && name != "yes" && name != "global" && name != "help" {
				return Command{}, usagef("unknown option '--%s'", name)
			}
			if hasValue {
				return Command{}, usagef("--%s does not take a value", name)
			}
			p.boolean(name)
		case strings.HasPrefix(arg, "-") && arg != "-":
			group, _, hasValue := strings.Cut(arg[1:], "=")
			for k := 0; k < len(group); k++ {
				letter := group[k]
				name, ok := updateShortNames[letter]
				if !ok {
					return Command{}, usagef("unknown option '-%c'", letter)
				}
				if hasValue && k == len(group)-1 {
					return Command{}, usagef("-%c does not take a value", letter)
				}
				p.boolean(name)
			}
		default:
			p.opts.Skills = append(p.opts.Skills, arg)
		}
	}
	if p.help {
		return Command{Kind: KindHelp}, nil
	}
	p.opts.Global = p.global
	return Command{Kind: KindUpdate, Update: p.opts}, nil
}

// updateShortNames maps the single-letter switches of update to their long names.
var updateShortNames = map[byte]string{'p': "project", 'y': "yes", 'g': "global", 'h': "help"}

func (p *updateParser) boolean(name string) {
	switch name {
	case "project":
		p.opts.Project = true
	case "yes":
		p.opts.Yes = true
	case "global":
		p.global = true
	case "help":
		p.help = true
	}
}

// removeParser accumulates the options of remove.
type removeParser struct {
	opts   app.RemoveOptions
	global bool
	help   bool
}

// parseRemove parses `remove` / `rm` / `r`: repeated --agent and --skill values, -y, --all and the
// rejected --global. Positionals are skill names or an exact lock source.
func parseRemove(args []string) (Command, error) {
	p := &removeParser{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch {
		case arg == "--":
			p.opts.Skills = append(p.opts.Skills, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			i, err = p.long(args, i)
		case strings.HasPrefix(arg, "-") && arg != "-":
			i, err = p.short(args, i)
		default:
			p.opts.Skills = append(p.opts.Skills, arg)
		}
		if err != nil {
			return Command{}, err
		}
	}
	if p.help {
		return Command{Kind: KindHelp}, nil
	}
	p.opts.Global = p.global
	if p.opts.All {
		p.opts.Agents = []string{"*"}
		p.opts.Yes = true
	}
	return Command{Kind: KindRemove, Remove: p.opts}, nil
}

func (p *removeParser) long(args []string, i int) (int, error) {
	name, value, hasValue := strings.Cut(args[i][2:], "=")
	switch name {
	case "agent", "skill":
		return p.repeatable(args, i, name, value, hasValue)
	case "yes", "all", "global", "help":
		if hasValue {
			return i, usagef("--%s does not take a value", name)
		}
		switch name {
		case "yes":
			p.opts.Yes = true
		case "all":
			p.opts.All = true
		case "global":
			p.global = true
		case "help":
			p.help = true
		}
		return i, nil
	}
	return i, usagef("unknown option '--%s'", name)
}

func (p *removeParser) short(args []string, i int) (int, error) {
	group, value, hasValue := strings.Cut(args[i][1:], "=")
	for k := 0; k < len(group); k++ {
		letter := group[k]
		switch letter {
		case 'a', 's':
			if k != len(group)-1 {
				return i, usagef("-%c takes a value and must be last in its group", letter)
			}
			name := "agent"
			if letter == 's' {
				name = "skill"
			}
			return p.repeatable(args, i, name, value, hasValue)
		case 'y', 'g', 'h':
			if hasValue && k == len(group)-1 {
				return i, usagef("-%c does not take a value", letter)
			}
			switch letter {
			case 'y':
				p.opts.Yes = true
			case 'g':
				p.global = true
			case 'h':
				p.help = true
			}
		default:
			return i, usagef("unknown option '-%c'", letter)
		}
	}
	return i, nil
}

func (p *removeParser) repeatable(args []string, i int, name, inline string, hasInline bool) (int, error) {
	values, last := collectValues(args, i, inline, hasInline)
	if len(values) == 0 || (hasInline && inline == "") {
		noun := "an agent name"
		if name == "skill" {
			noun = "a skill name"
		}
		return i, usagef("--%s requires %s", name, noun)
	}
	if name == "agent" {
		p.opts.Agents = append(p.opts.Agents, values...)
	} else {
		p.opts.Skills = append(p.opts.Skills, values...)
	}
	return last, nil
}

// useParser accumulates the options of use.
type useParser struct {
	opts      UseOptions
	hasSource bool
	help      bool
}

// parseUse parses `use <source>[@skill]`: repeated --skill and --agent values (validated later),
// --full-depth, and its own help. A missing source is not a parse error: the caller reports it
// with the use help.
func parseUse(args []string) (Command, error) {
	if len(args) == 1 && args[0] == "help" {
		return Command{Kind: KindUse, UseHelp: true}, nil
	}
	p := &useParser{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var err error
		switch {
		case arg == "--":
			for _, rest := range args[i+1:] {
				if err = p.source(rest); err != nil {
					break
				}
			}
			i = len(args)
		case strings.HasPrefix(arg, "--"):
			i, err = p.long(args, i)
		case strings.HasPrefix(arg, "-") && arg != "-":
			i, err = p.short(args, i)
		default:
			err = p.source(arg)
		}
		if err != nil {
			return Command{}, err
		}
	}
	if p.help {
		return Command{Kind: KindUse, UseHelp: true}, nil
	}
	return Command{Kind: KindUse, Use: p.opts}, nil
}

func (p *useParser) source(arg string) error {
	if p.hasSource {
		return usagef("unexpected argument '%s': only one source is allowed", arg)
	}
	p.hasSource = true
	p.opts.Source = arg
	return nil
}

func (p *useParser) long(args []string, i int) (int, error) {
	name, value, hasValue := strings.Cut(args[i][2:], "=")
	switch name {
	case "agent", "skill":
		return p.repeatable(args, i, name, value, hasValue)
	case "full-depth", "help":
		if hasValue {
			return i, usagef("--%s does not take a value", name)
		}
		if name == "help" {
			p.help = true
		} else {
			p.opts.FullDepth = true
		}
		return i, nil
	}
	return i, usagef("unknown option '--%s'", name)
}

func (p *useParser) short(args []string, i int) (int, error) {
	group, value, hasValue := strings.Cut(args[i][1:], "=")
	for k := 0; k < len(group); k++ {
		letter := group[k]
		switch letter {
		case 'a', 's':
			if k != len(group)-1 {
				return i, usagef("-%c takes a value and must be last in its group", letter)
			}
			name := "agent"
			if letter == 's' {
				name = "skill"
			}
			return p.repeatable(args, i, name, value, hasValue)
		case 'h':
			if hasValue && k == len(group)-1 {
				return i, usagef("-%c does not take a value", letter)
			}
			p.help = true
		default:
			return i, usagef("unknown option '-%c'", letter)
		}
	}
	return i, nil
}

func (p *useParser) repeatable(args []string, i int, name, inline string, hasInline bool) (int, error) {
	values, last := collectValues(args, i, inline, hasInline)
	if len(values) == 0 || (hasInline && inline == "") {
		noun := "an agent name"
		if name == "skill" {
			noun = "a skill name"
		}
		return i, usagef("--%s requires %s", name, noun)
	}
	if name == "agent" {
		p.opts.Agents = append(p.opts.Agents, values...)
	} else {
		p.opts.Skills = append(p.opts.Skills, values...)
	}
	return last, nil
}

// parseInit parses `init [name]`: one optional positional name, and the rejected extra positionals
// and options.
func parseInit(args []string) (Command, error) {
	names := make([]string, 0, 1)
	help := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			names = append(names, args[i+1:]...)
			i = len(args)
		case arg == "-h" || arg == "--help":
			help = true
		case strings.HasPrefix(arg, "-") && arg != "-":
			return Command{}, usagef("unknown option '%s'", arg)
		default:
			names = append(names, arg)
		}
	}
	if help {
		return Command{Kind: KindHelp}, nil
	}
	if len(names) > 1 {
		return Command{}, usagef("unexpected argument '%s': only one skill name is allowed", names[1])
	}
	if len(names) == 1 {
		if names[0] == "" {
			return Command{}, usagef("skill name must not be empty")
		}
		return Command{Kind: KindInit, Init: app.InitOptions{Name: names[0]}}, nil
	}
	return Command{Kind: KindInit}, nil
}
