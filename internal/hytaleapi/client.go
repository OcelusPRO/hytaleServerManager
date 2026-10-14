// Package hytaleapi is a thin HTTP client for the Hytale OAuth, account and
// game-session endpoints documented in the Server Provider Authentication Guide.
package hytaleapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ClientID and Scope identify the pre-configured hytale-server OAuth client.
const (
	ClientID = "hytale-server"
	Scope    = "openid offline auth:server"
)

const maxResponseBytes = 1 << 20

// Endpoints lists the URLs used by the client.
type Endpoints struct {
	DeviceAuth     string
	Token          string
	Profiles       string
	GameSessionNew string
	GameSession    string
	GameAssets     string
}

// DefaultEndpoints returns the production Hytale endpoints.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		DeviceAuth:     "https://oauth.accounts.hytale.com/oauth2/device/auth",
		Token:          "https://oauth.accounts.hytale.com/oauth2/token",
		Profiles:       "https://account-data.hytale.com/my-account/get-profiles",
		GameSessionNew: "https://sessions.hytale.com/game-session/new",
		GameSession:    "https://sessions.hytale.com/game-session",
		GameAssets:     "https://account-data.hytale.com/game-assets/",
	}
}

// Client talks to the Hytale services.
type Client struct {
	http      *http.Client
	endpoints Endpoints
}

// NewClient builds a client using httpClient for transport.
func NewClient(httpClient *http.Client, endpoints Endpoints) *Client {
	return &Client{http: httpClient, endpoints: endpoints}
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, out)
}

func (c *Client) sendJSON(ctx context.Context, method, endpoint, bearer string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", req.Method, req.URL.Host, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(resp.StatusCode, body)
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", req.URL.Host, err)
	}
	return nil
}
