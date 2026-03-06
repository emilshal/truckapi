package auth

import (
	"encoding/base64"
	"fmt"
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
