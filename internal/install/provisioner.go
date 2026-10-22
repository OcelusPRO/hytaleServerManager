package install

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hytale-server-manager/internal/hytaleapi"
)

// ErrServerRunning is returned when an update needs the server stopped.
var ErrServerRunning = errors.New("stop the server before updating it")

// Activity is what the provisioner is currently doing.
type Activity string

// Activities.
const (
	ActivityIdle        Activity = "idle"
	ActivityJava        Activity = "java"
	ActivityChecking    Activity = "checking"
	ActivityDownloading Activity = "downloading"
	ActivityExtracting  Activity = "extracting"
)

const (
	reconcileInterval = 5 * time.Second
	updateInterval    = time.Hour
	retryDelay        = time.Minute
)

// Status describes the installation for the interface.
type Status struct {
	JavaReady       bool   `json:"javaReady"`
	JavaVersion     string `json:"javaVersion,omitempty"`
	GameReady       bool   `json:"gameReady"`
	GameVersion     string `json:"gameVersion,omitempty"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	// PinnedVersion is the build the operator froze the game on ("" = follow
	// the latest).
	PinnedVersion     string   `json:"pinnedVersion"`
	WaitingForAccount bool     `json:"waitingForAccount"`
	Activity          Activity `json:"activity"`
	// Progress is the completed fraction of the activity, or -1 if unknown.
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`
}

// Provisioner keeps the runtime and the game installed without operator
// intervention: Java is fetched at startup, the game as soon as a Hytale
// account is connected, and updates are applied while the server is stopped.
type Provisioner struct {
	java          *JavaRuntime
	game          *GameInstall
	accountReady  func() bool
	serverRunning func() bool
	patchline     func() string
	pinned        func() string
	logger        *slog.Logger
	wake          chan struct{}

	mu              sync.Mutex
	activity        Activity
	progress        float64
	lastErr         string
	latest          string
	lastCheck       time.Time
	checkedLine     string // patchline of the last check
	nextAttempt     time.Time
	updateRequested bool
	afterUpdate     func()
	pendingSince    time.Time // when the installed build was first seen outdated
}

// ProvisionerDeps lists the collaborators of the Provisioner.
type ProvisionerDeps struct {
	Java          *JavaRuntime
	Game          *GameInstall
	AccountReady  func() bool
	ServerRunning func() bool
	Patchline     func() string
	// PinnedVersion returns the frozen build, or "" to follow the latest.
	PinnedVersion func() string
	Logger        *slog.Logger
}

// NewProvisioner creates an idle provisioner; call Run to start it.
func NewProvisioner(d ProvisionerDeps) *Provisioner {
	if d.PinnedVersion == nil {
		d.PinnedVersion = func() string { return "" }
	}
	return &Provisioner{
		pinned:        d.PinnedVersion,
		java:          d.Java,
		game:          d.Game,
		accountReady:  d.AccountReady,
		serverRunning: d.ServerRunning,
		patchline:     d.Patchline,
		logger:        d.Logger,
		wake:          make(chan struct{}, 1),
		activity:      ActivityIdle,
	}
}

// Run installs Java, then keeps the game installed and up to date.
func (p *Provisioner) Run(ctx context.Context) {
	p.ensureJava(ctx)
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		p.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.wake:
		}
	}
}

// Wake asks for an immediate reconciliation (e.g. right after login).
func (p *Provisioner) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// RequestUpdate installs the latest build as soon as possible and calls
// after (which may be nil) once it succeeded.
func (p *Provisioner) RequestUpdate(after func()) error {
	if p.serverRunning() {
		return ErrServerRunning
	}
	p.mu.Lock()
	p.updateRequested, p.afterUpdate, p.nextAttempt = true, after, time.Time{}
	p.mu.Unlock()
	p.Wake()
	return nil
}

// Status reports the installation state.
func (p *Provisioner) Status() Status {
	javaVersion := p.java.Version()
	_, gameReady := p.game.Files()
	gameVersion := p.game.Version()
	accountReady := p.accountReady()
	pin := p.pinned()

	p.mu.Lock()
	defer p.mu.Unlock()
	return Status{
		JavaReady:         javaVersion != "",
		JavaVersion:       javaVersion,
		GameReady:         gameReady,
		GameVersion:       gameVersion,
		LatestVersion:     p.latest,
		UpdateAvailable:   pin == "" && gameReady && p.latest != "" && p.latest != gameVersion,
		PinnedVersion:     pin,
		WaitingForAccount: !gameReady && !accountReady,
		Activity:          p.activity,
		Progress:          p.progress,
		Error:             p.lastErr,
	}
}

// Busy reports whether game files are being replaced.
func (p *Provisioner) Busy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activity == ActivityDownloading || p.activity == ActivityExtracting
}

// JavaBinary returns the installed java executable.
func (p *Provisioner) JavaBinary() (string, bool) { return p.java.Binary() }

// GameFiles returns the installed game files.
func (p *Provisioner) GameFiles() (GameFiles, bool) { return p.game.Files() }

func (p *Provisioner) ensureJava(ctx context.Context) {
	for {
		if _, ok := p.java.Binary(); ok {
			return
		}
		p.begin(ActivityJava, 0)
		err := p.java.Install(ctx, p.reportProgress)
		p.end(err)
		if err == nil {
			return
		}
		p.logger.Warn("java installation failed, retrying", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}

func (p *Provisioner) reconcile(ctx context.Context) {
	if !p.accountReady() {
		return
	}
	line := p.patchline()
	p.mu.Lock()
	backingOff := time.Now().Before(p.nextAttempt)
	requested := p.updateRequested
	// A new patchline is checked at once: its latest build differs.
	dueCheck := time.Since(p.lastCheck) >= updateInterval || line != p.checkedLine
	p.mu.Unlock()
	if backingOff {
		return
	}

	_, installed := p.game.Files()
	pin := p.pinned()
	switch {
	case !installed || requested:
		p.installLatest(ctx)
	case pin != "" && pin != p.game.Version() && !p.serverRunning():
		p.installLatest(ctx) // switch to the pinned build
	case pin != "":
		// Frozen: never update on its own, only keep the latest version known.
		if dueCheck {
			p.refreshLatest(ctx)
		}
	case dueCheck && !p.serverRunning():
		p.installLatest(ctx)
	case dueCheck:
		p.refreshLatest(ctx)
	}
}

// refreshLatest only records the newest version (server is running).
func (p *Provisioner) refreshLatest(ctx context.Context) {
	if _, err := p.latestManifest(ctx); err != nil {
		p.fail(err)
	}
}

func (p *Provisioner) installLatest(ctx context.Context) {
	if p.serverRunning() {
		return
	}
	p.begin(ActivityChecking, -1)
	pin := p.pinned()
	if _, installed := p.game.Files(); installed && pin != "" && p.game.Version() == pin {
		p.succeed()
		return
	}
	m, err := p.latestManifest(ctx)
	if err != nil {
		p.fail(err)
		return
	}
	if pin != "" && m.Version != pin {
		p.fail(fmt.Errorf("version %s is not installed and can no longer be downloaded (latest version: %s)", pin, m.Version))
		return
	}
	if _, installed := p.game.Files(); installed && m.Version == p.game.Version() {
		p.succeed()
		return
	}
	p.logger.Info("installing game build", "version", m.Version)
	p.begin(ActivityDownloading, 0)
	if err := p.game.Install(ctx, m, p.reportProgress); err != nil {
		p.fail(err)
		return
	}
	p.logger.Info("game build installed", "version", m.Version)
	p.succeed()
}

func (p *Provisioner) latestManifest(ctx context.Context) (hytaleapi.Manifest, error) {
	line := p.patchline()
	m, err := p.game.Latest(ctx, line)
	if err != nil {
		return m, err
	}
	_, installed := p.game.Files()
	outdated := installed && p.pinned() == "" && m.Version != p.game.Version()
	p.mu.Lock()
	p.latest, p.lastCheck, p.checkedLine = m.Version, time.Now(), line
	switch {
	case !outdated:
		p.pendingSince = time.Time{}
	case p.pendingSince.IsZero():
		p.pendingSince = p.lastCheck
	}
	p.mu.Unlock()
	return m, nil
}

// UpdatePendingSince reports since when a newer build than the installed one
// is known to exist.
func (p *Provisioner) UpdatePendingSince() (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pendingSince, !p.pendingSince.IsZero() && p.pinned() == ""
}

func (p *Provisioner) begin(activity Activity, progress float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activity, p.progress = activity, progress
}

// reportProgress records download progress; a complete download moves on
// to the extraction step.
func (p *Provisioner) reportProgress(fraction float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if fraction >= 1 && p.activity == ActivityDownloading {
		p.activity, p.progress = ActivityExtracting, -1
		return
	}
	p.progress = fraction
}

func (p *Provisioner) end(err error) {
	if err != nil {
		p.fail(err)
		return
	}
	p.begin(ActivityIdle, 0)
	p.mu.Lock()
	p.lastErr = ""
	p.mu.Unlock()
}

func (p *Provisioner) succeed() {
	p.mu.Lock()
	p.activity, p.progress, p.lastErr = ActivityIdle, 0, ""
	p.updateRequested = false
	p.pendingSince = time.Time{}
	after := p.afterUpdate
	p.afterUpdate = nil
	p.mu.Unlock()
	if after != nil {
		go after()
	}
}

func (p *Provisioner) fail(err error) {
	p.logger.Warn("provisioning failed", "error", err)
	p.mu.Lock()
	p.activity, p.progress = ActivityIdle, 0
	p.lastErr = err.Error()
	p.nextAttempt = time.Now().Add(retryDelay)
	// A requested update that failed must not leave the server stopped.
	p.updateRequested = false
	after := p.afterUpdate
	p.afterUpdate = nil
	p.mu.Unlock()
	if after != nil {
		go after()
	}
}
