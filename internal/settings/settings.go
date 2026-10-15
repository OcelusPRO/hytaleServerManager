// Package settings stores the operator-editable server settings.
package settings

import (
	"errors"
	"fmt"
	"regexp"
	"sync"

	"hytale-server-manager/internal/fsutil"
)

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

// Authentication modes accepted by the server (--auth-mode).
const (
	AuthModeAuthenticated = "authenticated"
	AuthModeOffline       = "offline"
)

// Patchlines offered by the Hytale asset service.
const (
	PatchlineRelease    = "release"
	PatchlinePreRelease = "pre-release"
)

// Settings configures how the game server is launched.
type Settings struct {
	ActiveSaveID        string `json:"activeSaveId"`
	MinMemoryMB         int    `json:"minMemoryMb"`
	MaxMemoryMB         int    `json:"maxMemoryMb"`
	Port                int    `json:"port"`
	Patchline           string `json:"patchline"`
	AuthMode            string `json:"authMode"`
	UseAOTCache         bool   `json:"useAotCache"`
	DisableSentry       bool   `json:"disableSentry"`
	AutoStart           bool   `json:"autoStart"`
	AutoRestart         bool   `json:"autoRestart"`
	NativeBackups       bool   `json:"nativeBackups"`
	NativeBackupMinutes int    `json:"nativeBackupMinutes"`
	StopTimeoutSeconds  int    `json:"stopTimeoutSeconds"`
	BackupBeforeRestore bool   `json:"backupBeforeRestore"`
	AcceptEarlyPlugins  bool   `json:"acceptEarlyPlugins"`
	AutosaveEnabled     bool   `json:"autosaveEnabled"`
	AutosaveMinutes     int    `json:"autosaveMinutes"`
	AutosaveSlots       int    `json:"autosaveSlots"`
	// AutoUpdate installs pending game updates without operator action:
	// once the server has been empty for AutoUpdateIdleMinutes, or after
	// AutoUpdateForceHours with a warning countdown to the players.
	AutoUpdate            bool `json:"autoUpdate"`
	AutoUpdateIdleMinutes int  `json:"autoUpdateIdleMinutes"`
	AutoUpdateForceHours  int  `json:"autoUpdateForceHours"`
	// SleepEnabled stops the server once it has been empty for
	// SleepIdleMinutes; the manager then listens on the game port and starts
	// it again as soon as a player tries to connect.
	SleepEnabled     bool `json:"sleepEnabled"`
	SleepIdleMinutes int  `json:"sleepIdleMinutes"`
	// GameLanguage is the language of the announcements shown to players
	// in game (automatic updates).
	GameLanguage string `json:"gameLanguage"`
	// GameVersionPin freezes the game on one build ("" = follow the latest).
	// It is changed through /api/setup/version, which also applies it.
	GameVersionPin string `json:"gameVersionPin"`
}

// Defaults returns the settings used on first launch.
func Defaults() Settings {
	return Settings{
		MinMemoryMB:         1024,
		MaxMemoryMB:         4096,
		Port:                5520,
		Patchline:           PatchlineRelease,
		AuthMode:            AuthModeAuthenticated,
		UseAOTCache:         true,
		DisableSentry:       false,
		AutoStart:           false,
		AutoRestart:         true,
		NativeBackups:       false,
		NativeBackupMinutes: 30,
		StopTimeoutSeconds:  60,
		BackupBeforeRestore: true,
		AutosaveMinutes:     30,
		AutosaveSlots:       5,

		AutoUpdate:            true,
		AutoUpdateIdleMinutes: 10,
		AutoUpdateForceHours:  6,

		SleepEnabled:     false,
		SleepIdleMinutes: 10,
		GameLanguage:     "en",
	}
}

// Validate checks that every value is within a sane range.
func (s Settings) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	check(s.MinMemoryMB >= 256 && s.MinMemoryMB <= 262144, "invalid minimum memory")
	check(s.MaxMemoryMB >= 1024 && s.MaxMemoryMB <= 262144, "invalid maximum memory (at least 1024 MB)")
	check(s.MinMemoryMB <= s.MaxMemoryMB, "the minimum memory exceeds the maximum")
	check(s.Port >= 1 && s.Port <= 65535, "invalid port")
	check(s.Patchline == PatchlineRelease || s.Patchline == PatchlinePreRelease, "invalid patchline")
	check(s.AuthMode == AuthModeAuthenticated || s.AuthMode == AuthModeOffline, "invalid authentication mode")
	check(s.NativeBackupMinutes >= 5 && s.NativeBackupMinutes <= 1440, "invalid backup frequency (5-1440 min)")
	check(s.StopTimeoutSeconds >= 10 && s.StopTimeoutSeconds <= 600, "invalid stop timeout (10-600 s)")
	check(s.AutosaveMinutes >= 1 && s.AutosaveMinutes <= 1440, "invalid autosave frequency (1-1440 min)")
	check(s.AutosaveSlots >= 1 && s.AutosaveSlots <= 100, "invalid number of autosave slots (1-100)")
	check(s.AutoUpdateIdleMinutes >= 1 && s.AutoUpdateIdleMinutes <= 1440, "invalid update idle delay (1-1440 min)")
	check(s.AutoUpdateForceHours >= 1 && s.AutoUpdateForceHours <= 720, "invalid forced update delay (1-720 h)")
	check(s.SleepIdleMinutes >= 1 && s.SleepIdleMinutes <= 1440, "invalid sleep delay (1-1440 min)")
	check(s.GameLanguage == "en" || s.GameLanguage == "fr", "invalid game language")
	check(s.GameVersionPin == "" || versionPattern.MatchString(s.GameVersionPin), "invalid game version")
	return errors.Join(errs...)
}

// Store persists Settings as JSON.
type Store struct {
	path string

	mu      sync.RWMutex
	current Settings
}

// Open loads settings from path, using defaults for a fresh install.
func Open(path string) (*Store, error) {
	current := Defaults()
	if _, err := fsutil.ReadJSON(path, &current); err != nil {
		return nil, err
	}
	return &Store{path: path, current: current}, nil
}

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Update applies mutate, validates and persists the result.
func (s *Store) Update(mutate func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.current
	mutate(&next)
	if err := next.Validate(); err != nil {
		return s.current, err
	}
	if err := fsutil.WriteJSONAtomic(s.path, next, 0o600); err != nil {
		return s.current, err
	}
	s.current = next
	return next, nil
}
