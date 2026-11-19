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
)

// Command is a parsed command line.
type Command struct {
	Kind   Kind
	Add    app.AddOptions    // set when Kind is KindAdd
	List   app.ListOptions   // set when Kind is KindList
	Check  app.CheckOptions  // set when Kind is KindCheck
	Remove app.RemoveOptions // set when Kind is KindRemove
	Update app.UpdateOptions // set when Kind is KindUpdate
	JSON   bool              // --json, for list
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
	case name == "list" || name == "ls":
		return parseList(args[1:])
	case name == "check":
		return parseCheck(args[1:])
	case name == "update" || name == "upgrade":
		return parseUpdate(args[1:])
	case name == "remove" || name == "rm" || name == "r":
		return parseRemove(args[1:])
	case strings.HasPrefix(name, "-"):
		return Command{}, usagef("unknown option '%s'", name)
	default:
		return Command{}, usagef("unknown command '%s'", name)
	}
}

// addParser accumulates the options of add while the arguments are read.
type addParser struct {
	opts   app.AddOptions
	help   bool
	global bool
	all    bool
}

// shortNames maps the single-letter switches of add to their long names.
var shortNames = map[byte]string{'a': "agent", 's': "skill", 'y': "yes", 'l': "list", 'g': "global", 'h': "help"}

func parseAdd(name string, args []string) (Command, error) {
	p := &addParser{}
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
	if p.global {
		return Command{}, usagef("global scope is not supported yet")
	}
	if p.opts.Source == "" {
		if name == "install" || name == "i" {
			return Command{}, usagef("restoring from skills-lock.json is not supported yet; pass a source")
		}
		return Command{}, usagef("missing required argument: source")
	}
	return Command{Kind: KindAdd, Add: p.opts}, nil
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
	case "subagent":
		p.opts.Subagents = append(p.opts.Subagents, values...)
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
		return i, nil
	}
	if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
		return i, usagef("--%s requires a skill name", name)
	}
	p.opts.Rename = args[i+1]
	return i + 1, nil
}

func (p *addParser) boolean(name string) {
	switch name {
	case "copy":
		p.opts.Copy = true
	case "yes":
		p.opts.Yes = true
	case "list":
		p.opts.List = true
	case "global":
		p.global = true
	case "help":
		p.help = true
	case "all":
		p.all = true
	case "full-depth":
		p.opts.FullDepth = true
	case "json":
		p.opts.JSON = true
	}
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
	if p.global {
		return Command{}, usagef("global scope is not supported yet")
	}
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
	if p.global {
		return Command{}, usagef("global scope is not supported yet")
	}
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
	if p.global {
		return Command{}, usagef("global scope is not supported yet")
	}
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
	if p.global {
		return Command{}, usagef("global scope is not supported yet")
	}
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
