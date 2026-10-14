package hytaleapi

import (
	"context"
	"net/url"
	"time"
)

// DeviceCode is the answer to a device authorization request.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// Token is an OAuth token response.
type Token struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// ExpiresAt converts ExpiresIn into an absolute time relative to issuedAt.
func (t Token) ExpiresAt(issuedAt time.Time) time.Time {
	return issuedAt.Add(time.Duration(t.ExpiresIn) * time.Second)
}

// RequestDeviceCode starts the device authorization flow (step 1).
func (c *Client) RequestDeviceCode(ctx context.Context) (DeviceCode, error) {
	form := url.Values{"client_id": {ClientID}, "scope": {Scope}}
	var code DeviceCode
	err := c.postForm(ctx, c.endpoints.DeviceAuth, form, &code)
	return code, err
}

// PollDeviceToken exchanges a device code for tokens (step 3). It returns
// ErrAuthorizationPending while the user has not approved the request yet.
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (Token, error) {
	form := url.Values{
		"client_id":   {ClientID},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
	}
	var tok Token
	err := c.postForm(ctx, c.endpoints.Token, form, &tok)
	return tok, err
}

// RefreshToken rotates the OAuth tokens. The returned refresh token replaces
// (and invalidates) the one passed in.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{
		"client_id":     {ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	var tok Token
	err := c.postForm(ctx, c.endpoints.Token, form, &tok)
	return tok, err
}
