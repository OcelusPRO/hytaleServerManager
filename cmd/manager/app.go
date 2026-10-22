package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/auth"
	"hytale-server-manager/internal/autoupdate"
	"hytale-server-manager/internal/commandcatalog"
	"hytale-server-manager/internal/config"
	"hytale-server-manager/internal/curseforge"
	"hytale-server-manager/internal/database"
	"hytale-server-manager/internal/errorreports"
	"hytale-server-manager/internal/fsutil"
	"hytale-server-manager/internal/gameserver"
	"hytale-server-manager/internal/hostcheck"
	"hytale-server-manager/internal/httpapi"
	"hytale-server-manager/internal/hytaleapi"
	"hytale-server-manager/internal/hytaleauth"
	"hytale-server-manager/internal/install"
	"hytale-server-manager/internal/lifecycle"
	"hytale-server-manager/internal/metrics"
	"hytale-server-manager/internal/metricsstream"
	"hytale-server-manager/internal/passkey"
	"hytale-server-manager/internal/paths"
	"hytale-server-manager/internal/players"
	"hytale-server-manager/internal/saves"
	"hytale-server-manager/internal/secretbox"
	"hytale-server-manager/internal/settings"
	"hytale-server-manager/internal/sleep"
	"hytale-server-manager/web"
)

const (
	consoleHistoryLines = 5000
	defaultSaveName     = "Main world"
)

// app holds the wired services.
type app struct {
	logger      *slog.Logger
	handler     http.Handler
	controller  *lifecycle.Controller
	provisioner *install.Provisioner
	hytaleAuth  *hytaleauth.Service
	saves       *saves.Store
	commands    *commandcatalog.Service
	console     *gameserver.Console
	tracker     *players.Tracker
	playerSvc   *players.Service
	settings    *settings.Store
	backups     *saves.BackupStore
	updater     *autoupdate.Updater
	sleeper     *sleep.Sleeper
	hostChecks  *hostcheck.Monitor
	metrics     *metrics.Collector
	stream      *metricsstream.Service
	reports     *errorreports.Service
	db          *sql.DB
}

func buildApp(cfg config.Config, logger *slog.Logger) (*app, error) {
	layout := paths.New(cfg.DataDir)
	if err := layout.EnsureAll(); err != nil {
		return nil, err
	}
	removed, err := layout.CleanStale()
	for _, path := range removed {
		logger.Info("removed a leftover file", "path", path)
	}
	if err != nil {
		logger.Warn("could not remove every leftover file", "error", err)
	}
	if err := ensureMachineID(layout.MachineIDFile()); err != nil {
		logger.Warn("could not create the machine id", "error", err)
	}

	credentials, notice, err := bootstrapPassword(cfg, layout, logger)
	if err != nil {
		return nil, err
	}
	passkeys, err := passkey.Open(layout.File("passkeys.json"))
	if err != nil {
		return nil, err
	}
	if cfg.ResetPassword {
		// Recovery path: the new password must be enough to get back in.
		if err := passkeys.Reset(); err != nil {
			return nil, err
		}
		logger.Info("passkeys removed and password sign-in re-enabled (HSM_RESET_PASSWORD)")
	}
	settingsStore, err := settings.Open(layout.File("settings.json"))
	if err != nil {
		return nil, err
	}
	db, err := database.OpenWithBackup(layout.File("manager.db"), logger)
	if err != nil {
		return nil, err
	}
	hytaleAPI := hytaleapi.NewClient(&http.Client{Timeout: 30 * time.Second}, hytaleapi.DefaultEndpoints())
	box, err := secretbox.LoadOrCreate(layout.File("secret.key"))
	if err != nil {
		return nil, err
	}
	hytaleAuth, err := buildHytaleAuth(hytaleAPI, box, layout, logger)
	if err != nil {
		return nil, err
	}

	console := gameserver.NewConsole(consoleHistoryLines)
	process := gameserver.NewProcess(console)
	registry := players.NewRegistry(db)
	if n, err := registry.ImportJSON(layout.File("registered_players.json"), settingsStore.Get().ActiveSaveID); err != nil {
		return nil, fmt.Errorf("import the players: %w", err)
	} else if n > 0 {
		logger.Info("players moved to the database", "count", n)
	}
	if err := registry.CloseInterrupted(); err != nil {
		return nil, err
	}
	names := players.NewNameDirectory().WithRegistry(registry)
	tracker := players.NewTracker(names).WithRegistry(registry, func() string { return process.Status().SaveID })
	process.OnExit(func(gameserver.ExitInfo) { tracker.Reset() })

	fetcher := install.NewFetcher(&http.Client{Timeout: 3 * time.Hour})
	provisioner := install.NewProvisioner(install.ProvisionerDeps{
		Java:          install.NewJavaRuntime(layout.JavaDir(), layout.TmpDir(), fetcher),
		Game:          install.NewGameInstall(layout.GameDir(), layout.TmpDir(), hytaleAPI, hytaleAuth.AccessToken, fetcher),
		AccountReady:  hytaleAuth.Ready,
		ServerRunning: process.Active,
		Patchline:     func() string { return settingsStore.Get().Patchline },
		PinnedVersion: func() string { return settingsStore.Get().GameVersionPin },
		Logger:        logger,
	})

	saveStore := saves.NewStore(layout.SavesDir(), layout.TmpDir())
	backupLimits := archive.Limits{MaxFiles: 1_000_000, MaxTotalBytes: 200 << 30}
	stream := metricsstream.New(metricsstream.Deps{
		SecretPath: layout.File("metrics-stream.key"),
		Port:       cfg.MetricsStreamPort,
		Enabled:    func() bool { return settingsStore.Get().MetricsStream },
		OnPlayer: func(joined bool, uuid, name string) {
			if joined {
				tracker.Join(uuid, name)
			} else {
				tracker.Leave(uuid)
			}
		},
		Logger: logger,
	})
	reports, err := errorreports.New(errorreports.Deps{
		KeyPath: layout.File("error-reports.key"),
		DB:      db,
		Port:    cfg.ErrorReportsPort,
		Enabled: func() bool { return settingsStore.Get().ErrorReporting == settings.ErrorReportingManager },
		ForwardDSN: func() string {
			if st := settingsStore.Get(); st.ErrorReporting == settings.ErrorReportingManager {
				return st.SentryForwardDSN
			}
			return ""
		},
		Logger: logger,
	})
	if err != nil {
		return nil, err
	}
	if n, err := reports.Store().ImportJSON(layout.File("error-reports.json")); err != nil {
		return nil, fmt.Errorf("import the error reports: %w", err)
	} else if n > 0 {
		logger.Info("error reports moved to the database", "count", n)
	}
	var sleeper *sleep.Sleeper // needs the controller, which must free its port
	hookSocket := bindHookSocket(cfg, layout)
	controller := lifecycle.New(lifecycle.Deps{
		Process:  process,
		Console:  console,
		Settings: settingsStore,
		Saves:    saveStore,
		Install:  provisioner,
		Auth:     hytaleAuth,
		HomeDir:  layout.HomeDir(),
		PrepareSave: func(saveDir string) error {
			return errors.Join(stream.Prepare(saveDir), reports.Prepare(saveDir))
		},
		BeforeStart: func() {
			if sleeper != nil {
				sleeper.Release()
			}
		},
		LaunchEnv: func(st settings.Settings) []string {
			if !st.SleepEnabled || hookSocket == "" {
				return nil
			}
			return []string{
				"LD_PRELOAD=" + cfg.BindHookLibrary,
				"HSM_BINDHOOK_SOCK=" + hookSocket,
				"HSM_BINDHOOK_PORT=" + strconv.Itoa(st.Port),
			}
		},
	})
	sleeper = sleep.New(sleep.Deps{
		Config: func() sleep.Config {
			st := settingsStore.Get()
			return sleep.Config{
				Enabled:   st.SleepEnabled,
				IdleAfter: time.Duration(st.SleepIdleMinutes) * time.Minute,
				Port:      st.Port,
			}
		},
		Running:     controller.Running,
		PlayerCount: tracker.Count,
		// A pending automatic update restarts the server once it is empty:
		// let it go first rather than sleeping on the old build.
		Deferred: func() bool {
			_, pending := provisioner.UpdatePendingSince()
			return pending && settingsStore.Get().AutoUpdate
		},
		Stop:          controller.Stop,
		Start:         controller.Start,
		Restart:       controller.Restart,
		HookSocket:    hookSocket,
		TestWakeDelay: cfg.SleepTestWakeDelay,
		Log: func(message string) {
			console.System(message)
			logger.Info("sleep", "message", message)
		},
	})

	playerSvc := players.NewService(controller, names, func() (string, string, error) {
		id := settingsStore.Get().ActiveSaveID
		dir, err := saveStore.Dir(id)
		return id, dir, err
	}).WithRegistry(registry, players.NewSharedLists(db))

	commands := commandcatalog.NewService(commandcatalog.Deps{
		Builder: commandcatalog.NewBuilder(process, console),
		Console: console,
		SaveDir: saveStore.Dir,
		RunningSave: func() string {
			if !process.Active() {
				return ""
			}
			return process.Status().SaveID
		},
		GameVersion: func() string { return provisioner.Status().GameVersion },
		Logger:      logger,
	})

	modStore := saves.NewModStore(saveStore)
	curseKeys := curseforge.OpenKeyStore(layout.File("curseforge-key.enc"), box)
	curseService := curseforge.NewService(curseforge.Deps{
		// No overall timeout: mod downloads can be large (API calls are bounded individually).
		Client:       curseforge.NewClient(&http.Client{}, "", curseKeys.Get),
		Keys:         curseKeys,
		Mods:         modStore,
		TmpDir:       layout.TmpDir(),
		MaxFileBytes: cfg.MaxUploadBytes,
		PackLimits:   archive.Limits{MaxFiles: 200_000, MaxTotalBytes: cfg.MaxUploadBytes * 4},
		Logger:       logger,
	})

	backupStore := saves.NewBackupStore(layout.BackupsDir(), layout.TmpDir(), saveStore, backupLimits)

	hostChecks := hostcheck.NewMonitor()
	collector := metrics.New(metrics.Deps{
		Path: layout.File("metrics.json"),
		Probe: func() metrics.Probe {
			p := metrics.Probe{State: metrics.StateStopped, Players: tracker.Count()}
			// With the plugin connected, its readings are the reference.
			if s, ok := stream.Sample(); ok {
				p.Players = s.Players
				p.Plugin = &metrics.PluginSample{TPS: s.TPS, MSPT: s.MSPT, MSPTMax: s.MSPTMax, HeapMB: s.HeapMB}
			}
			switch st := process.Status(); {
			case process.Active():
				p.State, p.PID = metrics.StateRunning, st.PID
			case sleeper.Sleeping():
				p.State = metrics.StateSleeping
			}
			return p
		},
	})

	handler := httpapi.NewHandler(httpapi.Deps{
		Config:         cfg,
		Logger:         logger,
		Credentials:    credentials,
		Passkeys:       passkeys,
		PasswordNotice: notice,
		Sessions:       auth.NewSessionManager(12*time.Hour, 7*24*time.Hour),
		LoginLimiter:   auth.NewLoginLimiter(5, 15*time.Minute, 15*time.Minute),
		HytaleAuth:     hytaleAuth,
		Provisioner:    provisioner,
		Controller:     controller,
		Sleep:          sleeper,
		HostWarnings:   hostChecks,
		Metrics:        collector,
		MetricsStream:  stream,
		ErrorReports:   reports,
		Console:        console,
		Players:        tracker,
		Commands:       commands,
		PlayerAccess:   playerSvc,
		Saves:          saveStore,
		Mods:           modStore,
		CurseForge:     curseService,
		Backups:        backupStore,
		Settings:       settingsStore,
		TmpDir:         layout.TmpDir(),
		Static:         web.Static(),
	})

	updater := autoupdate.New(autoupdate.Deps{
		Config: func() autoupdate.Config {
			st := settingsStore.Get()
			return autoupdate.Config{
				Enabled:    st.AutoUpdate,
				IdleAfter:  time.Duration(st.AutoUpdateIdleMinutes) * time.Minute,
				ForceAfter: time.Duration(st.AutoUpdateForceHours) * time.Hour,
				Language:   st.GameLanguage,
			}
		},
		Running:       controller.Running,
		PlayerCount:   tracker.Count,
		PendingSince:  provisioner.UpdatePendingSince,
		Stop:          controller.Stop,
		Start:         controller.Start,
		Send:          controller.Send,
		RequestUpdate: provisioner.RequestUpdate,
		Backup: func() error {
			// The save of the process that just stopped, even if the
			// active save was changed meanwhile.
			id := controller.Status().SaveID
			if id == "" {
				return nil
			}
			_, err := backupStore.CreateNamed(id, saves.BackupOptions{Name: "Before server update", Origin: saves.OriginUpdate})
			return err
		},
		Log: func(message string) {
			console.System(message)
			logger.Info("auto update", "message", message)
		},
	})

	return &app{
		playerSvc:   playerSvc,
		updater:     updater,
		sleeper:     sleeper,
		hostChecks:  hostChecks,
		metrics:     collector,
		stream:      stream,
		reports:     reports,
		db:          db,
		logger:      logger,
		handler:     handler,
		controller:  controller,
		provisioner: provisioner,
		hytaleAuth:  hytaleAuth,
		saves:       saveStore,
		commands:    commands,
		console:     console,
		tracker:     tracker,
		settings:    settingsStore,
		backups:     backupStore,
	}, nil
}

// ensureMachineID creates the machine identifier the game server reads
// through /etc/machine-id (a link to this file in the image): containers
// have none, and a stable one survives the container being recreated.
func ensureMachineID(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, []byte(hex.EncodeToString(id)+"\n"), 0o644)
}

// bindHookSocket returns the socket the sleep bind hook library talks to, or
// "" when the library is unavailable (it only exists in the Linux image).
func bindHookSocket(cfg config.Config, layout paths.Layout) string {
	if runtime.GOOS != "linux" || cfg.BindHookLibrary == "" {
		return ""
	}
	if _, err := os.Stat(cfg.BindHookLibrary); err != nil {
		return ""
	}
	return filepath.Join(layout.TmpDir(), "bindhook.sock")
}

// bootstrapPassword generates the panel password on first launch (or when a
// reset is requested) and shows it once in the logs.
func bootstrapPassword(cfg config.Config, layout paths.Layout, logger *slog.Logger) (*auth.CredentialStore, *auth.InitialPasswordNotice, error) {
	credentials, err := auth.OpenCredentialStore(layout.File("credentials.json"))
	if err != nil {
		return nil, nil, err
	}
	notice := auth.NewInitialPasswordNotice(layout.File("initial-password.txt"))
	if credentials.Initialized() && !cfg.ResetPassword {
		return credentials, notice, nil
	}

	password := auth.GeneratePassword()
	if err := credentials.Set(password); err != nil {
		return nil, nil, err
	}
	if err := notice.Write(password); err != nil {
		return nil, nil, err
	}
	fmt.Printf("\n================================================================\n"+
		"  Management interface password: %s\n"+
		"  (also saved in %s until it is changed)\n"+
		"================================================================\n\n",
		password, notice.Path())
	logger.Info("panel password generated")
	return credentials, notice, nil
}

func buildHytaleAuth(api *hytaleapi.Client, box *secretbox.Box, layout paths.Layout, logger *slog.Logger) (*hytaleauth.Service, error) {
	store := hytaleauth.NewTokenStore(layout.File("hytale-auth.enc"), box)
	service, err := hytaleauth.NewService(api, store)
	if err == nil {
		return service, nil
	}
	// Unreadable credentials (e.g. lost key): start fresh, the operator
	// simply has to reconnect the Hytale account.
	logger.Warn("discarding unreadable Hytale credentials", "error", err)
	if err := store.Clear(); err != nil {
		return nil, err
	}
	return hytaleauth.NewService(api, store)
}

// startBackground launches long-running tasks tied to ctx.
func (a *app) startBackground(ctx context.Context) {
	go a.hytaleAuth.KeepAlive(ctx, a.logger)
	go a.provisioner.Run(ctx)
	go a.commands.Watch(ctx)
	go a.followConsole(ctx)
	go a.followAccess(ctx)
	go a.autosave(ctx)
	go a.updater.Run(ctx)
	go a.sleeper.Run(ctx)
	go a.metrics.Run(ctx)
	go a.reports.Run(ctx)
	a.stream.Reconcile()
	go func() { <-ctx.Done(); a.stream.Close() }()
	if err := a.ensureDefaultSave(); err != nil {
		a.logger.Warn("could not create the default world", "error", err)
	}
	if a.settings.Get().AutoStart {
		go a.autoStart(ctx)
	}
}

// ensureDefaultSave creates and activates a first world so that the server
// can start as soon as it is installed.
func (a *app) ensureDefaultSave() error {
	list, err := a.saves.List()
	if err != nil || len(list) > 0 {
		return err
	}
	save, err := a.saves.Create(defaultSaveName)
	if err != nil {
		return err
	}
	_, err = a.settings.Update(func(s *settings.Settings) { s.ActiveSaveID = save.ID })
	return err
}

// autoStart starts the server once Java and the game are installed.
func (a *app) autoStart(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		st := a.provisioner.Status()
		if st.JavaReady && st.GameReady && !a.provisioner.Busy() {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
	if err := a.controller.Start(ctx); err != nil {
		a.console.System("Automatic start failed: " + err.Error())
		a.logger.Warn("auto start failed", "error", err)
	}
}

// autosave periodically archives the world the server is running on, keeping
// a rotating set of autosaves. Settings are re-read on every tick so changes
// apply without a restart.
func (a *app) autosave(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		st := a.settings.Get()
		if !st.AutosaveEnabled {
			last = time.Now() // restart the countdown when re-enabled
			continue
		}
		if time.Since(last) < time.Duration(st.AutosaveMinutes)*time.Minute {
			continue
		}
		last = time.Now()
		if !a.controller.UsesSave(st.ActiveSaveID) {
			continue // nothing changes while the server is stopped
		}
		if _, err := a.backups.CreateAuto(st.ActiveSaveID, st.AutosaveSlots); err != nil {
			a.console.System("Autosave failed: " + err.Error())
			a.logger.Warn("autosave failed", "error", err)
			continue
		}
		a.logger.Info("autosave created", "save", st.ActiveSaveID)
	}
}

// followConsole feeds server output to the player tracker.
func (a *app) followConsole(ctx context.Context) {
	lines, cancel := a.console.Subscribe()
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case l := <-lines:
			if l.Stream == gameserver.StreamStdout || l.Stream == gameserver.StreamStderr {
				a.tracker.Observe(l.Text)
				a.sleeper.ObserveLine(l.Text)
				a.hostChecks.Observe(l.Text)
				if strings.Contains(l.Text, serverBootedLine) {
					go a.applySharedAccess()
				}
			}
		}
	}
}

// serverBootedLine is printed by the server once it accepts commands.
const serverBootedLine = "Hytale Server Booted!"

// applySharedAccess brings the world that just booted in line with the
// shared whitelist, bans and operators.
func (a *app) applySharedAccess() {
	sent, err := a.playerSvc.Reconcile()
	if sent > 0 {
		a.console.System(fmt.Sprintf("Shared whitelist, bans and operators applied to this world (%d change(s)).", sent))
	}
	if err != nil {
		a.console.System("Could not apply the shared whitelist, bans and operators: " + err.Error())
		a.logger.Warn("shared access lists not applied", "error", err)
	}
}

// followAccess takes the changes of the active world's whitelist, bans and
// operators (including those made from the game console) into the shared
// lists, by comparing its files periodically.
func (a *app) followAccess(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := a.playerSvc.Sync(); err != nil && !errors.Is(err, players.ErrNoActiveSave) {
			a.logger.Warn("access lists sync failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// shutdown stops the game server gracefully before the manager exits.
func (a *app) shutdown() {
	// Last: the server's final disconnections are recorded in it.
	defer a.db.Close()
	if !a.controller.Running() {
		return
	}
	a.logger.Info("stopping game server")
	if err := a.controller.Stop(); err != nil {
		a.logger.Warn("game server stop failed", "error", err)
	}
}
