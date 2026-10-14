package hytaleauth

import (
	"context"
	"errors"
	"sync"
	"time"

	"hytale-server-manager/internal/hytaleapi"
)

// LoginState is the state of a device code login.
type LoginState string

// Login states.
const (
	LoginIdle      LoginState = "idle"
	LoginPending   LoginState = "pending"
	LoginSucceeded LoginState = "succeeded"
	LoginFailed    LoginState = "failed"
	LoginCancelled LoginState = "cancelled"
)

// LoginStatus is what the interface shows while a login is in progress.
type LoginStatus struct {
	State                   LoginState `json:"state"`
	UserCode                string     `json:"userCode,omitempty"`
	VerificationURI         string     `json:"verificationUri,omitempty"`
	VerificationURIComplete string     `json:"verificationUriComplete,omitempty"`
	ExpiresAt               time.Time  `json:"expiresAt,omitzero"`
	Error                   string     `json:"error,omitempty"`
}

type deviceAPI interface {
	RequestDeviceCode(ctx context.Context) (hytaleapi.DeviceCode, error)
	PollDeviceToken(ctx context.Context, deviceCode string) (hytaleapi.Token, error)
}

// DeviceLogin runs at most one RFC 8628 device code flow at a time.
type DeviceLogin struct {
	api       deviceAPI
	onSuccess func(context.Context, hytaleapi.Token) error

	mu     sync.Mutex
	status LoginStatus
	cancel context.CancelFunc
}

// NewDeviceLogin creates a flow runner calling onSuccess with the tokens.
func NewDeviceLogin(api deviceAPI, onSuccess func(context.Context, hytaleapi.Token) error) *DeviceLogin {
	return &DeviceLogin{api: api, onSuccess: onSuccess, status: LoginStatus{State: LoginIdle}}
}

// Start requests a device code and polls for approval in the background.
// When a flow is already pending its status is returned unchanged.
func (d *DeviceLogin) Start(ctx context.Context) (LoginStatus, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State == LoginPending {
		return d.status, nil
	}

	code, err := d.api.RequestDeviceCode(ctx)
	if err != nil {
		return d.status, err
	}
	expiresAt := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	pollCtx, cancel := context.WithDeadline(context.Background(), expiresAt)
	d.cancel = cancel
	d.status = LoginStatus{
		State:                   LoginPending,
		UserCode:                code.UserCode,
		VerificationURI:         code.VerificationURI,
		VerificationURIComplete: code.VerificationURIComplete,
		ExpiresAt:               expiresAt,
	}
	go d.poll(pollCtx, code)
	return d.status, nil
}

// Cancel aborts the pending flow, if any.
func (d *DeviceLogin) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State != LoginPending {
		return
	}
	d.cancel()
	d.status = LoginStatus{State: LoginCancelled}
}

// Status returns the current flow status.
func (d *DeviceLogin) Status() LoginStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status
}

// Reset forgets the outcome of the last flow.
func (d *DeviceLogin) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State != LoginPending {
		d.status = LoginStatus{State: LoginIdle}
	}
}

func (d *DeviceLogin) poll(ctx context.Context, code hytaleapi.DeviceCode) {
	interval := time.Duration(max(code.Interval, 5)) * time.Second
	for {
		select {
		case <-ctx.Done():
			d.finish(ctx, errors.New("le code a expiré, recommencez la connexion"))
			return
		case <-time.After(interval):
		}

		tok, err := d.api.PollDeviceToken(ctx, code.DeviceCode)
		switch {
		case errors.Is(err, hytaleapi.ErrAuthorizationPending):
			continue
		case errors.Is(err, hytaleapi.ErrSlowDown):
			interval += 5 * time.Second
			continue
		case err != nil:
			d.finish(ctx, err)
			return
		}
		d.finish(ctx, d.onSuccess(ctx, tok))
		return
	}
}

func (d *DeviceLogin) finish(ctx context.Context, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State != LoginPending {
		return // cancelled meanwhile
	}
	d.cancel()
	switch {
	case err == nil:
		d.status = LoginStatus{State: LoginSucceeded}
	case errors.Is(ctx.Err(), context.Canceled):
		d.status = LoginStatus{State: LoginCancelled}
	default:
		d.status = LoginStatus{State: LoginFailed, Error: err.Error()}
	}
}
