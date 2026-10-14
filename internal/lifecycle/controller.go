// Package lifecycle orchestrates the server life: it checks prerequisites,
// obtains a Hytale game session and starts, stops or restarts the process.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"hytale-server-manager/internal/gameserver"
	"hytale-server-manager/internal/hytaleauth"
	"hytale-server-manager/internal/install"
	"hytale-server-manager/internal/settings"
)

// Errors explaining why the server cannot start.
var (
	ErrInstallRunning     = errors.New("le serveur est en cours de mise à jour, patientez")
	ErrJavaMissing        = errors.New("Java est en cours de téléchargement, patientez")
	ErrGameMissing        = errors.New("les fichiers du serveur ne sont pas encore téléchargés : connectez votre compte Hytale")
	ErrNoActiveSave       = errors.New("aucun monde actif : créez-en ou sélectionnez-en un")
	ErrHytaleAuthRequired = errors.New("connectez votre compte Hytale ou passez en mode offline")
)

const (
	crashRestartDelay = 10 * time.Second
	sessionCallTime   = 30 * time.Second
)

type installation interface {
	Busy() bool
	JavaBinary() (string, bool)
	GameFiles() (install.GameFiles, bool)
}

type sessionIssuer interface {
	Authenticated() bool
	NewGameSession(ctx context.Context) (hytaleauth.GameSessionGrant, error)
	TerminateGameSession(ctx context.Context, sessionToken string) error
}

type saveLocator interface {
	Dir(id string) (string, error)
}

// Status combines the process status with the configured save.
type Status struct {
	gameserver.Status
	ActiveSaveID string `json:"activeSaveId"`
	Installing   bool   `json:"installing"`
}

// Controller drives the server process.
type Controller struct {
	process  *gameserver.Process
	console  *gameserver.Console
	settings *settings.Store
	saves    saveLocator
	install  installation
	auth     sessionIssuer
	homeDir  string
	// beforeStart frees resources the server needs (the game port held
	// while asleep) right before the process starts.
	beforeStart func()
	launchEnv   func(settings.Settings) []string

	ops      sync.Mutex // serialises start/stop/restart
	session  sessionHolder
	restarts *restartBudget
}

// Deps lists the collaborators of the Controller.
type Deps struct {
	Process  *gameserver.Process
	Console  *gameserver.Console
	Settings *settings.Store
	Saves    saveLocator
	Install  installation
	Auth     sessionIssuer
	HomeDir  string
	// BeforeStart, optional, runs right before the process starts.
	BeforeStart func()
	// LaunchEnv, optional, adds environment variables to the server.
	LaunchEnv func(settings.Settings) []string
}

// New creates a controller and subscribes to process exits.
func New(d Deps) *Controller {
	c := &Controller{
		process:     d.Process,
		console:     d.Console,
		settings:    d.Settings,
		saves:       d.Saves,
		install:     d.Install,
		auth:        d.Auth,
		homeDir:     d.HomeDir,
		beforeStart: d.BeforeStart,
		launchEnv:   d.LaunchEnv,
		restarts:    newRestartBudget(3, 10*time.Minute),
	}
	d.Process.OnExit(c.handleExit)
	return c
}

// Start launches the server with the active save.
func (c *Controller) Start(ctx context.Context) error {
	c.ops.Lock()
	defer c.ops.Unlock()
	return c.startLocked(ctx)
}

// Stop shuts the server down within the configured timeout.
func (c *Controller) Stop() error {
	c.ops.Lock()
	defer c.ops.Unlock()
	return c.stopLocked()
}

// Restart stops the server if needed and starts it again.
func (c *Controller) Restart(ctx context.Context) error {
	c.ops.Lock()
	defer c.ops.Unlock()
	if c.process.Active() {
		if err := c.stopLocked(); err != nil {
			return err
		}
	}
	return c.startLocked(ctx)
}

// Kill terminates the server immediately.
func (c *Controller) Kill() error { return c.process.Kill() }

// Send forwards a console command.
func (c *Controller) Send(command string) error { return c.process.Send(command) }

// Status reports the server state.
func (c *Controller) Status() Status {
	return Status{
		Status:       c.process.Status(),
		ActiveSaveID: c.settings.Get().ActiveSaveID,
		Installing:   c.install.Busy(),
	}
}

// Running reports whether the server process is alive.
func (c *Controller) Running() bool { return c.process.Active() }

// UsesSave reports whether the running server uses the given save.
func (c *Controller) UsesSave(id string) bool {
	st := c.process.Status()
	return c.process.Active() && st.SaveID == id
}

func (c *Controller) startLocked(ctx context.Context) error {
	if c.process.Active() {
		return gameserver.ErrAlreadyRunning
	}
	if c.install.Busy() {
		return ErrInstallRunning
	}
	javaBin, ok := c.install.JavaBinary()
	if !ok {
		return ErrJavaMissing
	}
	files, ok := c.install.GameFiles()
	if !ok {
		return ErrGameMissing
	}
	st := c.settings.Get()
	if st.ActiveSaveID == "" {
		return ErrNoActiveSave
	}
	saveDir, err := c.saves.Dir(st.ActiveSaveID)
	if err != nil {
		return ErrNoActiveSave
	}

	javaTmp := filepath.Join(c.homeDir, "tmp")
	if err := os.MkdirAll(javaTmp, 0o750); err != nil {
		return err
	}
	tokens, err := c.gameSession(ctx, st)
	if err != nil {
		return err
	}
	var extraEnv []string
	if c.launchEnv != nil {
		extraEnv = c.launchEnv(st)
	}
	spec := gameserver.BuildLaunchSpec(gameserver.LaunchInputs{
		JavaBin:   javaBin,
		ServerJar: files.ServerJar,
		AssetsZip: files.AssetsZip,
		AOTCache:  files.AOTCache,
		SaveID:    st.ActiveSaveID,
		SaveDir:   saveDir,
		HomeDir:   c.homeDir,
		TmpDir:    javaTmp,
		Settings:  st,
		Tokens:    tokens,
		ExtraEnv:  extraEnv,
	})
	c.console.System(fmt.Sprintf("Démarrage du serveur (sauvegarde %s, mode %s)…", st.ActiveSaveID, st.AuthMode))
	if c.beforeStart != nil {
		c.beforeStart()
	}
	if err := c.process.Start(spec); err != nil {
		c.endSession()
		return err
	}
	return nil
}

func (c *Controller) stopLocked() error {
	timeout := time.Duration(c.settings.Get().StopTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.process.Stop(ctx)
}

// gameSession obtains game session tokens in authenticated mode.
func (c *Controller) gameSession(ctx context.Context, st settings.Settings) (*gameserver.SessionTokens, error) {
	if st.AuthMode != settings.AuthModeAuthenticated {
		return nil, nil
	}
	if !c.auth.Authenticated() {
		return nil, ErrHytaleAuthRequired
	}
	ctx, cancel := context.WithTimeout(ctx, sessionCallTime)
	defer cancel()
	grant, err := c.auth.NewGameSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("session de jeu Hytale: %w", err)
	}
	c.session.set(grant.SessionToken)
	return &gameserver.SessionTokens{
		SessionToken:  grant.SessionToken,
		IdentityToken: grant.IdentityToken,
		OwnerUUID:     grant.OwnerUUID,
	}, nil
}

// endSession terminates the game session handed to the last process.
func (c *Controller) endSession() {
	token := c.session.take()
	if token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionCallTime)
	defer cancel()
	_ = c.auth.TerminateGameSession(ctx, token) // best effort, sessions expire anyway
}

func (c *Controller) handleExit(info gameserver.ExitInfo) {
	go c.endSession()
	crashed := !info.Expected && info.ExitCode != 0
	if !crashed || !c.settings.Get().AutoRestart {
		return
	}
	if !c.restarts.take() {
		c.console.System("Trop de plantages récents : redémarrage automatique désactivé.")
		return
	}
	c.console.System(fmt.Sprintf("Plantage détecté, redémarrage dans %s…", crashRestartDelay))
	go func() {
		time.Sleep(crashRestartDelay)
		if err := c.Start(context.Background()); err != nil && !errors.Is(err, gameserver.ErrAlreadyRunning) {
			c.console.System("Redémarrage automatique impossible : " + err.Error())
		}
	}()
}
