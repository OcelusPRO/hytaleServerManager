// Package autoupdate applies pending game updates without operator action.
//
// A pending update is installed when the server has been empty for a while.
// If players keep it busy for too long, they get a countdown by console
// message and the update is applied once it ends. Either way the server is
// stopped cleanly and the world is backed up before the game is replaced.
package autoupdate

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	tickInterval = time.Second
	// countdownLength is the notice given to players before a forced update.
	countdownLength = 30 * time.Minute
	// retryDelay keeps a failing update from stopping the server in a loop.
	retryDelay = time.Hour
	// nativeSaveWait leaves the server time to finish /world save.
	nativeSaveWait = 5 * time.Second
)

// warnings are the moments (time left) at which players are told, in
// descending order. The first one is the countdown length.
var warnings = []time.Duration{
	30 * time.Minute,
	15 * time.Minute,
	10 * time.Minute,
	5 * time.Minute,
	time.Minute,
	30 * time.Second,
	10 * time.Second,
}

// Config is the operator-tunable behaviour, re-read on every tick.
type Config struct {
	Enabled    bool
	IdleAfter  time.Duration // empty server for this long => update now
	ForceAfter time.Duration // update pending for this long => countdown
}

// Deps lists the collaborators of the Updater.
type Deps struct {
	Config       func() Config
	Running      func() bool
	PlayerCount  func() int
	PendingSince func() (time.Time, bool)
	Stop         func() error
	Start        func(ctx context.Context) error
	Send         func(command string) error
	// Backup archives the world while the server is stopped.
	Backup func() error
	// RequestUpdate installs the latest build, then calls after.
	RequestUpdate func(after func()) error
	Log           func(message string)
	Now           func() time.Time
	// Wait pauses between steps (defaults to time.Sleep).
	Wait func(time.Duration)
}

// Updater watches for pending updates and applies them at a good moment.
type Updater struct {
	d Deps

	idleSince  time.Time
	deadline   time.Time // end of the running countdown, zero when none
	warned     map[time.Duration]bool
	retryAfter time.Time
}

// New creates an updater; call Run to start it.
func New(d Deps) *Updater {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Wait == nil {
		d.Wait = time.Sleep
	}
	return &Updater{d: d, warned: make(map[time.Duration]bool)}
}

// Run evaluates the situation every second until ctx ends.
func (u *Updater) Run(ctx context.Context) {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.tick(ctx)
		}
	}
}

func (u *Updater) tick(ctx context.Context) {
	now := u.d.Now()
	cfg := u.d.Config()
	pending, hasUpdate := u.d.PendingSince()
	if !cfg.Enabled || !hasUpdate || !u.d.Running() || now.Before(u.retryAfter) {
		u.reset()
		return
	}

	if u.d.PlayerCount() == 0 {
		u.deadline = time.Time{}
		clear(u.warned)
		if u.idleSince.IsZero() {
			u.idleSince = now
		}
		if now.Sub(u.idleSince) >= cfg.IdleAfter {
			u.apply(ctx, "serveur vide depuis "+cfg.IdleAfter.String())
		}
		return
	}
	u.idleSince = time.Time{}

	if u.deadline.IsZero() {
		if now.Sub(pending) < cfg.ForceAfter {
			return
		}
		u.deadline = now.Add(countdownLength)
		u.log(fmt.Sprintf("Mise à jour en attente depuis plus de %s : compte à rebours lancé.", cfg.ForceAfter))
	}
	remaining := u.deadline.Sub(now)
	if remaining <= 0 {
		u.alert("Arrêt du serveur", "Mise à jour en cours, il redémarre dans un instant", true)
		u.apply(ctx, "compte à rebours terminé")
		return
	}
	u.announce(remaining)
}

// announce tells the players about the smallest warning threshold crossed
// since the last tick, so a delayed tick never replays old warnings.
func (u *Updater) announce(remaining time.Duration) {
	var due time.Duration
	for _, w := range warnings {
		if remaining <= w && !u.warned[w] {
			u.warned[w] = true
			due = w
		}
	}
	if due > 0 {
		u.alert("Mise à jour du serveur dans "+describe(due), "Le serveur va redémarrer", due <= time.Minute)
	}
}

// apply stops the server, backs the world up and starts the installation.
func (u *Updater) apply(ctx context.Context, reason string) {
	u.reset()
	u.retryAfter = u.d.Now().Add(retryDelay)
	u.log("Mise à jour automatique (" + reason + ") : arrêt du serveur…")

	// Native save while the server is still up; stopping saves again.
	if err := u.d.Send("/world save"); err != nil {
		u.log("Sauvegarde native impossible : " + err.Error())
	} else {
		u.d.Wait(nativeSaveWait)
	}
	if err := u.d.Stop(); err != nil {
		u.log("Mise à jour automatique abandonnée, arrêt impossible : " + err.Error())
		return
	}
	if err := u.d.Backup(); err != nil {
		u.log("Mise à jour automatique abandonnée, sauvegarde impossible : " + err.Error())
		u.restart(ctx)
		return
	}
	u.log("Sauvegarde terminée, installation de la mise à jour…")
	err := u.d.RequestUpdate(func() { u.restart(context.Background()) })
	if err != nil {
		u.log("Mise à jour automatique impossible : " + err.Error())
		u.restart(ctx)
	}
}

func (u *Updater) restart(ctx context.Context) {
	if err := u.d.Start(ctx); err != nil {
		u.log("Redémarrage après mise à jour impossible : " + err.Error())
	}
}

func (u *Updater) reset() {
	u.idleSince, u.deadline = time.Time{}, time.Time{}
	clear(u.warned)
}

// alert shows a message to every player: a toast (/notify) and a large
// on-screen title (/eventtitle), major for the last minute.
func (u *Updater) alert(title, subtitle string, major bool) {
	title, subtitle = sanitize(title), sanitize(subtitle)
	commands := []string{
		"/notify " + title + ". " + subtitle,
		fmt.Sprintf(`/eventtitle --title "%s" --secondary "%s"`, title, subtitle),
	}
	if major {
		commands[1] += " --major"
	}
	for _, c := range commands {
		if err := u.d.Send(c); err != nil {
			u.log("Annonce impossible : " + err.Error())
			return
		}
	}
}

// sanitize keeps player-facing text safe inside a quoted console argument.
func sanitize(text string) string {
	return strings.ReplaceAll(text, `"`, "'")
}

func (u *Updater) log(message string) {
	if u.d.Log != nil {
		u.d.Log(message)
	}
}

func describe(d time.Duration) string {
	switch {
	case d >= time.Minute && d%time.Minute == 0:
		if d == time.Minute {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	default:
		return fmt.Sprintf("%d secondes", int(d/time.Second))
	}
}
