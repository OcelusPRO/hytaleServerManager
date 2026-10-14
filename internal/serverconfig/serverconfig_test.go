package serverconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestListAndApply(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.json", `{"ServerName":"A","Defaults":{"World":"default"},"Big":9007199254740993}`)
	write(t, dir, "universe/worlds/default/config.json", `{"Seed":1}`)
	write(t, dir, "mods/Group_Mod/Mod.json", `{"DebugMode":false}`)
	write(t, dir, "bans.json", `{}`)

	files, err := List(dir)
	if err != nil || len(files) != 3 {
		t.Fatalf("List = %v, %v", files, err)
	}

	err = Apply(dir, "config.json", []Change{
		{Path: []string{"ServerName"}, Value: json.RawMessage(`"B"`)},
		{Path: []string{"Defaults", "GameMode"}, Value: json.RawMessage(`"CREATIVE"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	for _, want := range []string{`"B"`, `"CREATIVE"`, `9007199254740993`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in %s", want, data)
		}
	}
}

func TestRejectsUnlistedFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.json", `{}`)
	write(t, dir, "bans.json", `{}`)
	for _, id := range []string{"bans.json", "../x.json", "mods/../bans.json"} {
		if _, err := Read(dir, id); !errors.Is(err, ErrUnknownFile) {
			t.Errorf("Read(%q) err = %v", id, err)
		}
	}
}
