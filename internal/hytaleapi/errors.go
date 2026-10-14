package hytaleapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// OAuth device-flow errors (RFC 8628 §3.5).
var (
	ErrAuthorizationPending = errors.New("authorization pending")
	ErrSlowDown             = errors.New("slow down")
	ErrAccessDenied         = errors.New("access denied by user")
	ErrExpiredToken         = errors.New("device code expired")
)

// APIError describes a non-2xx answer from a Hytale endpoint.
type APIError struct {
	Status      int
	Code        string
	Description string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("hytale api: HTTP %d", e.Status)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}

// Unauthorized reports whether the credentials were rejected.
func (e *APIError) Unauthorized() bool {
	return e.Status == http.StatusUnauthorized || e.Code == "invalid_grant"
}

func newAPIError(status int, body []byte) error {
	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		Message          string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)

	switch payload.Error {
	case "authorization_pending":
		return ErrAuthorizationPending
	case "slow_down":
		return ErrSlowDown
	case "access_denied":
		return ErrAccessDenied
	case "expired_token":
		return ErrExpiredToken
	}
	desc := payload.ErrorDescription
	if desc == "" {
		desc = payload.Message
	}
	return &APIError{Status: status, Code: payload.Error, Description: desc}
}

// IsUnauthorized reports whether err means the stored credentials are invalid.
func IsUnauthorized(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Unauthorized()
}
