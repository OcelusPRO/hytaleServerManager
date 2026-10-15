// Package serverconfig exposes the JSON configuration files of a world
// (server, worlds and mods) for structured editing.
package serverconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"hytale-server-manager/internal/fsutil"
)

// Errors.
var (
	ErrUnknownFile = errors.New("configuration file not found")
	ErrUnreadable  = errors.New("this configuration file is not a valid JSON object")
	ErrBadChange   = errors.New("invalid change")
)

const (
	maxFileBytes = 1 << 20
	maxModDepth  = 4
)

// Kind classifies a configuration file.
type Kind string

const (
	KindServer Kind = "server"
	KindWorld  Kind = "world"
	KindMod    Kind = "mod"
)

// File describes one editable configuration file.
type File struct {
	// ID is the slash-separated path relative to the save directory.
	ID    string `json:"id"`
	Kind  Kind   `json:"kind"`
	Label string `json:"label"`
	Group string `json:"group,omitempty"`
}

// Change sets the value found at Path.
type Change struct {
	Path  []string        `json:"path"`
	Value json.RawMessage `json:"value"`
}

// List returns the configuration files present in saveDir.
func List(saveDir string) ([]File, error) {
	var files []File
	if isFile(filepath.Join(saveDir, "config.json")) {
		files = append(files, File{ID: "config.json", Kind: KindServer, Label: "Serveur"})
	}

	worlds, err := os.ReadDir(filepath.Join(saveDir, "universe", "worlds"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range worlds {
		id := "universe/worlds/" + e.Name() + "/config.json"
		if e.IsDir() && isFile(filepath.Join(saveDir, filepath.FromSlash(id))) {
			files = append(files, File{ID: id, Kind: KindWorld, Label: e.Name()})
		}
	}

	modsDir := filepath.Join(saveDir, "mods")
	err = filepath.WalkDir(modsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(modsDir, path)
		if d.IsDir() {
			if strings.Count(rel, string(filepath.Separator)) >= maxModDepth || strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(d.Name()), ".json") {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		group := ""
		if len(parts) > 1 {
			group = strings.Join(parts[:len(parts)-1], "/")
		}
		files = append(files, File{ID: "mods/" + filepath.ToSlash(rel), Kind: KindMod, Label: d.Name(), Group: group})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(files, func(a, b File) int {
		if a.Kind != b.Kind {
			return strings.Compare(string(a.Kind), string(b.Kind))
		}
		return strings.Compare(strings.ToLower(a.ID), strings.ToLower(b.ID))
	})
	return files, nil
}

// Read returns the raw JSON object of a listed file.
func Read(saveDir, id string) (json.RawMessage, error) {
	path, err := resolve(saveDir, id)
	if err != nil {
		return nil, err
	}
	doc, err := load(path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// Apply sets the given values and writes the file atomically. Untouched
// values (including big integers) are preserved exactly.
func Apply(saveDir, id string, changes []Change) error {
	path, err := resolve(saveDir, id)
	if err != nil {
		return err
	}
	doc, err := load(path)
	if err != nil {
		return err
	}
	for _, c := range changes {
		var value any
		dec := json.NewDecoder(bytes.NewReader(c.Value))
		dec.UseNumber()
		if len(c.Path) == 0 || dec.Decode(&value) != nil {
			return ErrBadChange
		}
		if err := set(doc, c.Path, value); err != nil {
			return err
		}
	}
	return fsutil.WriteJSONAtomic(path, doc, 0o640)
}

func set(obj map[string]any, path []string, value any) error {
	for _, key := range path[:len(path)-1] {
		next, ok := obj[key].(map[string]any)
		if !ok {
			if _, exists := obj[key]; exists {
				return ErrBadChange
			}
			next = map[string]any{}
			obj[key] = next
		}
		obj = next
	}
	obj[path[len(path)-1]] = value
	return nil
}

func load(path string) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxFileBytes {
		return nil, ErrUnreadable
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, ErrUnreadable
	}
	return doc, nil
}

// resolve maps id to a path, only accepting files that List would return.
func resolve(saveDir, id string) (string, error) {
	files, err := List(saveDir)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(files, func(f File) bool { return f.ID == id }) {
		return "", ErrUnknownFile
	}
	return filepath.Join(saveDir, filepath.FromSlash(id)), nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
