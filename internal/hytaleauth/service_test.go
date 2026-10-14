package hytaleauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"hytale-server-manager/internal/hytaleapi"
	"hytale-server-manager/internal/secretbox"
)

// fakeHytale imitates the OAuth, account and session endpoints.
type fakeHytale struct {
	refreshes    atomic.Int32
	validRefresh atomic.Value // string
}

func (f *fakeHytale) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != f.validRefresh.Load().(string) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		n := f.refreshes.Add(1)
		next := "refresh-" + string(rune('A'+n))
		f.validRefresh.Store(next)
		json.NewEncoder(w).Encode(hytaleapi.Token{AccessToken: "access-new", RefreshToken: next, ExpiresIn: 3600})
	})
	mux.HandleFunc("GET /profiles", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(hytaleapi.Profiles{Profiles: []hytaleapi.Profile{{UUID: "uuid-1", Username: "Owner"}}})
	})
	mux.HandleFunc("POST /session/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-new" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(hytaleapi.GameSession{SessionToken: "sess", IdentityToken: "id", ExpiresAt: time.Now().Add(time.Hour)})
	})
	return mux
}

func newTestService(t *testing.T) (*Service, *fakeHytale, *TokenStore) {
	t.Helper()
	fake := &fakeHytale{}
	fake.validRefresh.Store("refresh-0")
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	box, err := secretbox.LoadOrCreate(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewTokenStore(filepath.Join(t.TempDir(), "auth.enc"), box)
	api := hytaleapi.NewClient(srv.Client(), hytaleapi.Endpoints{
		Token: srv.URL + "/token", Profiles: srv.URL + "/profiles",
		GameSessionNew: srv.URL + "/session/new", GameSession: srv.URL + "/session",
	})
	svc, err := NewService(api, store)
	if err != nil {
		t.Fatal(err)
	}
	return svc, fake, store
}

func TestLoginRefreshAndGameSession(t *testing.T) {
	svc, fake, store := newTestService(t)
	ctx := context.Background()

	// Device flow result with an already expired access token.
	if err := svc.completeLogin(ctx, hytaleapi.Token{AccessToken: "access-new", RefreshToken: "refresh-0", ExpiresIn: 0}); err != nil {
		t.Fatal(err)
	}
	if st := svc.Status(); !st.Authenticated || st.ProfileUUID != "uuid-1" {
		t.Fatalf("single profile should be auto-selected: %+v", st)
	}

	grant, err := svc.NewGameSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if grant.SessionToken != "sess" || grant.OwnerUUID != "uuid-1" {
		t.Errorf("unexpected grant %+v", grant)
	}
	if fake.refreshes.Load() != 1 {
		t.Errorf("expired access token should trigger exactly one refresh, got %d", fake.refreshes.Load())
	}

	// The rotated refresh token must be persisted (the old one is invalid).
	creds, found, err := store.Load()
	if err != nil || !found || creds.RefreshToken != fake.validRefresh.Load().(string) {
		t.Fatalf("rotated token not persisted: %+v %v", creds, err)
	}
}

func TestRevokedRefreshTokenRequiresLogin(t *testing.T) {
	svc, fake, store := newTestService(t)
	ctx := context.Background()
	if err := svc.completeLogin(ctx, hytaleapi.Token{AccessToken: "x", RefreshToken: "refresh-0"}); err != nil {
		t.Fatal(err)
	}
	fake.validRefresh.Store("someone-else")

	if _, err := svc.NewGameSession(ctx); !errors.Is(err, ErrReauthRequired) {
		t.Fatalf("got %v, want ErrReauthRequired", err)
	}
	if svc.Authenticated() {
		t.Error("credentials should be cleared")
	}
	if _, found, _ := store.Load(); found {
		t.Error("credentials file should be deleted")
	}
}
