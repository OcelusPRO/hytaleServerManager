package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanStaleRemovesLeftovers(t *testing.T) {
	root := t.TempDir()
	l := New(root)
	if err := l.EnsureAll(); err != nil {
		t.Fatal(err)
	}
	touch := func(rel string) string {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
		return path
	}
	stale := []string{
		touch("manager/.metrics.json.tmp-3736039567"),
		touch("saves/abc/universe/worlds/default/.config.json.tmp-12"),
		touch("saves/abc/mods/.upload-998"),
		touch("backups/abc/.backup-4242"),
	}
	kept := []string{
		touch("manager/metrics.json"),
		touch("saves/abc/.hsm-save.json"),
		touch("saves/abc/mods/Shop.jar"),
		touch("saves/abc/universe/region.tmp-1"), // not one of ours: no leading dot
		touch("backups/abc/b1.zip"),
		touch("game/Server/HytaleServer.jar"),
	}
	touch("game.old/Assets.zip")
	touch("game-versions/0.7.0/Assets.zip")

	removed, err := l.CleanStale()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != len(stale)+2 {
		t.Errorf("removed %v", removed)
	}
	for _, path := range append(stale, filepath.Join(root, "game.old"), filepath.Join(root, "game-versions")) {
		if _, err := os.Lstat(path); err == nil {
			t.Errorf("%s left behind", path)
		}
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s removed", path)
		}
	}
}
