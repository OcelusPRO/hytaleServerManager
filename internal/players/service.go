package players

import (
	"errors"
	"time"
)

// Service errors.
var (
	ErrServerStopped = errors.New("the server must be running for this action")
	ErrNoActiveSave  = errors.New("no active world")
	ErrInvalidUUID   = errors.New("invalid player identifier")
	ErrUnknownList   = errors.New("unknown list")
)

// List identifies an access list a player can be removed from.
type List string

// Access lists.
const (
	ListBans      List = "bans"
	ListOperators List = "operators"
	ListWhitelist List = "whitelist"
)

// console is the running server, as seen by the player service.
type console interface {
	UsesSave(id string) bool
	Send(command string) error
}

// ActiveSave returns the id and directory of the active save.
type ActiveSave func() (id, dir string, err error)

// Service manages the access lists of the active save. While the server
// runs on that save, changes go through console commands (so that the
// server's in-memory state stays authoritative); otherwise the files are
// edited directly.
type Service struct {
	console console
	names   *NameDirectory
	active  ActiveSave

	registry *Registry // optional
}

// NewService builds the player service.
func NewService(console console, names *NameDirectory, active ActiveSave) *Service {
	return &Service{console: console, names: names, active: active}
}

// WithRegistry makes the service keep the ban history of r up to date.
func (s *Service) WithRegistry(r *Registry) *Service {
	s.registry = r
	return s
}

// SyncBans records ban/unban events found in the active save's bans.json.
func (s *Service) SyncBans() error {
	if s.registry == nil {
		return nil
	}
	_, dir, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	roster, err := ReadRoster(dir, s.names.Resolver(dir))
	if err != nil {
		return err
	}
	return s.registry.SyncBans(roster.Bans, time.Now().UTC())
}

// Roster returns the access lists of the active save.
func (s *Service) Roster() (Roster, error) {
	_, dir, err := s.active()
	if err != nil {
		return Roster{}, ErrNoActiveSave
	}
	roster, err := ReadRoster(dir, s.names.Resolver(dir))
	if err == nil && s.registry != nil {
		_ = s.registry.SyncBans(roster.Bans, time.Now().UTC())
	}
	return roster, err
}

// Live reports whether the server currently runs the active save.
func (s *Service) Live() bool {
	id, _, err := s.active()
	return err == nil && s.console.UsesSave(id)
}

// Act sends a moderation command; it requires the server to be running.
func (s *Service) Act(action Action, player, reason string) error {
	command, err := BuildCommand(action, player, reason)
	if err != nil {
		return err
	}
	if !s.Live() {
		return ErrServerStopped
	}
	return s.console.Send(command)
}

// Remove takes the player uuid out of list.
func (s *Service) Remove(list List, uuid string) error {
	if !ValidUUID(uuid) {
		return ErrInvalidUUID
	}
	_, dir, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	if s.Live() {
		action, ok := map[List]Action{ListBans: ActionUnban, ListOperators: ActionDeop, ListWhitelist: ActionWhitelistRemove}[list]
		if !ok {
			return ErrUnknownList
		}
		target := uuid
		if name := s.names.Resolver(dir)(uuid).Name; name != "" {
			target = name
		}
		return s.Act(action, target, "")
	}
	switch list {
	case ListBans:
		return RemoveBan(dir, uuid)
	case ListOperators:
		return RemoveOperator(dir, uuid)
	case ListWhitelist:
		return RemoveFromWhitelist(dir, uuid)
	}
	return ErrUnknownList
}

// SetWhitelist enables or disables the whitelist.
func (s *Service) SetWhitelist(enabled bool) error {
	_, dir, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	if s.Live() {
		return s.console.Send(WhitelistCommand(enabled))
	}
	return SetWhitelistEnabled(dir, enabled)
}
