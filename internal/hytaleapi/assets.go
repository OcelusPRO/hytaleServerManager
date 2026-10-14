package hytaleapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Manifest describes the latest game build of a patchline.
type Manifest struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
}

var (
	patchlinePattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)
	errBadAssetPath  = errors.New("invalid game asset path")
)

// SignedAssetURL exchanges a game asset path for a short-lived signed URL.
// This is the mechanism used by the server's built-in updater, authorised
// by the OAuth access token of the hytale-server client.
func (c *Client) SignedAssetURL(ctx context.Context, accessToken, assetPath string) (string, error) {
	if assetPath == "" || strings.HasPrefix(assetPath, "/") || strings.Contains(assetPath, "..") {
		return "", errBadAssetPath
	}
	endpoint, err := url.JoinPath(c.endpoints.GameAssets, assetPath)
	if err != nil {
		return "", err
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := c.sendJSON(ctx, http.MethodGet, endpoint, accessToken, nil, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.URL, "https://") {
		return "", fmt.Errorf("unexpected signed URL for %s", assetPath)
	}
	return out.URL, nil
}

// GameManifest returns the latest build description of a patchline.
func (c *Client) GameManifest(ctx context.Context, accessToken, patchline string) (Manifest, error) {
	if !patchlinePattern.MatchString(patchline) {
		return Manifest{}, fmt.Errorf("invalid patchline %q", patchline)
	}
	signed, err := c.SignedAssetURL(ctx, accessToken, "version/"+patchline+".json")
	if err != nil {
		return Manifest{}, fmt.Errorf("sign manifest URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signed, nil)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := c.do(req, &m); err != nil {
		return Manifest{}, fmt.Errorf("fetch manifest: %w", err)
	}
	if m.Version == "" || m.DownloadURL == "" || m.SHA256 == "" {
		return Manifest{}, errors.New("incomplete game manifest")
	}
	return m, nil
}
