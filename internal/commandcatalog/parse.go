// Package commandcatalog builds the list of server commands and their
// syntax, used for autocompletion in the console. It is learnt from the
// running server itself (`/commands dump` and `/help <command>`), so mod
// commands are included.
package commandcatalog

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// Arg describes one argument of a command syntax.
type Arg struct {
	// Name is "player" for a positional argument, "--title" for a flag.
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
	Flag     bool   `json:"flag,omitempty"`
	// TakesValue is false for boolean flags ("--force").
	TakesValue  bool     `json:"takesValue,omitempty"`
	Type        string   `json:"type,omitempty"`
	Default     string   `json:"default,omitempty"`
	Description string   `json:"description,omitempty"`
	Values      []string `json:"values,omitempty"`
}

// Entry is one invocable syntax of a command.
type Entry struct {
	// Command is the command path without slash, e.g. "tp all".
	Command string `json:"command"`
	// Usage is the human readable syntax, e.g. "<x> <y> <z> [--pitch <?>]".
	Usage       string `json:"usage"`
	Description string `json:"description,omitempty"`
	Args        []Arg  `json:"args"`
}

var (
	typedRequired = regexp.MustCompile(`<([A-Za-z0-9_-]+):[^>]*>`)
	typedOptional = regexp.MustCompile(`\[([A-Za-z0-9_=?-]+):[^\]]*\]`)
	usageToken    = regexp.MustCompile(`<[^>]+>|\[[^\]]+\]`)
	spaces        = regexp.MustCompile(`\s+`)
	leafHeader    = regexp.MustCompile(`^([A-Za-z0-9_-]+(?: [A-Za-z0-9_-]+)*):\s*(.*)$`)
	separator     = regexp.MustCompile(`^-{5,}$`)
	identifier    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	wordPattern   = regexp.MustCompile(`[A-Za-z0-9_-]+`)

	// Lines of the argument sections of a single command's help:
	//   player (Player UUID/Username (with lookup)) -> "The player to ban"
	//   --title Text -> "The primary title text"
	//   --secondary (Text:default=Event) -> "The secondary title text"
	//   Event Title Style: default, major, goblinbreach, voideviction
	//       Examples: 'default', 'major', 'goblinbreach', 'voideviction'.
	requiredLine = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s+\((.*)\)\s*->\s*"(.*)"\s*$`)
	defaultLine  = regexp.MustCompile(`^(--[A-Za-z0-9_-]+)\s+\((.*?):default=(.*)\)\s*->\s*"(.*)"\s*$`)
	optionalLine = regexp.MustCompile(`^(--[A-Za-z0-9_-]+)(?:\s+(.+?))?\s*->\s*"(.*)"\s*$`)
	typeLine     = regexp.MustCompile(`^([^:]+):\s*(.*)$`)
	examplesLine = regexp.MustCompile(`^Examples:\s*(.*)$`)
	quoted       = regexp.MustCompile(`'([^']*)'`)
)

// ParseDump extracts the command paths ("ping clear") of commands.dump.json.
func ParseDump(data []byte) ([]string, error) {
	var dump struct {
		Modern []struct {
			Name string `json:"name"`
		} `json:"modern"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(dump.Modern))
	for _, c := range dump.Modern {
		if p := strings.TrimSpace(strings.TrimPrefix(c.Name, "/")); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Roots returns the distinct top-level commands of paths, sorted.
func Roots(paths []string) []string {
	seen := map[string]bool{}
	var roots []string
	for _, p := range paths {
		root, _, _ := strings.Cut(p, " ")
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	slices.Sort(roots)
	return roots
}

// ParseHelp turns the output of `/help <root>` into entries. The output is
// made of blocks separated by dashes. A block documenting one syntax starts
// with "name: description" (name being the root or one of its
// subcommands) and lists its arguments in sections; a block starting with
// "/root" lists one syntax per indented line (command groups).
func ParseHelp(root string, lines []string) []Entry {
	var entries []Entry
	for _, block := range splitBlocks(lines) {
		if name, description, ok := blockHeader(root, block); ok {
			entries = append(entries, parseLeaf(name, description, block)...)
			continue
		}
		entries = append(entries, parseGroup(root, block)...)
	}
	return entries
}

func splitBlocks(lines []string) [][]string {
	var blocks [][]string
	var current []string
	for _, l := range lines {
		if separator.MatchString(strings.TrimSpace(l)) {
			blocks = append(blocks, current)
			current = nil
			continue
		}
		current = append(current, l)
	}
	return append(blocks, current)
}

// blockHeader reads "give armor: Give armor to players…" on the first line.
func blockHeader(root string, block []string) (name, description string, ok bool) {
	for _, l := range block {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		m := leafHeader.FindStringSubmatch(trimmed)
		if m == nil {
			return "", "", false
		}
		name = strings.ToLower(m[1])
		if name != root && !strings.HasPrefix(name, root+" ") {
			return "", "", false
		}
		return name, strings.TrimSpace(m[2]), true
	}
	return "", "", false
}

func parseGroup(root string, lines []string) []Entry {
	var entries []Entry
	header := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, " ")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
		case trimmed == "/"+root:
			header = true
		case header && strings.HasPrefix(line, " "):
			entries = append(entries, groupEntry(root, trimmed))
		}
	}
	return entries
}

// groupEntry parses "  add <player>" (subcommand) or "   <player>" (variant
// of the root itself).
func groupEntry(root, trimmed string) Entry {
	command, usage := root, trimmed
	if !strings.HasPrefix(trimmed, "<") && !strings.HasPrefix(trimmed, "[") {
		name, rest, _ := strings.Cut(trimmed, " ")
		command, usage = root+" "+name, rest
	}
	return newEntry(command, argsFromUsage(simplify(usage)), "")
}

type argType struct {
	description string
	values      []string
}

// parseLeaf reads one documented syntax: usage line and argument sections.
func parseLeaf(root, description string, lines []string) []Entry {
	var (
		usages     []string
		positional = map[string]Arg{}
		flags      []Arg
		types      = map[string]*argType{}
		section    string
		lastType   *argType
	)
	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		switch {
		case trimmed == "":
			continue
		case strings.HasSuffix(trimmed, "Arguments:") || trimmed == "Argument Types:":
			section = strings.TrimSuffix(trimmed, ":")
			continue
		case section == "" && (trimmed == "/"+root || strings.HasPrefix(trimmed, "/"+root+" ")):
			usages = append(usages, simplify(strings.TrimPrefix(trimmed, "/"+root)))
			continue
		case section == "":
			continue // header line
		}

		switch section {
		case "Required Arguments":
			if m := requiredLine.FindStringSubmatch(trimmed); m != nil {
				positional[m[1]] = Arg{Name: m[1], Required: true, Type: m[2], Description: m[3]}
			}
		case "Default-Value Arguments":
			if m := defaultLine.FindStringSubmatch(trimmed); m != nil {
				flags = append(flags, Arg{Name: m[1], Flag: true, TakesValue: true, Type: m[2], Default: m[3], Description: m[4]})
			}
		case "Argument Types":
			if m := examplesLine.FindStringSubmatch(trimmed); m != nil && lastType != nil {
				lastType.values = enumValues(lastType.description, m[1])
			} else if m := typeLine.FindStringSubmatch(trimmed); m != nil {
				lastType = &argType{description: strings.TrimSpace(m[2])}
				types[strings.TrimSpace(m[1])] = lastType
			}
		default: // "Optional Arguments" and any other flag section
			if m := optionalLine.FindStringSubmatch(trimmed); m != nil {
				typ := strings.Trim(strings.TrimSpace(m[2]), "()")
				flags = append(flags, Arg{Name: m[1], Flag: true, TakesValue: typ != "", Type: typ, Description: m[3]})
			}
		}
	}

	if len(usages) == 0 {
		usages = []string{""}
	}
	entries := make([]Entry, 0, len(usages))
	for _, usage := range usages {
		command := root
		// "/give armor <search>": leading plain words are subcommands.
		for usage != "" && !strings.ContainsAny(usage[:1], "<[") {
			word, rest, _ := strings.Cut(usage, " ")
			command, usage = command+" "+word, strings.TrimSpace(rest)
		}
		var args []Arg
		for _, a := range argsFromUsage(usage) {
			if detailed, ok := positional[a.Name]; ok && !a.Flag {
				a = detailed
			}
			args = append(args, a)
		}
		for _, f := range flags {
			if !slices.ContainsFunc(args, func(a Arg) bool { return a.Name == f.Name }) {
				args = append(args, f)
			}
		}
		for i := range args {
			if t, ok := types[args[i].Type]; ok {
				args[i].Values = t.values
			}
		}
		entries = append(entries, newEntry(command, args, description))
	}
	return entries
}

// enumValues returns the accepted values of a type when all its examples
// are plain words quoted in its description ("A game mode (adventure,
// creative, or their abbreviations: a, c)"), nil for free-form types such
// as numbers, text or player names.
func enumValues(description, examples string) []string {
	var listed []string
	for _, m := range quoted.FindAllStringSubmatch(examples, -1) {
		listed = append(listed, m[1])
	}
	if len(listed) < 2 {
		return nil
	}
	words := map[string]bool{}
	for _, w := range wordPattern.FindAllString(description, -1) {
		words[strings.ToLower(w)] = true
	}
	for _, v := range listed {
		if !identifier.MatchString(v) || !words[strings.ToLower(v)] {
			return nil
		}
	}
	return listed
}

// argsFromUsage reads "<x> [--pitch=?] [--force]" into arguments.
func argsFromUsage(usage string) []Arg {
	var args []Arg
	for _, tok := range usageToken.FindAllString(usage, -1) {
		inner := tok[1 : len(tok)-1]
		if strings.HasPrefix(tok, "<") {
			args = append(args, Arg{Name: inner, Required: true})
			continue
		}
		name, value, hasValue := strings.Cut(inner, "=")
		if strings.HasPrefix(name, "--") {
			args = append(args, Arg{Name: name, Flag: true, TakesValue: hasValue && value != ""})
		} else {
			args = append(args, Arg{Name: name})
		}
	}
	return args
}

// newEntry builds an entry and its display usage from the arguments.
func newEntry(command string, args []Arg, description string) Entry {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case a.Flag && a.TakesValue:
			parts = append(parts, "["+a.Name+" <"+valueLabel(a)+">]")
		case a.Flag:
			parts = append(parts, "["+a.Name+"]")
		case a.Required:
			parts = append(parts, "<"+a.Name+">")
		default:
			parts = append(parts, "["+a.Name+"]")
		}
	}
	if args == nil {
		args = []Arg{}
	}
	return Entry{Command: command, Usage: strings.Join(parts, " "), Description: description, Args: args}
}

func valueLabel(a Arg) string {
	if a.Type != "" {
		return strings.ToLower(a.Type)
	}
	return "valeur"
}

// simplify drops argument types: "<player:Player UUID>" becomes "<player>".
func simplify(usage string) string {
	usage = typedRequired.ReplaceAllString(usage, "<$1>")
	usage = typedOptional.ReplaceAllString(usage, "[$1]")
	return strings.TrimSpace(spaces.ReplaceAllString(usage, " "))
}

// Merge completes the parsed entries with paths that /help did not
// describe, and removes duplicates. Usages starting with a known
// subcommand ("give" + "armor <search>") are attached to that subcommand.
func Merge(entries []Entry, paths []string) []Entry {
	known := make(map[string]bool, len(paths))
	for _, p := range paths {
		known[p] = true
	}
	seen := map[string]bool{}
	described := map[string]bool{}
	out := make([]Entry, 0, len(entries)+len(paths))
	for _, e := range entries {
		key := e.Command + "\x00" + e.Usage
		if !seen[key] {
			seen[key] = true
			described[e.Command] = true
			out = append(out, e)
		}
	}
	for _, p := range paths {
		if !described[p] {
			described[p] = true
			out = append(out, newEntry(p, nil, ""))
		}
	}
	slices.SortStableFunc(out, func(a, b Entry) int { return strings.Compare(a.Command, b.Command) })
	return out
}
