package players

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"hytale-server-manager/internal/fsutil"
)

// The server reads these files at startup and rewrites them while running,
// so they are only edited directly while it is stopped.

// ErrNotInList is returned when the player is not in the edited list.
var ErrNotInList = errors.New("this player is not in the list")

// RemoveBan deletes the bans targeting uuid from bans.json.
func RemoveBan(saveDir, uuid string) error {
	path := filepath.Join(saveDir, bansFile)
	return editJSON(path, func(doc any) (any, bool) {
		switch v := doc.(type) {
		case map[string]any: // {"Version": 1, "Bans": [...]}
			list, _ := v["Bans"].([]any)
			kept, removed := withoutTarget(list, uuid)
			v["Bans"] = kept
			return v, removed
		case []any: // legacy array
			return withoutTarget(v, uuid)
		}
		return doc, false
	})
}

// RemoveOperator removes uuid from the operator group.
func RemoveOperator(saveDir, uuid string) error {
	return editUser(saveDir, uuid, "groups", operatorGroup)
}

// RemoveFromWhitelist revokes the join permission of uuid.
func RemoveFromWhitelist(saveDir, uuid string) error {
	return editUser(saveDir, uuid, "permissions", joinPermission)
}

// SetWhitelistEnabled toggles RequireJoinPermission in config.json.
func SetWhitelistEnabled(saveDir string, enabled bool) error {
	path := filepath.Join(saveDir, configFile)
	return editJSON(path, func(doc any) (any, bool) {
		cfg, ok := doc.(map[string]any)
		if !ok {
			cfg = map[string]any{}
		}
		cfg[requireJoinSetting] = enabled
		return cfg, true
	})
}

func editUser(saveDir, uuid, field, value string) error {
	path := filepath.Join(saveDir, permissionsFile)
	return editJSON(path, func(doc any) (any, bool) {
		root, _ := doc.(map[string]any)
		users, _ := root["users"].(map[string]any)
		for key, raw := range users {
			if !strings.EqualFold(key, uuid) {
				continue
			}
			user, _ := raw.(map[string]any)
			values, _ := user[field].([]any)
			kept := make([]any, 0, len(values))
			for _, v := range values {
				if v != value {
					kept = append(kept, v)
				}
			}
			if len(kept) == len(values) {
				return doc, false
			}
			user[field] = kept
			return doc, true
		}
		return doc, false
	})
}

func withoutTarget(list []any, uuid string) ([]any, bool) {
	kept := make([]any, 0, len(list))
	for _, entry := range list {
		e, _ := entry.(map[string]any)
		target, _ := e["Target"].(string)
		if target == "" {
			target, _ = e["target"].(string)
		}
		if !strings.EqualFold(target, uuid) {
			kept = append(kept, entry)
		}
	}
	return kept, len(kept) != len(list)
}

// editJSON applies mutate to a JSON file, preserving numbers exactly.
func editJSON(path string, mutate func(doc any) (any, bool)) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		data = []byte("{}")
	} else if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("%s is unreadable", filepath.Base(path))
	}
	doc, changed := mutate(doc)
	if !changed {
		return ErrNotInList
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, out, 0o640)
}
