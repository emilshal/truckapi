package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"truckapi/pkg/config"
)

func makeTestJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix())))
	return header + "." + payload + ".sig"
}

func TestNewTokenStore_UsesValidJWTExpiry(t *testing.T) {
	token := makeTestJWT(time.Now().UTC().Add(2 * time.Hour))
	t.Setenv(config.CHRobAccessToken, token)

	store := NewTokenStore()
	got, valid := store.GetToken()
	if !valid {
		t.Fatalf("expected token to be valid")
	}
	if got != token {
		t.Fatalf("expected token from env to be used")
	}
}

func TestNewTokenStore_ExpiredJWTForcesRefresh(t *testing.T) {
	token := makeTestJWT(time.Now().UTC().Add(-2 * time.Hour))
	t.Setenv(config.CHRobAccessToken, token)

	store := NewTokenStore()
	if got, valid := store.GetToken(); valid || got != "" {
		t.Fatalf("expected expired token to be invalid and cleared")
	}
}

func TestNewTokenStore_InvalidJWTForcesRefresh(t *testing.T) {
	t.Setenv(config.CHRobAccessToken, "not-a-jwt")

	store := NewTokenStore()
	if got, valid := store.GetToken(); valid || got != "" {
		t.Fatalf("expected invalid token to be rejected and cleared")
	}
}

// When the token endpoint keeps failing, RefreshToken must not hammer it: the
// first failure arms a backoff window during which further attempts are
// suppressed. This is the guard against the 429 storm a bad-credential state
// caused (every per-cycle search re-requesting a token).
func TestRefreshToken_BacksOffAfterFailure(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	}))
	defer srv.Close()

	t.Setenv(config.CHRobTokenUrl, srv.URL)
	t.Setenv(config.CHRobClientID, "test-client")
	t.Setenv(config.CHRobClientSecret, "test-secret")
	t.Setenv("CHROB_TOKEN_REFRESH_BACKOFF_SECONDS", "60")

	store := &TokenStore{}

	// First attempt reaches the endpoint and fails.
	if err := store.RefreshToken(); err == nil {
		t.Fatalf("expected first refresh to fail")
	}
	// Subsequent attempts within the window must be suppressed, not sent.
	for i := 0; i < 5; i++ {
		if err := store.RefreshToken(); err != errTokenRefreshBackoff {
			t.Fatalf("attempt %d: expected backoff suppression, got %v", i, err)
		}
	}

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected token endpoint to be hit exactly once, got %d", got)
	}
}

// A backoff of 0 disables suppression so each attempt is sent (useful to
// confirm the guard is opt-outable and doesn't wedge a recovered endpoint).
func TestRefreshToken_ZeroBackoffDisablesSuppression(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	t.Setenv(config.CHRobTokenUrl, srv.URL)
	t.Setenv(config.CHRobClientID, "test-client")
	t.Setenv(config.CHRobClientSecret, "test-secret")
	t.Setenv("CHROB_TOKEN_REFRESH_BACKOFF_SECONDS", "0")

	store := &TokenStore{}
	for i := 0; i < 3; i++ {
		_ = store.RefreshToken()
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("expected 3 hits with backoff disabled, got %d", got)
	}
}
