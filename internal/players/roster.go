package players

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Files and identifiers used by the Hytale server (0.7+): the whitelist is
// the hytale.server.join permission, enforced when RequireJoinPermission is
// set in config.json; operators belong to the hytale:Admin group.
const (
	permissionsFile    = "permissions.json"
	bansFile           = "bans.json"
	configFile         = "config.json"
	joinPermission     = "hytale.server.join"
	operatorGroup      = "hytale:Admin"
	requireJoinSetting = "RequireJoinPermission"
	maxDataFileBytes   = 8 << 20
)

// PlayerRef identifies a player; Name is empty when it cannot be resolved.
type PlayerRef struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// Ban is an entry of the ban list.
type Ban struct {
	Player    PlayerRef  `json:"player"`
	By        *PlayerRef `json:"by,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	ExpiresOn *time.Time `json:"expiresOn,omitempty"`
}

// Whitelist is the join restriction state.
type Whitelist struct {
	Enabled bool        `json:"enabled"`
	Players []PlayerRef `json:"players"`
}

// Roster gathers the access lists of a save.
type Roster struct {
	Whitelist Whitelist   `json:"whitelist"`
	Bans      []Ban       `json:"bans"`
	Operators []PlayerRef `json:"operators"`
}

type permissionsDoc struct {
	Users map[string]struct {
		Groups      []string `json:"groups"`
		Permissions []string `json:"permissions"`
	} `json:"users"`
}

// ReadRoster parses the server files of saveDir. Missing files yield empty
// lists; resolve turns UUIDs into player references.
func ReadRoster(saveDir string, resolve func(uuid string) PlayerRef) (Roster, error) {
	roster := Roster{Whitelist: Whitelist{Players: []PlayerRef{}}, Bans: []Ban{}, Operators: []PlayerRef{}}

	var perms permissionsDoc
	if err := readJSONFile(filepath.Join(saveDir, permissionsFile), &perms); err != nil {
		return roster, err
	}
	for uuid, user := range perms.Users {
		ref := resolve(uuid)
		if slices.Contains(user.Groups, operatorGroup) {
			roster.Operators = append(roster.Operators, ref)
		}
		if slices.Contains(user.Permissions, joinPermission) {
			roster.Whitelist.Players = append(roster.Whitelist.Players, ref)
		}
	}

	var config map[string]json.RawMessage
	if err := readJSONFile(filepath.Join(saveDir, configFile), &config); err != nil {
		return roster, err
	}
	_ = json.Unmarshal(config[requireJoinSetting], &roster.Whitelist.Enabled)

	bans, err := readBans(filepath.Join(saveDir, bansFile), resolve)
	if err != nil {
		return roster, err
	}
	roster.Bans = bans

	sortRefs(roster.Operators)
	sortRefs(roster.Whitelist.Players)
	return roster, nil
}

// rawBan accepts both the versioned (PascalCase) and legacy (camelCase)
// formats written by the server.
type rawBan struct {
	Target    string          `json:"Target"`
	By        string          `json:"By"`
	Reason    string          `json:"Reason"`
	Timestamp json.RawMessage `json:"Timestamp"`
	ExpiresOn json.RawMessage `json:"ExpiresOn"`

	LegacyTarget    string          `json:"target"`
	LegacyBy        string          `json:"by"`
	LegacyReason    string          `json:"reason"`
	LegacyTimestamp json.RawMessage `json:"timestamp"`
	LegacyExpiresOn json.RawMessage `json:"expiresOn"`
}

func readBans(path string, resolve func(string) PlayerRef) ([]Ban, error) {
	var raw json.RawMessage
	if err := readJSONFile(path, &raw); err != nil || len(raw) == 0 {
		return []Ban{}, err
	}
	var entries []rawBan
	var versioned struct {
		Bans []rawBan `json:"Bans"`
	}
	if json.Unmarshal(raw, &versioned) == nil && versioned.Bans != nil {
		entries = versioned.Bans
	} else if err := json.Unmarshal(raw, &entries); err != nil {
		return []Ban{}, nil // unknown format: show nothing rather than fail
	}

	bans := make([]Ban, 0, len(entries))
	for _, e := range entries {
		target := firstNonEmpty(e.Target, e.LegacyTarget)
		if target == "" {
			continue
		}
		ban := Ban{
			Player:    resolve(target),
			Reason:    firstNonEmpty(e.Reason, e.LegacyReason),
			Timestamp: parseInstant(firstRaw(e.Timestamp, e.LegacyTimestamp)),
			ExpiresOn: parseInstant(firstRaw(e.ExpiresOn, e.LegacyExpiresOn)),
		}
		if by := firstNonEmpty(e.By, e.LegacyBy); by != "" {
			ref := resolve(by)
			ban.By = &ref
		}
		bans = append(bans, ban)
	}
	return bans, nil
}

// parseInstant reads an ISO-8601 string or an epoch (seconds or millis).
func parseInstant(raw json.RawMessage) *time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return &t
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return epoch(n)
		}
		return nil
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return epoch(n)
	}
	return nil
}

func epoch(n int64) *time.Time {
	var t time.Time
	if n > 1e12 {
		t = time.UnixMilli(n).UTC()
	} else {
		t = time.Unix(n, 0).UTC()
	}
	return &t
}

func readJSONFile(path string, v any) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() > maxDataFileBytes {
		return fmt.Errorf("%s is too large", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s is unreadable", filepath.Base(path))
	}
	return nil
}

func sortRefs(refs []PlayerRef) {
	slices.SortFunc(refs, func(a, b PlayerRef) int {
		return strings.Compare(strings.ToLower(a.Name+a.UUID), strings.ToLower(b.Name+b.UUID))
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstRaw(values ...json.RawMessage) json.RawMessage {
	for _, v := range values {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}
