package commandcatalog

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"hytale-server-manager/internal/gameserver"
)

// Outputs captured from a Hytale 0.7 server (ANSI colours stripped).
var helpOutputs = map[string][]string{
	"op": {"/op", "  self", "  add <player>", "  remove <player>"},
	"ban": {
		"ban: Ban a player ",
		"/ban  <player:Player UUID/Username (with lookup)>",
		"Required Arguments: ",
		`    player (Player UUID/Username (with lookup)) -> "The player to ban (username or UUID)"`,
		"Argument Types:",
		"    Player UUID/Username (with lookup): A player UUID or username. If not found locally, performs a remote lookup via the Profile Service.",
		"        Examples: '53cbd9ec-a450-4889-9d7a-0b8983aa61dd', 'john_doe', 'user123'.",
	},
	"eventtitle": {
		"eventtitle: Show an event title to all players on the server (for testing purposes) ",
		"/eventtitle ",
		"Optional Arguments: ",
		`    --title Text -> "The primary title text"`,
		"Default-Value Arguments: ",
		`    --secondary (Text:default=Event) -> "The secondary title text"`,
		`    --style (Event Title Style:default=Default Style) -> "The event title style"`,
		"Argument Types:",
		"    Event Title Style: default, major, goblinbreach, voideviction",
		"        Examples: 'default', 'major', 'goblinbreach', 'voideviction'.",
		"    Text: Words, text, numbers, letters",
		`        Examples: '"Hytale is really cool!"', '"Numbers work 2!"', '"If you can type it..."'.`,
	},
	// Real multi-block output: one block per syntax or subcommand.
	"give": {
		"give: Gives an item to a player ",
		"/give  <item:Item Asset>",
		"Required Arguments: ",
		`    item (Item Asset) -> "The item to give"`,
		"Optional Arguments: ",
		`    --durability Double -> "Item durability"`,
		"Default-Value Arguments: ",
		`    --quantity (Integer:default=1) -> "The quantity of items to give"`,
		"Argument Types:",
		"    Item Asset: A reference to an Asset of type Item",
		"        Examples: ''.",
		"    Integer: A whole number",
		"        Examples: '-27432', '-1', '0', '1', '56346'.",
		"---------------------------------",
		"give: Give an item to another player ",
		"/give  <player:Player> <item:Item Asset>",
		"Required Arguments: ",
		`    player (Player) -> "An online player"`,
		`    item (Item Asset) -> "The item to give"`,
		"Argument Types:",
		"    Player: The name of the player.",
		"        Examples: 'john_doe', 'user123'.",
		"---------------------------------",
		"give armor: Give armor to players based on a search string ",
		"/give armor  <search:Text>",
		"Required Arguments: ",
		`    search (Text) -> "The armor type to search for (e.g., "Iron", "Gold")"`,
		"Optional Arguments: ",
		`    --player Text -> "The target player (use '*' for all players)"`,
		"Flag Arguments: ",
		`    --set -> "Clear existing armor before adding new armor"`,
		"Argument Types:",
		`    Flag: An argument that represents "true" when specified on a command, and "false" by default`,
		"        Examples: 'None, just specify the argument for true'.",
	},
	"gamemode": {
		"gamemode: Change player game mode ",
		"/gamemode  <gamemode:GameMode>",
		"Required Arguments: ",
		`    gamemode (GameMode) -> "The game mode to set (adventure, creative, or their abbreviations: a, c)"`,
		"Argument Types:",
		"    GameMode: A game mode (adventure, creative, or their abbreviations: a, c)",
		"        Examples: 'adventure', 'creative', 'a', 'c'.",
		"---------------------------------",
		"gamemode: Change another player's game mode ",
		"/gamemode  <gamemode:GameMode> <player:Player>",
		"Required Arguments: ",
		`    gamemode (GameMode) -> "The game mode to set (adventure, creative, or their abbreviations: a, c)"`,
		`    player (Player) -> "An online player"`,
		"Argument Types:",
		"    GameMode: A game mode (adventure, creative, or their abbreviations: a, c)",
		"        Examples: 'adventure', 'creative', 'a', 'c'.",
		"    Player: The name of the player.",
		"        Examples: 'john_doe', 'user123'.",
	},
	"time": {
		"time: Get the world time ",
		"/time ",
		"Optional Arguments: ",
		`    --world World -> "The world to operate this command on"`,
		"Argument Types:",
		"    World: A world folder name",
		"        Examples: 'default'.",
		"---------------------------------",
		"time dawn: Sets the world time to dawn. ",
		"/time dawn ",
		"Optional Arguments: ",
		`    --world World -> "The world to operate this command on"`,
		"---------------------------------",
		" set",
		"     <time> [--world=?]",
		"    dawn [--world=?]",
	},
	"tp": {
		"/tp",
		"   <player> <targetPlayer>",
		"   <x> <y> <z> [--pitch=?] [--yaw=?] [--roll=?]",
		"  all <x> <y> <z> [--pitch=?] [--yaw=?] [--world=?] [--roll=?]",
		"  back [--count=?]",
	},
}

const dumpJSON = `{"modern":[
 {"name":"/op"},{"name":"/op self"},{"name":"/op add"},{"name":"/op remove"},
 {"name":"/ban"},{"name":"/eventtitle"},{"name":"/give"},{"name":"/give armor"},
 {"name":"/gamemode"},{"name":"/time"},{"name":"/time dawn"},
 {"name":"/tp"},{"name":"/tp all"},{"name":"/tp back"},{"name":"/tp home"}
]}`

func argNames(args []Arg) []string {
	names := make([]string, len(args))
	for i, a := range args {
		names[i] = a.Name
	}
	return names
}

func TestParseLeafWithArgumentSections(t *testing.T) {
	entries := ParseHelp("eventtitle", helpOutputs["eventtitle"])
	if len(entries) != 1 {
		t.Fatalf("eventtitle: want exactly one syntax, got %+v", entries)
	}
	e := entries[0]
	if e.Command != "eventtitle" || !strings.HasPrefix(e.Description, "Show an event title") {
		t.Errorf("header: %+v", e)
	}
	if got := argNames(e.Args); !slices.Equal(got, []string{"--title", "--secondary", "--style"}) {
		t.Fatalf("args: %v", got)
	}
	title, secondary, style := e.Args[0], e.Args[1], e.Args[2]
	if !title.Flag || !title.TakesValue || title.Type != "Text" || title.Description != "The primary title text" {
		t.Errorf("--title: %+v", title)
	}
	if secondary.Default != "Event" || !secondary.TakesValue {
		t.Errorf("--secondary: %+v", secondary)
	}
	if !slices.Equal(style.Values, []string{"default", "major", "goblinbreach", "voideviction"}) || style.Default != "Default Style" {
		t.Errorf("--style: %+v", style)
	}
	if title.Values != nil {
		t.Error("free text must not be offered as an enumeration")
	}
	if e.Usage != "[--title <text>] [--secondary <text>] [--style <event title style>]" {
		t.Errorf("usage: %q", e.Usage)
	}
}

func TestParseLeafRequiredArgument(t *testing.T) {
	e := ParseHelp("ban", helpOutputs["ban"])
	if len(e) != 1 || len(e[0].Args) != 1 {
		t.Fatalf("ban: %+v", e)
	}
	a := e[0].Args[0]
	if a.Name != "player" || !a.Required || a.Flag || a.Description != "The player to ban (username or UUID)" || a.Values != nil {
		t.Errorf("player arg: %+v", a)
	}
}

func TestParseMultiBlockHelp(t *testing.T) {
	give := ParseHelp("give", helpOutputs["give"])
	if len(give) != 3 {
		t.Fatalf("give: want 3 syntaxes, got %+v", give)
	}
	if got := argNames(give[0].Args); !slices.Equal(got, []string{"item", "--durability", "--quantity"}) {
		t.Errorf("give #1: %v", got)
	}
	if give[1].Description != "Give an item to another player" || !slices.Equal(argNames(give[1].Args), []string{"player", "item"}) {
		t.Errorf("give #2: %+v", give[1])
	}
	armor := give[2]
	if armor.Command != "give armor" || !slices.Equal(argNames(armor.Args), []string{"search", "--player", "--set"}) {
		t.Fatalf("give armor: %+v", armor)
	}
	if set := armor.Args[2]; !set.Flag || set.TakesValue {
		t.Errorf("--set is a boolean flag: %+v", set)
	}
	for _, a := range armor.Args {
		if a.Values != nil {
			t.Errorf("%s must not be an enumeration: %v", a.Name, a.Values)
		}
	}
}

func TestEnumDetectedFromDescription(t *testing.T) {
	gm := ParseHelp("gamemode", helpOutputs["gamemode"])
	if len(gm) != 2 {
		t.Fatalf("gamemode: %+v", gm)
	}
	if v := gm[0].Args[0].Values; !slices.Equal(v, []string{"adventure", "creative", "a", "c"}) {
		t.Errorf("GameMode values: %v", v)
	}
	if v := gm[1].Args[1].Values; v != nil {
		t.Errorf("player names are not an enumeration: %v", v)
	}
}

func TestSubcommandBlocksAndAliasListing(t *testing.T) {
	time := ParseHelp("time", helpOutputs["time"])
	commands := []string{}
	for _, e := range time {
		commands = append(commands, e.Command)
	}
	if !slices.Equal(commands, []string{"time", "time dawn"}) {
		t.Errorf("time: %v (the indented alias listing must be ignored)", commands)
	}
	if time[1].Args[0].Name != "--world" || time[1].Args[0].Values != nil {
		t.Errorf("--world: %+v", time[1].Args[0])
	}
}

func TestParseGroup(t *testing.T) {
	tp := ParseHelp("tp", helpOutputs["tp"])
	if len(tp) != 4 || tp[2].Command != "tp all" || tp[3].Command != "tp back" {
		t.Fatalf("tp: %+v", tp)
	}
	pitch := tp[1].Args[3]
	if pitch.Name != "--pitch" || !pitch.Flag || !pitch.TakesValue {
		t.Errorf("--pitch: %+v", pitch)
	}
	if got := argNames(tp[0].Args); !slices.Equal(got, []string{"player", "targetPlayer"}) {
		t.Errorf("tp variant: %v", got)
	}
}

func TestMergeAddsUndescribedCommands(t *testing.T) {
	paths, err := ParseDump([]byte(dumpJSON))
	if err != nil {
		t.Fatal(err)
	}
	entries := Merge(ParseHelp("tp", helpOutputs["tp"]), paths)
	if !slices.ContainsFunc(entries, func(e Entry) bool { return e.Command == "tp home" }) {
		t.Error("commands missing from /help must still be listed")
	}
}

// fakeServer answers commands on the console like the real server does.
type fakeServer struct {
	console *gameserver.Console
	saveDir string
}

func (f *fakeServer) SendSilent(command string) error {
	cmd := strings.TrimPrefix(command, "/")
	f.console.Append(gameserver.StreamStdout, "[2026/10/13 09:00:09   INFO]  [CommandManager] Console executed command: "+cmd)
	switch {
	case cmd == "commands dump":
		os.MkdirAll(filepath.Join(f.saveDir, "dumps"), 0o755)
		os.WriteFile(filepath.Join(f.saveDir, dumpFile), []byte(dumpJSON), 0o644)
		f.console.Append(gameserver.StreamStdout, "Dumped commands to /work/dumps/commands.dump.json")
	case strings.HasPrefix(cmd, "help "):
		for _, l := range helpOutputs[strings.TrimPrefix(cmd, "help ")] {
			f.console.Append(gameserver.StreamStdout, l)
		}
	}
	// Unrelated server activity must stay visible.
	f.console.Append(gameserver.StreamStdout, "[2026/10/13 09:00:10   INFO] [World] tick")
	return nil
}

func TestBuilderHidesHousekeepingOutput(t *testing.T) {
	console := gameserver.NewConsole(1000)
	server := &fakeServer{console: console, saveDir: t.TempDir()}
	entries, err := NewBuilder(server, console).Build(context.Background(), server.saveDir)
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]bool{}
	for _, e := range entries {
		commands[e.Command] = true
	}
	for _, want := range []string{"op add", "ban", "eventtitle", "give armor", "tp all", "tp home"} {
		if !commands[want] {
			t.Errorf("missing %q in %v", want, commands)
		}
	}
	for _, bogus := range []string{"eventtitle Text:", "eventtitle --style", "eventtitle Examples:"} {
		if commands[bogus] {
			t.Errorf("argument documentation parsed as a subcommand: %q", bogus)
		}
	}
	for _, l := range console.Since(0) {
		if !strings.Contains(l.Text, "[World] tick") {
			t.Errorf("housekeeping line leaked into the console: %q", l.Text)
		}
	}
}
