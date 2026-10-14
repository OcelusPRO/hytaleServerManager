import { describe, expect, it } from "vitest";
import { applySuggestion, complete, quote, tokenize, type CommandEntry } from "./commandCompletion";

// Shapes produced by the server catalog for real Hytale commands.
const eventtitle: CommandEntry = {
  command: "eventtitle",
  usage: "[--title <text>] [--secondary <text>] [--style <event title style>]",
  description: "Show an event title to all players on the server (for testing purposes)",
  args: [
    { name: "--title", flag: true, takesValue: true, type: "Text", description: "The primary title text" },
    { name: "--secondary", flag: true, takesValue: true, type: "Text", default: "Event", description: "The secondary title text" },
    {
      name: "--style",
      flag: true,
      takesValue: true,
      type: "Event Title Style",
      default: "Default Style",
      description: "The event title style",
      values: ["default", "major", "goblinbreach", "voideviction"],
    },
  ],
};

const ban: CommandEntry = {
  command: "ban",
  usage: "<player>",
  description: "Ban a player",
  args: [{ name: "player", required: true, type: "Player UUID/Username (with lookup)" }],
};

const tp: CommandEntry[] = [
  { command: "tp", usage: "<player> <targetPlayer>", args: [{ name: "player", required: true }, { name: "targetPlayer", required: true }] },
  {
    command: "tp",
    usage: "<x> <y> <z> [--pitch <valeur>]",
    args: [{ name: "x", required: true }, { name: "y", required: true }, { name: "z", required: true }, { name: "--pitch", flag: true, takesValue: true }],
  },
  { command: "tp all", usage: "<x> <y> <z>", args: [{ name: "x", required: true }, { name: "y", required: true }, { name: "z", required: true }] },
];

const entries = [eventtitle, ban, ...tp];
const labels = (input: string, players: string[] = []) => complete(input, entries, players).suggestions.map((s) => s.label);

describe("tokenize", () => {
  it("keeps quoted text as one token", () => {
    expect(tokenize('eventtitle --title "Restart en cours" --st')).toEqual({
      done: ["eventtitle", "--title", "Restart en cours"],
      current: "--st",
      prefix: 'eventtitle --title "Restart en cours" ',
      inQuotes: false,
    });
  });

  it("detects an open quote", () => {
    expect(tokenize('eventtitle --title "Restart en').inQuotes).toBe(true);
  });

  it("keeps empty quotes as an argument", () => {
    expect(tokenize('eventtitle --title "" --secondary "" --style ').done).toEqual(["eventtitle", "--title", "", "--secondary", "", "--style"]);
  });

  it("has no escape sequence, like the server", () => {
    // Checked on a Hytale 0.7 server: \" does not escape, every quote toggles.
    expect(tokenize('say "il a dit \\"salut\\"" ').done).not.toContain('il a dit "salut"');
  });
});

describe("quote", () => {
  it("quotes only when needed", () => {
    expect(quote("major")).toBe("major");
    expect(quote("Default Style")).toBe('"Default Style"');
    expect(quote("Redémarrage en cours")).toBe('"Redémarrage en cours"');
    expect(quote("")).toBe('""');
  });
});

describe("unwritable values", () => {
  it("never suggests a value containing a quote", () => {
    const entry: CommandEntry = { command: "warp", usage: "", args: [{ name: "name", required: true, values: ['Le "Spawn"', "Base"] }] };
    expect(complete("/warp ", [entry], []).suggestions.map((s) => s.label)).toEqual(["Base"]);
  });
});

describe("complete", () => {
  it("suggests commands while typing the name", () => {
    expect(labels("/ev")).toEqual(["eventtitle"]);
  });

  it("offers only the real flags of eventtitle", () => {
    expect(labels("/eventtitle ")).toEqual(["--title", "--secondary", "--style"]);
  });

  it("does not offer a flag twice", () => {
    expect(labels('/eventtitle --title "Restart" ')).toEqual(["--secondary", "--style"]);
  });

  it("offers the values of an enumerated flag", () => {
    expect(labels("/eventtitle --style ")).toEqual(["default", "major", "goblinbreach", "voideviction"]);
    expect(labels("/eventtitle --style go")).toEqual(["goblinbreach"]);
    const accepted = complete("/eventtitle --style go", entries, []).suggestions[0]!.value;
    expect(accepted).toBe("/eventtitle --style goblinbreach ");
  });

  it("describes the flag being typed and stays quiet inside quotes", () => {
    const result = complete('/eventtitle --title "Restart en', entries, []);
    expect(result.suggestions).toEqual([]);
    expect(result.hint?.current?.name).toBe("--title");
    expect(result.hint?.inQuotes).toBe(true);
  });

  it("handles empty quoted values before an enumerated flag (reported case)", () => {
    const r = complete('/eventtitle --title "" --secondary "" --style ', entries, []);
    expect(r.hint?.current?.name).toBe("--style");
    expect(r.suggestions.map((s) => s.label)).toEqual(["default", "major", "goblinbreach", "voideviction"]);
  });

  it("does not mistake a flag for the value of the previous one", () => {
    const r = complete('/eventtitle --title "Deux mots" --secondary ', entries, []);
    expect(r.hint?.current?.name).toBe("--secondary");
  });

  it("completes values written with --flag=value", () => {
    const s = complete("/eventtitle --style=go", entries, []).suggestions;
    expect(s.map((x) => x.value)).toEqual(["/eventtitle --style=goblinbreach "]);
  });

  it("completes an enumerated value typed inside quotes", () => {
    const s = complete('/eventtitle --style "ma', entries, []).suggestions;
    expect(s.map((x) => x.value)).toEqual(["/eventtitle --style major "]);
  });

  it("helps writing free text with quotes and offers the default", () => {
    const s = complete("/eventtitle --secondary ", entries, []).suggestions;
    const snippet = s.find((x) => x.kind === "snippet")!;
    expect(snippet.value).toBe('/eventtitle --secondary ""');
    expect(snippet.caret).toBe('/eventtitle --secondary "'.length);
    expect(s.find((x) => x.label === "Event")?.value).toBe("/eventtitle --secondary Event ");
  });

  it("quotes inserted values containing spaces", () => {
    const withSpaces: CommandEntry = { command: "warp", usage: "", args: [{ name: "name", required: true, values: ["Spawn Nord", "Base"] }] };
    const s = complete("/warp sp", [withSpaces], []).suggestions;
    expect(s.map((x) => x.value)).toEqual(['/warp "Spawn Nord" ']);
  });

  it("suggests online players for player arguments", () => {
    expect(labels("/ban ", ["Ocelus", "Lyra"])).toEqual(["Ocelus", "Lyra"]);
    expect(labels("/ban oc", ["Ocelus", "Lyra"])).toEqual(["Ocelus"]);
  });

  it("keeps only the syntaxes compatible with what was typed", () => {
    const hint = complete("/tp 1 2 3 ", entries, []).hint!;
    expect(hint.syntaxes).toHaveLength(1);
    expect(hint.syntaxes[0]!.map((a) => a.name)).toContain("--pitch");
  });

  it("prefers subcommands over the parent syntax", () => {
    const result = complete("/tp all 10 ", entries, []);
    expect(result.hint?.command).toBe("tp all");
    expect(result.hint?.current?.name).toBe("y");
  });
});

describe("caret-aware completion", () => {
  /** Completes `value` with the caret at the "|" marker, then accepts the first suggestion. */
  const at = (marked: string) => {
    const caret = marked.indexOf("|");
    const value = marked.replace("|", "");
    const result = complete(value.slice(0, caret), entries, []);
    return { value, caret, result };
  };

  it("stops offering quotes once the caret is between them", () => {
    const { result } = at('/eventtitle --title "|"');
    expect(result.suggestions).toEqual([]);
    expect(result.hint?.current?.name).toBe("--title");
  });

  it("reported flow: quotes, text, then the next flag", () => {
    const { result } = at('/eventtitle --title "test" --sec|');
    expect(result.hint?.current).toBeUndefined();
    expect(result.suggestions.map((s) => s.label)).toEqual(["--secondary"]);
  });

  it("completes the argument under the caret, not the end of the line", () => {
    // Editing the style while a title already follows.
    const { value, caret, result } = at('/eventtitle --style ma| --title "Redémarrage en cours"');
    expect(result.hint?.current?.name).toBe("--style");
    const edit = applySuggestion(value, caret, result.suggestions[0]!);
    expect(edit.value).toBe('/eventtitle --style major --title "Redémarrage en cours"');
    expect(edit.caret).toBe("/eventtitle --style major ".length);
  });

  it("replaces the rest of the word under the caret", () => {
    const { value, caret, result } = at("/eventtitle --sec|xyz");
    const edit = applySuggestion(value, caret, result.suggestions[0]!);
    expect(edit.value).toBe("/eventtitle --secondary ");
  });
});
