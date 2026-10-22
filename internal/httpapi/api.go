// Package httpapi exposes the management interface over HTTP.
package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"hytale-server-manager/internal/auth"
	"hytale-server-manager/internal/commandcatalog"
	"hytale-server-manager/internal/config"
	"hytale-server-manager/internal/curseforge"
	"hytale-server-manager/internal/gameserver"
	"hytale-server-manager/internal/hostcheck"
	"hytale-server-manager/internal/hytaleauth"
	"hytale-server-manager/internal/install"
	"hytale-server-manager/internal/lifecycle"
	"hytale-server-manager/internal/metrics"
	"hytale-server-manager/internal/passkey"
	"hytale-server-manager/internal/players"
	"hytale-server-manager/internal/saves"
	"hytale-server-manager/internal/settings"
)

// Deps lists the services used by the HTTP layer.
type Deps struct {
	Config         config.Config
	Logger         *slog.Logger
	Credentials    *auth.CredentialStore
	Passkeys       *passkey.Service // optional
	PasswordNotice *auth.InitialPasswordNotice
	Sessions       *auth.SessionManager
	LoginLimiter   *auth.LoginLimiter
	HytaleAuth     *hytaleauth.Service
	Provisioner    *install.Provisioner
	Controller     *lifecycle.Controller
	Sleep          sleepState // optional
	HostWarnings   hostWarnings
	Metrics        metricsSource // optional
	MetricsStream  metricsStream // optional
	ErrorReports   errorReports  // optional
	Console        *gameserver.Console
	Players        *players.Tracker
	PlayerAccess   *players.Service
	Commands       *commandcatalog.Service
	Saves          *saves.Store
	Mods           *saves.ModStore
	CurseForge     *curseforge.Service
	Backups        *saves.BackupStore
	Settings       *settings.Store
	TmpDir         string
	Static         fs.FS
}

// sleepState is the sleep mode, as seen by the HTTP layer.
type sleepState interface {
	Sleeping() bool
	Cancel() bool
}

// hostWarnings lists the host problems reported by the server.
type hostWarnings interface {
	Warnings() []hostcheck.Warning
}

// metricsSource is the resource history of the server.
type metricsSource interface {
	Series(span time.Duration) metrics.Series
}

type api struct {
	deps Deps
	log  *slog.Logger
}

// NewHandler builds the HTTP handler serving the API and the interface.
func NewHandler(deps Deps) http.Handler {
	a := &api{deps: deps, log: deps.Logger}

	public := http.NewServeMux()
	public.HandleFunc("POST /api/login", a.login)
	public.HandleFunc("GET /api/session", a.session)
	if deps.Passkeys != nil {
		a.routePasskeysPublic(public)
	}

	private := http.NewServeMux()
	a.routeAccount(private)
	a.routeServer(private)
	a.routeHytale(private)
	a.routeSetup(private)
	a.routeSaves(private)
	a.routePlayers(private)
	a.routeSettings(private)
	a.routeMetrics(private)
	if deps.MetricsStream != nil {
		a.routeMetricsStream(private)
	}
	if deps.ErrorReports != nil {
		a.routeErrorReports(private)
	}
	a.routeCurseForge(private)
	if deps.Passkeys != nil {
		a.routePasskeys(private)
	}

	apiMux := http.NewServeMux()
	apiMux.Handle("/api/login", public)
	apiMux.Handle("/api/login/", public)
	apiMux.Handle("/api/session", public)
	apiMux.Handle("/api/", a.requireSession(private))

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.Handle("/api/", noStore(csrfGuard(apiMux)))
	root.Handle("/", spaHandler(deps.Static))

	return recoverPanics(a.log, securityHeaders(root))
}
