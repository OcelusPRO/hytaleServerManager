package httpapi

import (
	"errors"
	"net/http"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/auth"
	"hytale-server-manager/internal/curseforge"
	"hytale-server-manager/internal/gameserver"
	"hytale-server-manager/internal/hytaleapi"
	"hytale-server-manager/internal/hytaleauth"
	"hytale-server-manager/internal/install"
	"hytale-server-manager/internal/lifecycle"
	"hytale-server-manager/internal/players"
	"hytale-server-manager/internal/saves"
	"hytale-server-manager/internal/serverconfig"
)

// httpError is an error carrying its own status and public message.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func errBadRequest(msg string) error { return &httpError{status: http.StatusBadRequest, message: msg} }
func errConflict(msg string) error   { return &httpError{status: http.StatusConflict, message: msg} }

// Known errors whose message is safe to show, grouped by status.
var publicErrors = map[int][]error{
	http.StatusBadRequest: {
		serverconfig.ErrUnreadable, serverconfig.ErrBadChange,
		saves.ErrInvalidID, saves.ErrInvalidName, saves.ErrInvalidModName, saves.ErrNotAnArchive,
		saves.ErrInvalidBackupID, players.ErrUnknownAction, players.ErrInvalidPlayer,
		players.ErrInvalidReason, gameserver.ErrInvalidCommand, auth.ErrWeakPassword,
		hytaleauth.ErrUnknownProfile, archive.ErrUnsafePath, players.ErrInvalidUUID, players.ErrUnknownList,
		curseforge.ErrInvalidAPIKey, curseforge.ErrUnsupportedPack, curseforge.ErrNotAModArchive,
	},
	http.StatusForbidden: {auth.ErrWrongPassword},
	http.StatusNotFound:  {serverconfig.ErrUnknownFile, saves.ErrNotFound, players.ErrNotInList},
	http.StatusConflict: {
		saves.ErrModExists, gameserver.ErrAlreadyRunning, gameserver.ErrNotRunning,
		install.ErrServerRunning, lifecycle.ErrInstallRunning,
		lifecycle.ErrJavaMissing, lifecycle.ErrGameMissing, lifecycle.ErrNoActiveSave,
		lifecycle.ErrHytaleAuthRequired, hytaleauth.ErrNotAuthenticated, hytaleauth.ErrNoProfile,
		hytaleauth.ErrReauthRequired, players.ErrServerStopped, players.ErrNoActiveSave,
		curseforge.ErrNoAPIKey, curseforge.ErrBusy, curseforge.ErrNoFile, curseforge.ErrNoModpacks,
		curseforge.ErrGameNotFound, curseforge.ErrChecksum,
	},
	http.StatusRequestEntityTooLarge: {saves.ErrModTooLarge, archive.ErrTooLarge},
}

func classify(err error) (int, string) {
	var he *httpError
	if errors.As(err, &he) {
		return he.status, he.message
	}
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return http.StatusRequestEntityTooLarge, "fichier trop volumineux"
	}
	for status, known := range publicErrors {
		for _, k := range known {
			if errors.Is(err, k) {
				return status, k.Error()
			}
		}
	}
	var disabled *curseforge.DownloadDisabledError
	if errors.As(err, &disabled) {
		return http.StatusConflict, disabled.Error()
	}
	var cfErr *curseforge.APIError
	if errors.As(err, &cfErr) {
		return http.StatusBadGateway, cfErr.Error()
	}
	var apiErr *hytaleapi.APIError
	if errors.As(err, &apiErr) {
		return http.StatusBadGateway, "Hytale a refusé la requête : " + apiErr.Error()
	}
	return http.StatusInternalServerError, "erreur interne, consultez les journaux du conteneur"
}
