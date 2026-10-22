package players

import (
	"errors"
	"strings"
	"time"
)

// Service errors.
var (
	ErrServerStopped = errors.New("the server must be running for this action")
	ErrNoActiveSave  = errors.New("no active world")
	ErrInvalidUUID   = errors.New("invalid player identifier")
	ErrUnknownList   = errors.New("unknown list")
	// ErrUnknownPlayer is returned for an addition while the server is
	// stopped: only players the manager has seen can be identified.
	ErrUnknownPlayer = errors.New("unknown player: start the server to add a player who never connected")
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

// Service manages the access lists. While the server runs on the active
// save, changes go through console commands (so that the server's
// in-memory state stays authoritative); otherwise the files are edited
// directly. With a registry, the lists are shared by every world (see
// SharedLists).
type Service struct {
	console console
	names   *NameDirectory
	active  ActiveSave

	registry *Registry    // optional
	shared   *SharedLists // optional, with the registry
	now      func() time.Time
}

// NewService builds the player service.
func NewService(console console, names *NameDirectory, active ActiveSave) *Service {
	return &Service{console: console, names: names, active: active, now: func() time.Time { return time.Now().UTC() }}
}

// WithRegistry makes the service keep the history of r up to date and
// share the access lists of every world through shared.
func (s *Service) WithRegistry(r *Registry, shared *SharedLists) *Service {
	s.registry, s.shared = r, shared
	return s
}

// Pending lists the players of the shared lists not yet applied to the
// active world (they are at its next boot).
type Pending struct {
	Bans      []string `json:"bans"`
	Operators []string `json:"operators"`
	Whitelist []string `json:"whitelist"`
}

// Sync takes the changes found in the active save's files into the shared
// lists.
func (s *Service) Sync() error {
	if s.shared == nil {
		return nil
	}
	id, dir, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	roster, err := ReadRoster(dir, s.names.Resolver(dir))
	if err != nil {
		return err
	}
	return s.shared.Sync(id, roster, s.now())
}

// Roster returns the access lists: the shared ones when available,
// otherwise those of the active save's files.
func (s *Service) Roster() (Roster, Pending, error) {
	id, dir, err := s.active()
	if err != nil {
		return Roster{}, Pending{}, ErrNoActiveSave
	}
	resolve := s.names.Resolver(dir)
	files, err := ReadRoster(dir, resolve)
	if err != nil || s.shared == nil {
		return files, Pending{}, err
	}
	if err := s.shared.Sync(id, files, s.now()); err != nil {
		return files, Pending{}, err
	}
	shared, err := s.shared.Get()
	if err != nil {
		return files, Pending{}, err
	}
	pending := pendingEntries(shared, files)
	return sharedRoster(shared, resolve), Pending{Bans: pending[ListBans], Operators: pending[ListOperators], Whitelist: pending[ListWhitelist]}, nil
}

// sharedRoster presents the shared lists like a world's.
func sharedRoster(shared Shared, resolve func(string) PlayerRef) Roster {
	ref := func(p PlayerRef) PlayerRef {
		if p.Name == "" {
			p.Name = resolve(p.UUID).Name
		}
		return p
	}
	r := Roster{Whitelist: Whitelist{Enabled: shared.WhitelistEnabled, Players: []PlayerRef{}}, Bans: []Ban{}, Operators: []PlayerRef{}}
	for _, e := range shared.Lists[ListWhitelist] {
		r.Whitelist.Players = append(r.Whitelist.Players, ref(e.Player))
	}
	for _, e := range shared.Lists[ListOperators] {
		r.Operators = append(r.Operators, ref(e.Player))
	}
	for _, e := range shared.Lists[ListBans] {
		b := Ban{Player: ref(e.Player), Reason: e.Reason}
		if !e.AddedAt.IsZero() {
			at := e.AddedAt
			b.Timestamp = &at
		}
		if !e.ExpiresOn.IsZero() {
			exp := e.ExpiresOn
			b.ExpiresOn = &exp
		}
		r.Bans = append(r.Bans, b)
	}
	return r
}

// Live reports whether the server currently runs the active save.
func (s *Service) Live() bool {
	id, _, err := s.active()
	return err == nil && s.console.UsesSave(id)
}

// Act applies a moderation action. With the server running, it is sent as
// a console command; otherwise additions go to the shared lists (applied
// at the next boot) and removals to the shared lists and the files. A kick
// needs the server running.
func (s *Service) Act(action Action, player, reason string) error {
	command, err := BuildCommand(action, player, reason)
	if err != nil {
		return err
	}
	if s.Live() {
		if err := s.console.Send(command); err != nil {
			return err
		}
		s.record(action, player, reason)
		return nil
	}
	list, adds, ok := listFor(action)
	if !ok || s.shared == nil {
		return ErrServerStopped
	}
	ref, err := s.identify(player)
	if err != nil {
		return err
	}
	if !adds {
		return s.Remove(list, ref.UUID)
	}
	id, _, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	return s.shared.Add(list, ref, strings.TrimSpace(reason), id, s.now())
}

// identify finds the player designated by a name or a uuid.
func (s *Service) identify(player string) (PlayerRef, error) {
	if ValidUUID(player) {
		ref := PlayerRef{UUID: strings.ToLower(player)}
		if s.registry != nil {
			ref.Name = s.registry.Name(player)
		}
		return ref, nil
	}
	if s.registry != nil {
		if uuid := s.registry.UUIDByName(player); uuid != "" {
			return PlayerRef{UUID: uuid, Name: s.registry.Name(uuid)}, nil
		}
	}
	return PlayerRef{}, ErrUnknownPlayer
}

// record logs an action made from the panel; player is a name or a uuid.
func (s *Service) record(action Action, player, reason string) {
	if s.registry == nil {
		return
	}
	id, _, _ := s.active()
	a := ActionRecord{Action: action, Reason: reason, SaveID: id, Source: SourcePanel, At: s.now()}
	if ValidUUID(player) {
		a.UUID = player
	} else {
		a.Name, a.UUID = player, s.registry.UUIDByName(player)
	}
	_ = s.registry.RecordAction(a) // best effort: the action itself succeeded
}

// Remove takes the player uuid out of list.
func (s *Service) Remove(list List, uuid string) error {
	if !ValidUUID(uuid) {
		return ErrInvalidUUID
	}
	id, dir, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	action, ok := map[List]Action{ListBans: ActionUnban, ListOperators: ActionDeop, ListWhitelist: ActionWhitelistRemove}[list]
	if !ok {
		return ErrUnknownList
	}
	if s.Live() {
		target := uuid
		if name := s.names.Resolver(dir)(uuid).Name; name != "" {
			target = name
		}
		return s.Act(action, target, "")
	}
	var remove func(string, string) error
	switch list {
	case ListBans:
		remove = RemoveBan
	case ListOperators:
		remove = RemoveOperator
	case ListWhitelist:
		remove = RemoveFromWhitelist
	}
	err = remove(dir, uuid)
	if s.shared == nil {
		if err != nil {
			return err
		}
		s.record(action, uuid, "")
		return nil
	}
	// Only in the shared lists (not applied to this world yet) is fine.
	if err != nil && !errors.Is(err, ErrNotInList) {
		return err
	}
	return s.shared.Remove(list, uuid, id, s.now())
}

// SetWhitelist enables or disables the whitelist.
func (s *Service) SetWhitelist(enabled bool) error {
	id, dir, err := s.active()
	if err != nil {
		return ErrNoActiveSave
	}
	if s.Live() {
		return s.console.Send(WhitelistCommand(enabled))
	}
	if err := SetWhitelistEnabled(dir, enabled); err != nil {
		return err
	}
	if s.shared != nil {
		return s.shared.SetWhitelistEnabled(enabled, id)
	}
	return nil
}

// Reconcile brings the world the server just booted in line with the
// shared lists through console commands, and returns how many it sent.
func (s *Service) Reconcile() (int, error) {
	if s.shared == nil || !s.Live() {
		return 0, nil
	}
	_, dir, err := s.active()
	if err != nil {
		return 0, ErrNoActiveSave
	}
	roster, err := ReadRoster(dir, s.names.Resolver(dir))
	if err != nil {
		return 0, err
	}
	changes, whitelist, err := s.shared.Plan(roster, s.now())
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []error
	for _, c := range changes {
		target := c.Player.UUID
		if c.Player.Name != "" && playerPattern.MatchString(c.Player.Name) {
			target = c.Player.Name
		}
		command, err := BuildCommand(c.Action, target, c.Reason)
		if err == nil {
			err = s.console.Send(command)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		sent++
	}
	if whitelist != nil {
		if err := s.console.Send(WhitelistCommand(*whitelist)); err != nil {
			errs = append(errs, err)
		} else {
			sent++
		}
	}
	return sent, errors.Join(errs...)
}

// Known lists the players the manager has seen.
func (s *Service) Known() ([]KnownPlayer, error) {
	if s.registry == nil {
		return []KnownPlayer{}, nil
	}
	return s.registry.List()
}

// Registered returns what the manager stored about uuid (activity, names,
// sessions, bans and moderation actions).
func (s *Service) Registered(uuid string) (RegisteredPlayer, bool, error) {
	if s.registry == nil {
		return RegisteredPlayer{}, false, nil
	}
	return s.registry.Get(uuid)
}
