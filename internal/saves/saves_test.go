package saves

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hytale-server-manager/internal/archive"
)

func newStores(t *testing.T) (*Store, *ModStore, *BackupStore) {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"saves", "backups", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(filepath.Join(root, "saves"), filepath.Join(root, "tmp"))
	backups := NewBackupStore(filepath.Join(root, "backups"), filepath.Join(root, "tmp"), store, archive.Limits{})
	return store, NewModStore(store), backups
}

func TestSaveLifecycle(t *testing.T) {
	store, _, _ := newStores(t)
	save, err := store.Create("  Mon monde  ")
	if err != nil {
		t.Fatal(err)
	}
	if save.Name != "Mon monde" {
		t.Errorf("name not trimmed: %q", save.Name)
	}
	if _, err := store.Create("bad\x00name"); !errors.Is(err, ErrInvalidName) {
		t.Errorf("control characters accepted: %v", err)
	}
	if _, err := store.Dir("../../etc"); !errors.Is(err, ErrInvalidID) {
		t.Errorf("traversal id accepted: %v", err)
	}
	if _, err := store.Rename(save.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 || list[0].Name != "Renamed" {
		t.Fatalf("unexpected list %v, %v", list, err)
	}
	if err := store.Delete(save.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(save.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted save still present: %v", err)
	}
}

func TestModValidationAndToggle(t *testing.T) {
	store, mods, _ := newStores(t)
	save, _ := store.Create("w")
	jar := append([]byte("PK\x03\x04"), bytes.Repeat([]byte{1}, 100)...)

	if _, err := mods.Add(save.ID, "../evil.jar", bytes.NewReader(jar), 1<<20); !errors.Is(err, ErrInvalidModName) {
		t.Errorf("path in mod name accepted: %v", err)
	}
	if _, err := mods.Add(save.ID, "script.sh", bytes.NewReader(jar), 1<<20); !errors.Is(err, ErrInvalidModName) {
		t.Errorf("non-archive extension accepted: %v", err)
	}
	if _, err := mods.Add(save.ID, "fake.jar", strings.NewReader("not a zip"), 1<<20); !errors.Is(err, ErrNotAnArchive) {
		t.Errorf("non-zip content accepted: %v", err)
	}
	if _, err := mods.Add(save.ID, "big.jar", bytes.NewReader(jar), 10); !errors.Is(err, ErrModTooLarge) {
		t.Errorf("oversized mod accepted: %v", err)
	}
	if _, err := mods.Add(save.ID, "Cool Mod-1.0.jar", bytes.NewReader(jar), 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := mods.SetEnabled(save.ID, "Cool Mod-1.0.jar", false); err != nil {
		t.Fatal(err)
	}
	list, _ := mods.List(save.ID)
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("mod should be disabled: %v", list)
	}
	if err := mods.Remove(save.ID, "Cool Mod-1.0.jar"); err != nil {
		t.Fatal(err)
	}
}

func TestBackupAndRestore(t *testing.T) {
	store, _, backups := newStores(t)
	save, _ := store.Create("w")
	dir, _ := store.Dir(save.ID)
	world := filepath.Join(dir, "universe", "data.bin")
	os.MkdirAll(filepath.Dir(world), 0o755)
	os.WriteFile(world, []byte("v1"), 0o644)

	b, err := backups.Create(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(world, []byte("v2"), 0o644)
	if err := backups.Restore(save.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(world)
	if string(got) != "v1" {
		t.Errorf("restore produced %q", got)
	}
	if restored, err := store.Get(save.ID); err != nil || restored.Name != "w" {
		t.Errorf("metadata lost after restore: %v %v", restored, err)
	}
	if _, err := backups.Path(save.ID, "../../x"); !errors.Is(err, ErrInvalidBackupID) {
		t.Errorf("invalid backup id accepted: %v", err)
	}
}

func TestAutosaveRotation(t *testing.T) {
	store, _, backups := newStores(t)
	save, err := store.Create("Monde")
	if err != nil {
		t.Fatal(err)
	}
	manual, err := backups.Create(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	var last Backup
	for i := 0; i < 4; i++ {
		if last, err = backups.CreateAuto(save.ID, 2); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1100 * time.Millisecond) // IDs have a one-second resolution
	}
	list, err := backups.List(save.ID)
	if err != nil {
		t.Fatal(err)
	}
	autos, manuals := 0, 0
	for _, b := range list {
		if b.Auto {
			autos++
		} else {
			manuals++
		}
	}
	if autos != 2 || manuals != 1 {
		t.Fatalf("got %d autosaves and %d manual backups, want 2 and 1", autos, manuals)
	}
	if !last.Auto || list[0].ID != last.ID && list[1].ID != last.ID {
		t.Fatalf("newest autosave %s was not kept", last.ID)
	}
	if _, err := backups.Path(save.ID, manual.ID); err != nil {
		t.Fatalf("manual backup was removed: %v", err)
	}
}
