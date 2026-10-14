// Package curseforge talks to the CurseForge API to search, install and
// update Hytale mods and modpacks.
package curseforge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production CurseForge API.
const DefaultBaseURL = "https://api.curseforge.com"

const (
	maxResponseBytes = 8 << 20
	// apiTimeout bounds one API call; the HTTP client itself has no overall
	// timeout so that large downloads can complete.
	apiTimeout = 30 * time.Second
)

// Errors returned by the client.
var (
	ErrNoAPIKey      = errors.New("aucune clé API CurseForge n'est configurée (Paramètres)")
	ErrInvalidAPIKey = errors.New("clé API CurseForge refusée")
)

// APIError is a non-successful CurseForge response.
type APIError struct {
	Status int
}

func (e *APIError) Error() string { return fmt.Sprintf("CurseForge a répondu HTTP %d", e.Status) }

// Release types of a file.
const (
	ReleaseTypeRelease = 1
	ReleaseTypeBeta    = 2
	ReleaseTypeAlpha   = 3
)

// RelationRequiredDependency marks a dependency that must be installed.
const RelationRequiredDependency = 3

// Search sort fields (ModsSearchSortField).
const (
	SortFeatured       = 1
	SortPopularity     = 2
	SortLastUpdated    = 3
	SortName           = 4
	SortTotalDownloads = 6
)

// Game is a CurseForge game.
type Game struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Category is a category or, when IsClass is set, a class of projects
// (such as "Mods" or "Modpacks").
type Category struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	IsClass bool   `json:"isClass"`
	ClassID int    `json:"classId"`
}

// Hash is a file checksum.
type Hash struct {
	Value string `json:"value"`
	Algo  int    `json:"algo"` // 1 = SHA-1, 2 = MD5
}

// Dependency links a file to another project.
type Dependency struct {
	ModID        int `json:"modId"`
	RelationType int `json:"relationType"`
}

// File is a downloadable file of a project.
type File struct {
	ID               int          `json:"id"`
	ModID            int          `json:"modId"`
	DisplayName      string       `json:"displayName"`
	FileName         string       `json:"fileName"`
	FileDate         time.Time    `json:"fileDate"`
	FileLength       int64        `json:"fileLength"`
	ReleaseType      int          `json:"releaseType"`
	DownloadURL      string       `json:"downloadUrl"` // empty when the author disallows third-party downloads
	GameVersions     []string     `json:"gameVersions"`
	IsAvailable      bool         `json:"isAvailable"`
	IsServerPack     bool         `json:"isServerPack"`
	ServerPackFileID int          `json:"serverPackFileId"`
	Hashes           []Hash       `json:"hashes"`
	Dependencies     []Dependency `json:"dependencies"`
}

// SHA1 returns the SHA-1 checksum of the file, if the API reported one.
func (f File) SHA1() string {
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			return h.Value
		}
	}
	return ""
}

// Mod is a CurseForge project (mod or modpack).
type Mod struct {
	ID            int      `json:"id"`
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	Summary       string   `json:"summary"`
	DownloadCount float64  `json:"downloadCount"`
	ClassID       int      `json:"classId"`
	DateModified  string   `json:"dateModified"`
	LatestFiles   []File   `json:"latestFiles"`
	Logo          *Logo    `json:"logo"`
	Links         Links    `json:"links"`
	Authors       []Author `json:"authors"`
}

// Logo is a project image.
type Logo struct {
	ThumbnailURL string `json:"thumbnailUrl"`
}

// Links are the public URLs of a project.
type Links struct {
	WebsiteURL string `json:"websiteUrl"`
}

// Author is a project author.
type Author struct {
	Name string `json:"name"`
}

// SearchParams filters a project search.
type SearchParams struct {
	GameID    int
	ClassID   int
	Query     string
	SortField int
	Index     int
	PageSize  int
}

// Page describes a slice of a result set.
type Page struct {
	Index       int `json:"index"`
	PageSize    int `json:"pageSize"`
	ResultCount int `json:"resultCount"`
	TotalCount  int `json:"totalCount"`
}

// Client is a minimal CurseForge API client. The API key is read for every
// request so that it can be changed at runtime.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  func() string
}

// NewClient returns a client using httpClient. baseURL may be empty.
func NewClient(httpClient *http.Client, baseURL string, apiKey func() string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{http: httpClient, baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey}
}

type envelope[T any] struct {
	Data       T     `json:"data"`
	Pagination *Page `json:"pagination"`
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) (*Page, error) {
	key := c.apiKey()
	if key == "" {
		return nil, ErrNoAPIKey
	}
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CurseForge injoignable : %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrInvalidAPIKey
	case resp.StatusCode != http.StatusOK:
		return nil, &APIError{Status: resp.StatusCode}
	}
	var env envelope[json.RawMessage]
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&env); err != nil {
		return nil, fmt.Errorf("réponse CurseForge illisible : %w", err)
	}
	if out != nil {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return nil, fmt.Errorf("réponse CurseForge inattendue : %w", err)
		}
	}
	return env.Pagination, nil
}

// Games lists the games known to CurseForge.
func (c *Client) Games(ctx context.Context) ([]Game, error) {
	var games []Game
	q := url.Values{"pageSize": {"50"}}
	for index := 0; index < 1000; index += 50 {
		q.Set("index", strconv.Itoa(index))
		var batch []Game
		page, err := c.do(ctx, http.MethodGet, "/v1/games", q, nil, &batch)
		if err != nil {
			return nil, err
		}
		games = append(games, batch...)
		if page == nil || index+len(batch) >= page.TotalCount || len(batch) == 0 {
			break
		}
	}
	return games, nil
}

// Classes lists the project classes (Mods, Modpacks…) of a game.
func (c *Client) Classes(ctx context.Context, gameID int) ([]Category, error) {
	var classes []Category
	q := url.Values{"gameId": {strconv.Itoa(gameID)}, "classesOnly": {"true"}}
	_, err := c.do(ctx, http.MethodGet, "/v1/categories", q, nil, &classes)
	return classes, err
}

// Search looks for projects.
func (c *Client) Search(ctx context.Context, p SearchParams) ([]Mod, Page, error) {
	q := url.Values{
		"gameId":    {strconv.Itoa(p.GameID)},
		"sortField": {strconv.Itoa(p.SortField)},
		"sortOrder": {"desc"},
		"index":     {strconv.Itoa(p.Index)},
		"pageSize":  {strconv.Itoa(p.PageSize)},
	}
	if p.ClassID != 0 {
		q.Set("classId", strconv.Itoa(p.ClassID))
	}
	if p.Query != "" {
		q.Set("searchFilter", p.Query)
		if p.SortField == SortName {
			q.Set("sortOrder", "asc")
		}
	}
	var mods []Mod
	page, err := c.do(ctx, http.MethodGet, "/v1/mods/search", q, nil, &mods)
	if page == nil {
		page = &Page{}
	}
	return mods, *page, err
}

// Mod returns one project.
func (c *Client) Mod(ctx context.Context, id int) (Mod, error) {
	var mod Mod
	_, err := c.do(ctx, http.MethodGet, "/v1/mods/"+strconv.Itoa(id), nil, nil, &mod)
	return mod, err
}

// Mods returns several projects at once.
func (c *Client) Mods(ctx context.Context, ids []int) ([]Mod, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var mods []Mod
	_, err := c.do(ctx, http.MethodPost, "/v1/mods", nil, map[string]any{"modIds": ids}, &mods)
	return mods, err
}

// ModFiles lists the files of a project, newest first.
func (c *Client) ModFiles(ctx context.Context, modID, index, pageSize int) ([]File, Page, error) {
	q := url.Values{"index": {strconv.Itoa(index)}, "pageSize": {strconv.Itoa(pageSize)}}
	var files []File
	page, err := c.do(ctx, http.MethodGet, "/v1/mods/"+strconv.Itoa(modID)+"/files", q, nil, &files)
	if page == nil {
		page = &Page{}
	}
	return files, *page, err
}

// File returns one file of a project.
func (c *Client) File(ctx context.Context, modID, fileID int) (File, error) {
	var f File
	_, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/mods/%d/files/%d", modID, fileID), nil, nil, &f)
	return f, err
}

// Files returns several files by id.
func (c *Client) Files(ctx context.Context, ids []int) ([]File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var files []File
	_, err := c.do(ctx, http.MethodPost, "/v1/mods/files", nil, map[string]any{"fileIds": ids}, &files)
	return files, err
}

// DownloadURL asks the API for the download URL of a file whose listing
// did not include one. It returns "" when downloads are not allowed.
func (c *Client) DownloadURL(ctx context.Context, modID, fileID int) (string, error) {
	var u *string
	_, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/mods/%d/files/%d/download-url", modID, fileID), nil, nil, &u)
	// The API answers 403 when the author disallows third-party downloads;
	// the key was just accepted by the calls that led here.
	if errors.Is(err, ErrInvalidAPIKey) {
		return "", nil
	}
	if err != nil || u == nil {
		return "", err
	}
	return *u, nil
}

// Download streams the content of a file URL into dst, refusing more than
// maxBytes. File URLs are public CDN links: the API key is not sent.
func (c *Client) Download(ctx context.Context, fileURL string, dst io.Writer, maxBytes int64) (int64, error) {
	u, err := url.Parse(fileURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return 0, errors.New("URL de téléchargement CurseForge invalide")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("téléchargement impossible : %w", errors.Unwrap(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, &APIError{Status: resp.StatusCode}
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return n, err
	}
	if n > maxBytes {
		return n, errors.New("le fichier dépasse la taille autorisée")
	}
	return n, nil
}
