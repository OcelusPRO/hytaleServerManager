package httpapi

import (
	"net/http"
	"strconv"

	"hytale-server-manager/internal/curseforge"
)

// routeCurseForge registers the CurseForge integration endpoints.
func (a *api) routeCurseForge(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/curseforge", a.curseForgeStatus)
	mux.HandleFunc("PUT /api/curseforge/key", a.setCurseForgeKey)
	mux.HandleFunc("DELETE /api/curseforge/key", a.clearCurseForgeKey)
	mux.HandleFunc("GET /api/curseforge/search", a.searchCurseForge)
	mux.HandleFunc("GET /api/curseforge/mods/{modId}/files", a.curseForgeFiles)
	mux.HandleFunc("GET /api/curseforge/jobs", a.listCurseForgeJobs)
	mux.HandleFunc("GET /api/curseforge/jobs/{jobId}", a.getCurseForgeJob)
	mux.HandleFunc("POST /api/saves/{id}/curseforge/mods", a.installCurseForgeMod)
	mux.HandleFunc("POST /api/saves/{id}/curseforge/modpacks", a.installCurseForgeModpack)
	mux.HandleFunc("GET /api/saves/{id}/curseforge/updates", a.checkCurseForgeUpdates)
	mux.HandleFunc("POST /api/saves/{id}/curseforge/updates", a.applyCurseForgeUpdates)
}

func (a *api) curseForgeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.CurseForge.Status(r.Context()))
}

func (a *api) setCurseForgeKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	if err := a.deps.CurseForge.SetKey(r.Context(), body.APIKey); err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, a.deps.CurseForge.Status(r.Context()))
}

func (a *api) clearCurseForgeKey(w http.ResponseWriter, _ *http.Request) {
	a.respondAction(w, a.deps.CurseForge.ClearKey())
}

func (a *api) searchCurseForge(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	res, err := a.deps.CurseForge.Search(r.Context(), curseforge.SearchQuery{
		Text:     q.Get("q"),
		Modpacks: q.Get("kind") == "modpacks",
		Sort:     q.Get("sort"),
		Page:     page,
	})
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *api) curseForgeFiles(w http.ResponseWriter, r *http.Request) {
	modID, ok := a.intPath(w, r, "modId")
	if !ok {
		return
	}
	files, err := a.deps.CurseForge.Files(r.Context(), modID)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (a *api) installCurseForgeMod(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModID        int  `json:"modId"`
		FileID       int  `json:"fileId"`
		Dependencies bool `json:"dependencies"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	if body.ModID <= 0 || body.FileID < 0 {
		writeError(w, a.log, errBadRequest("identifiants CurseForge invalides"))
		return
	}
	res, err := a.deps.CurseForge.InstallMod(r.Context(), r.PathValue("id"), body.ModID, body.FileID, body.Dependencies)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (a *api) installCurseForgeModpack(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ModpackID int `json:"modpackId"`
		FileID    int `json:"fileId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	if body.ModpackID <= 0 || body.FileID < 0 {
		writeError(w, a.log, errBadRequest("identifiants CurseForge invalides"))
		return
	}
	job, err := a.deps.CurseForge.StartModpack(r.PathValue("id"), body.ModpackID, body.FileID)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (a *api) checkCurseForgeUpdates(w http.ResponseWriter, r *http.Request) {
	updates, err := a.deps.CurseForge.CheckUpdates(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusOK, updates)
}

func (a *api) applyCurseForgeUpdates(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileNames []string `json:"fileNames"` // empty = every outdated mod
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, a.log, err)
		return
	}
	job, err := a.deps.CurseForge.StartUpdates(r.PathValue("id"), body.FileNames)
	if err != nil {
		writeError(w, a.log, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (a *api) listCurseForgeJobs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.deps.CurseForge.Jobs())
}

func (a *api) getCurseForgeJob(w http.ResponseWriter, r *http.Request) {
	job, ok := a.deps.CurseForge.Job(r.PathValue("jobId"))
	if !ok {
		writeMessage(w, http.StatusNotFound, "operation not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (a *api) intPath(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil || n <= 0 {
		writeError(w, a.log, errBadRequest("invalid identifier"))
		return 0, false
	}
	return n, true
}
