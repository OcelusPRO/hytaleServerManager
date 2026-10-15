package curseforge

import (
	"context"
	"crypto/sha1" //nolint:gosec // CurseForge publishes SHA-1 checksums
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"hytale-server-manager/internal/archive"
	"hytale-server-manager/internal/saves"
)

// Errors reported to the operator.
var (
	ErrGameNotFound   = errors.New("Hytale is not in the CurseForge catalog")
	ErrNoModpacks     = errors.New("CurseForge has no modpack category for Hytale")
	ErrNoFile         = errors.New("no downloadable file for this project")
	ErrBusy           = errors.New("a CurseForge operation is already running")
	ErrChecksum       = errors.New("checksum mismatch, download rejected")
	ErrNotAModArchive = errors.New("the CurseForge file is not an installable .jar/.zip")
)

// DownloadDisabledError reports a file its author keeps off third-party tools.
type DownloadDisabledError struct {
	Name string
	URL  string
}

func (e *DownloadDisabledError) Error() string {
	if e.URL != "" {
		return fmt.Sprintf("the author of “%s” does not allow downloads by third-party tools: download it from %s, then add the file manually", e.Name, e.URL)
	}
	return fmt.Sprintf("the author of “%s” does not allow downloads by third-party tools", e.Name)
}

// Deps wires the Service.
type Deps struct {
	Client       *Client
	Keys         *KeyStore
	Mods         *saves.ModStore
	TmpDir       string
	MaxFileBytes int64
	PackLimits   archive.Limits
	Logger       *slog.Logger
}

// Service implements the CurseForge features of the manager.
type Service struct {
	Deps
	jobs *jobBoard

	mu      sync.Mutex
	catalog *catalog
}

// catalog holds the CurseForge identifiers of Hytale, discovered at runtime.
type catalog struct {
	gameID       int
	modsClassID  int
	packsClassID int
}

// NewService builds the service.
func NewService(d Deps) *Service {
	return &Service{Deps: d, jobs: newJobBoard()}
}

// Status describes the CurseForge configuration.
type Status struct {
	Configured bool   `json:"configured"`
	KeyHint    string `json:"keyHint,omitempty"`
	Ready      bool   `json:"ready"`
	Modpacks   bool   `json:"modpacks"`
	Error      string `json:"error,omitempty"`
}

// Status reports whether the key is set and whether Hytale can be resolved.
func (s *Service) Status(ctx context.Context) Status {
	key := s.Keys.Get()
	if key == "" {
		return Status{}
	}
	st := Status{Configured: true, KeyHint: Hint(key)}
	cat, err := s.resolve(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Ready = true
	st.Modpacks = cat.packsClassID != 0
	return st
}

// SetKey validates and stores a new API key.
func (s *Service) SetKey(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty API key")
	}
	probe := NewClient(s.Client.http, s.Client.baseURL, func() string { return key })
	if _, err := probe.Games(ctx); err != nil {
		return err
	}
	if err := s.Keys.Set(key); err != nil {
		return err
	}
	s.resetCatalog()
	return nil
}

// ClearKey forgets the API key.
func (s *Service) ClearKey() error {
	s.resetCatalog()
	return s.Keys.Clear()
}

func (s *Service) resetCatalog() {
	s.mu.Lock()
	s.catalog = nil
	s.mu.Unlock()
}

func (s *Service) resolve(ctx context.Context) (catalog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.catalog != nil {
		return *s.catalog, nil
	}
	games, err := s.Client.Games(ctx)
	if err != nil {
		return catalog{}, err
	}
	var cat catalog
	for _, g := range games {
		if strings.EqualFold(g.Slug, "hytale") || strings.EqualFold(g.Name, "hytale") {
			cat.gameID = g.ID
			break
		}
	}
	if cat.gameID == 0 {
		return catalog{}, ErrGameNotFound
	}
	classes, err := s.Client.Classes(ctx, cat.gameID)
	if err != nil {
		return catalog{}, err
	}
	for _, c := range classes {
		slug, name := strings.ToLower(c.Slug), strings.ToLower(c.Name)
		switch {
		case strings.Contains(slug, "modpack") || strings.Contains(name, "modpack"):
			cat.packsClassID = c.ID
		case slug == "mods" || name == "mods":
			cat.modsClassID = c.ID
		}
	}
	s.catalog = &cat
	return cat, nil
}

// ---------------------------------------------------------------- search

// FileSummary is the part of a file the interface displays.
type FileSummary struct {
	ID           int       `json:"id"`
	DisplayName  string    `json:"displayName"`
	FileName     string    `json:"fileName"`
	Date         time.Time `json:"date"`
	SizeBytes    int64     `json:"sizeBytes"`
	ReleaseType  string    `json:"releaseType"`
	GameVersions []string  `json:"gameVersions,omitempty"`
	Downloadable bool      `json:"downloadable"`
}

// ProjectSummary is a search result.
type ProjectSummary struct {
	ID        int          `json:"id"`
	Name      string       `json:"name"`
	Summary   string       `json:"summary"`
	Author    string       `json:"author,omitempty"`
	Downloads int64        `json:"downloads"`
	LogoURL   string       `json:"logoUrl,omitempty"`
	URL       string       `json:"url,omitempty"`
	Latest    *FileSummary `json:"latest,omitempty"`
}

// SearchQuery filters a search.
type SearchQuery struct {
	Text     string
	Modpacks bool
	Sort     string // "popularity" | "updated" | "downloads" | "name"
	Page     int
}

// SearchResult is one page of results.
type SearchResult struct {
	Items      []ProjectSummary `json:"items"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	TotalCount int              `json:"totalCount"`
}

const searchPageSize = 20

// Search looks for mods or modpacks.
func (s *Service) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	cat, err := s.resolve(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	params := SearchParams{GameID: cat.gameID, ClassID: cat.modsClassID, Query: strings.TrimSpace(q.Text), PageSize: searchPageSize}
	if q.Modpacks {
		if cat.packsClassID == 0 {
			return SearchResult{}, ErrNoModpacks
		}
		params.ClassID = cat.packsClassID
	}
	switch q.Sort {
	case "updated":
		params.SortField = SortLastUpdated
	case "downloads":
		params.SortField = SortTotalDownloads
	case "name":
		params.SortField = SortName
	default:
		params.SortField = SortPopularity
	}
	page := max(q.Page, 0)
	params.Index = min(page*searchPageSize, 10000-searchPageSize)

	mods, info, err := s.Client.Search(ctx, params)
	if err != nil {
		return SearchResult{}, err
	}
	res := SearchResult{Items: make([]ProjectSummary, 0, len(mods)), Page: page, PageSize: searchPageSize, TotalCount: min(info.TotalCount, 10000)}
	for _, m := range mods {
		res.Items = append(res.Items, summarize(m))
	}
	return res, nil
}

func summarize(m Mod) ProjectSummary {
	p := ProjectSummary{ID: m.ID, Name: m.Name, Summary: m.Summary, Downloads: int64(m.DownloadCount), URL: m.Links.WebsiteURL}
	if len(m.Authors) > 0 {
		p.Author = m.Authors[0].Name
	}
	if m.Logo != nil {
		p.LogoURL = m.Logo.ThumbnailURL
	}
	if f, ok := pickFile(m.LatestFiles); ok {
		fs := summarizeFile(f)
		p.Latest = &fs
	}
	return p
}

func summarizeFile(f File) FileSummary {
	rel := "release"
	switch f.ReleaseType {
	case ReleaseTypeBeta:
		rel = "beta"
	case ReleaseTypeAlpha:
		rel = "alpha"
	}
	return FileSummary{
		ID: f.ID, DisplayName: f.DisplayName, FileName: f.FileName, Date: f.FileDate, SizeBytes: f.FileLength,
		ReleaseType: rel, GameVersions: f.GameVersions, Downloadable: f.DownloadURL != "",
	}
}

// Files lists the files of a project for the version picker.
func (s *Service) Files(ctx context.Context, modID int) ([]FileSummary, error) {
	files, _, err := s.Client.ModFiles(ctx, modID, 0, 50)
	if err != nil {
		return nil, err
	}
	out := make([]FileSummary, 0, len(files))
	for _, f := range files {
		if f.IsAvailable {
			out = append(out, summarizeFile(f))
		}
	}
	return out, nil
}

// pickFile chooses the file to install: the newest stable one, or the newest
// of any kind when no stable file exists.
func pickFile(files []File) (File, bool) {
	var best File
	found := false
	better := func(a, b File) bool { // is a better than b?
		if (a.ReleaseType == ReleaseTypeRelease) != (b.ReleaseType == ReleaseTypeRelease) {
			return a.ReleaseType == ReleaseTypeRelease
		}
		return a.FileDate.After(b.FileDate)
	}
	for _, f := range files {
		if !f.IsAvailable || f.IsServerPack {
			continue
		}
		if !found || better(f, best) {
			best, found = f, true
		}
	}
	return best, found
}

func (s *Service) latestFile(ctx context.Context, m Mod) (File, error) {
	if f, ok := pickFile(m.LatestFiles); ok {
		return f, nil
	}
	files, _, err := s.Client.ModFiles(ctx, m.ID, 0, 50)
	if err != nil {
		return File{}, err
	}
	f, ok := pickFile(files)
	if !ok {
		return File{}, ErrNoFile
	}
	return f, nil
}

// ---------------------------------------------------------------- install

// InstallResult lists what an installation changed.
type InstallResult struct {
	Installed []string `json:"installed"`
	Warnings  []string `json:"warnings,omitempty"`
}

// InstallMod installs a mod (fileID 0 = best file) and, optionally, the
// dependencies it requires.
func (s *Service) InstallMod(ctx context.Context, saveID string, modID, fileID int, deps bool) (InstallResult, error) {
	mod, err := s.Client.Mod(ctx, modID)
	if err != nil {
		return InstallResult{}, err
	}
	file, err := s.chooseFile(ctx, mod, fileID)
	if err != nil {
		return InstallResult{}, err
	}
	var res InstallResult
	seen := map[int]bool{mod.ID: true}
	name, err := s.install(ctx, saveID, mod, file, "")
	if err != nil {
		return res, err
	}
	res.Installed = append(res.Installed, name)
	if deps {
		s.installDependencies(ctx, saveID, file, seen, 0, &res)
	}
	return res, nil
}

func (s *Service) chooseFile(ctx context.Context, mod Mod, fileID int) (File, error) {
	if fileID == 0 {
		return s.latestFile(ctx, mod)
	}
	f, err := s.Client.File(ctx, mod.ID, fileID)
	if err != nil {
		return File{}, err
	}
	if !f.IsAvailable {
		return File{}, ErrNoFile
	}
	return f, nil
}

const maxDependencyDepth = 4

func (s *Service) installDependencies(ctx context.Context, saveID string, file File, seen map[int]bool, depth int, res *InstallResult) {
	if depth >= maxDependencyDepth {
		return
	}
	links, err := s.Mods.Links(saveID)
	if err != nil {
		res.Warnings = append(res.Warnings, err.Error())
		return
	}
	for _, dep := range file.Dependencies {
		if dep.RelationType != RelationRequiredDependency || seen[dep.ModID] || hasProject(links, dep.ModID) {
			continue
		}
		seen[dep.ModID] = true
		mod, err := s.Client.Mod(ctx, dep.ModID)
		if err == nil {
			var f File
			if f, err = s.latestFile(ctx, mod); err == nil {
				var name string
				if name, err = s.install(ctx, saveID, mod, f, ""); err == nil {
					res.Installed = append(res.Installed, name)
					s.installDependencies(ctx, saveID, f, seen, depth+1, res)
					continue
				}
			}
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("dependency %d not installed: %v", dep.ModID, err))
	}
}

func hasProject(links map[string]saves.CurseForgeLink, projectID int) bool {
	for _, l := range links {
		if l.ProjectID == projectID {
			return true
		}
	}
	return false
}

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9 ._()+\-\[\]]`)

// modFileName turns a CurseForge file name into one accepted by the mod store.
func modFileName(name string) (string, error) {
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, ".zip") {
		return "", ErrNotAModArchive
	}
	ext := lower[len(lower)-4:]
	base := unsafeNameChars.ReplaceAllString(name[:len(name)-4], "_")
	base = strings.TrimLeft(base, " ._-")
	if base == "" {
		base = "mod"
	}
	if len(base) > 140 {
		base = base[:140]
	}
	return base + ext, nil
}

// install downloads file and stores it in the save, replacing the file
// previously installed from the same project (keeping its enabled state).
// It returns the stored file name.
func (s *Service) install(ctx context.Context, saveID string, mod Mod, file File, pack string) (string, error) {
	fileName, err := modFileName(file.FileName)
	if err != nil {
		return "", err
	}
	links, err := s.Mods.Links(saveID)
	if err != nil {
		return "", err
	}
	enabled, previous := true, ""
	for name, l := range links {
		if l.ProjectID == mod.ID {
			previous = name
			if en, found, err := s.Mods.State(saveID, name); err == nil && found {
				enabled = en
			}
			break
		}
	}
	if _, found, err := s.Mods.State(saveID, fileName); err != nil {
		return "", err
	} else if found {
		if l, ok := links[fileName]; !ok || l.ProjectID != mod.ID {
			return "", fmt.Errorf("%w : %s", saves.ErrModExists, fileName)
		}
	}

	tmp, err := s.download(ctx, mod, file)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp)
	f, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := s.Mods.Put(saveID, fileName, enabled, f, s.MaxFileBytes); err != nil {
		return "", err
	}
	if previous != "" && previous != fileName {
		if err := s.Mods.Remove(saveID, previous); err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, saves.ErrNotFound) {
			return "", err
		}
	}
	err = s.Mods.Link(saveID, fileName, saves.CurseForgeLink{
		ProjectID: mod.ID, FileID: file.ID, Name: mod.Name, Slug: mod.Slug, URL: mod.Links.WebsiteURL,
		FileDate: file.FileDate, InstalledAt: time.Now().UTC(), Pack: pack,
	})
	return fileName, err
}

// download fetches file into a temporary file and verifies its checksum.
func (s *Service) download(ctx context.Context, mod Mod, file File) (string, error) {
	url := file.DownloadURL
	if url == "" {
		var err error
		if url, err = s.Client.DownloadURL(ctx, mod.ID, file.ID); err != nil {
			return "", err
		}
	}
	if url == "" {
		return "", &DownloadDisabledError{Name: mod.Name, URL: mod.Links.WebsiteURL}
	}
	tmp, err := os.CreateTemp(s.TmpDir, "curseforge-*")
	if err != nil {
		return "", err
	}
	hash := sha1.New() //nolint:gosec
	_, err = s.Client.Download(ctx, url, io.MultiWriter(tmp, hash), s.MaxFileBytes)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		if want := file.SHA1(); want != "" && !strings.EqualFold(want, hex.EncodeToString(hash.Sum(nil))) {
			err = ErrChecksum
		}
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// ---------------------------------------------------------------- updates

// UpdateInfo describes an installed mod that has a newer file.
type UpdateInfo struct {
	FileName    string      `json:"fileName"`
	Name        string      `json:"name"`
	ProjectID   int         `json:"projectId"`
	CurrentDate time.Time   `json:"currentDate"`
	Latest      FileSummary `json:"latest"`
}

const batchSize = 50

// CheckUpdates compares the installed CurseForge mods with their latest files.
func (s *Service) CheckUpdates(ctx context.Context, saveID string) ([]UpdateInfo, error) {
	updates, _, err := s.findUpdates(ctx, saveID)
	return updates, err
}

func (s *Service) findUpdates(ctx context.Context, saveID string) ([]UpdateInfo, map[int]Mod, error) {
	links, err := s.Mods.Links(saveID)
	if err != nil {
		return nil, nil, err
	}
	installed, err := s.Mods.List(saveID)
	if err != nil {
		return nil, nil, err
	}
	var ids []int
	for _, m := range installed {
		if m.CurseForge != nil && !slices.Contains(ids, m.CurseForge.ProjectID) {
			ids = append(ids, m.CurseForge.ProjectID)
		}
	}
	projects := map[int]Mod{}
	for chunk := range slices.Chunk(ids, batchSize) {
		mods, err := s.Client.Mods(ctx, chunk)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range mods {
			projects[m.ID] = m
		}
	}
	updates := []UpdateInfo{}
	for _, m := range installed {
		link, ok := links[m.FileName]
		if !ok {
			continue
		}
		project, ok := projects[link.ProjectID]
		if !ok {
			continue
		}
		latest, ok := pickFile(project.LatestFiles)
		if !ok || latest.ID == link.FileID || !latest.FileDate.After(link.FileDate) {
			continue
		}
		updates = append(updates, UpdateInfo{
			FileName: m.FileName, Name: link.Name, ProjectID: link.ProjectID,
			CurrentDate: link.FileDate, Latest: summarizeFile(latest),
		})
	}
	return updates, projects, nil
}
