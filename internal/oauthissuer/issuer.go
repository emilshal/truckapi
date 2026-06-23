// Package oauthissuer implements an OAuth 2.0 client_credentials token issuer
// used by CHRob to authenticate callbacks to our truckapi.
//
// CHRob's integration tooling can't register callbacks that use a static
// shared bearer; it requires fetching a token via client_credentials. This
// package issues short-lived JWTs that CHRob then sends as `Authorization:
// Bearer <jwt>` on offer-response and shipment-details callbacks.
package oauthissuer

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"truckapi/pkg/config"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultTokenTTL = time.Hour
	tokenIssuer     = "truckapi-callback-issuer"
	tokenAudience   = "truckapi-callback"
)

// TokenResponse is the OAuth 2.0 token endpoint response body.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// ErrInvalidClient is returned when the supplied client credentials don't match
// what's configured in the environment.
var ErrInvalidClient = errors.New("invalid client credentials")

// ErrNotConfigured is returned when the issuer's signing key or expected client
// credentials aren't set in the environment.
var ErrNotConfigured = errors.New("oauth callback issuer not configured")

func tokenTTL() time.Duration {
	raw := strings.TrimSpace(config.GetEnv(config.CallbackOAuthTokenTTLSeconds, ""))
	if raw == "" {
		return defaultTokenTTL
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultTokenTTL
	}
	return time.Duration(n) * time.Second
}

func signingKey() ([]byte, error) {
	secret := strings.TrimSpace(config.GetEnv(config.CallbackOAuthJWTSecret, ""))
	if secret == "" {
		return nil, ErrNotConfigured
	}
	return []byte(secret), nil
}

// JWTSigningConfigured reports whether the signing key is present, i.e. whether
// the JWT validation/issuance paths are usable. Cheap; safe to call per request.
func JWTSigningConfigured() bool {
	return strings.TrimSpace(config.GetEnv(config.CallbackOAuthJWTSecret, "")) != ""
}

// IssueToken validates client credentials and returns a signed JWT response.
func IssueToken(clientID, clientSecret string) (*TokenResponse, error) {
	expectedID := strings.TrimSpace(config.GetEnv(config.CallbackOAuthClientID, ""))
	expectedSecret := strings.TrimSpace(config.GetEnv(config.CallbackOAuthClientSecret, ""))
	if expectedID == "" || expectedSecret == "" {
		return nil, ErrNotConfigured
	}

	// Constant-time compare to avoid timing oracle.
	idOK := subtle.ConstantTimeCompare([]byte(clientID), []byte(expectedID)) == 1
	secretOK := subtle.ConstantTimeCompare([]byte(clientSecret), []byte(expectedSecret)) == 1
	if !idOK || !secretOK {
		return nil, ErrInvalidClient
	}

	key, err := signingKey()
	if err != nil {
		return nil, err
	}

	ttl := tokenTTL()
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Subject:   clientID,
		Audience:  jwt.ClaimStrings{tokenAudience},
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}

	return &TokenResponse{
		AccessToken: signed,
		TokenType:   "Bearer",
		ExpiresIn:   int(ttl.Seconds()),
	}, nil
}

// ValidateToken verifies the JWT signature, issuer, audience, and expiry.
// Returns nil on success.
func ValidateToken(tokenString string) error {
	if strings.TrimSpace(tokenString) == "" {
		return errors.New("empty token")
	}
	key, err := signingKey()
	if err != nil {
		return err
	}

	parsed, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Method.Alg())
		}
		return key, nil
	})
	if err != nil {
		return fmt.Errorf("parse token: %w", err)
	}
	if !parsed.Valid {
		return errors.New("invalid token")
	}

	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok {
		return errors.New("unexpected claims type")
	}
	if claims.Issuer != tokenIssuer {
		return errors.New("invalid token issuer")
	}
	audOK := false
	for _, a := range claims.Audience {
		if a == tokenAudience {
			audOK = true
			break
		}
	}
	if !audOK {
		return errors.New("invalid token audience")
	}
	return nil
}
