package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"truckapi/internal/types"
	"truckapi/pkg/config"

	log "github.com/sirupsen/logrus"
)

// Stores the OAuth token and its expiration time (which we track)
type TokenStore struct {
	sync.Mutex
	token     string
	expiresAt time.Time
}

const fallbackTokenTTL = 24 * time.Hour

// Function used to create a new instance of the TokenStore
func NewTokenStore() *TokenStore {
	// Retrieve the existing token from the environment variable
	token := strings.TrimSpace(config.GetEnv(config.CHRobAccessToken, ""))
	// Declare a variable to hold the expiration time of the token
	var expiresAt time.Time
	// Check if the token was retrieved from the environment variable
	if token != "" {
		if parsedExp, err := tokenExpiryFromJWT(token); err == nil {
			if time.Now().UTC().Before(parsedExp.UTC()) {
				expiresAt = parsedExp.UTC()
				log.WithField("token_expiry", expiresAt.Format(time.RFC3339)).Info("Loaded CHRob access token from environment")
			} else {
				// Expired token should not be treated as usable; force refresh on first API call.
				token = ""
				log.WithField("token_expiry", parsedExp.Format(time.RFC3339)).Warn("CHRob access token in environment is expired; will refresh")
			}
		} else {
			// Non-JWT or malformed token should be refreshed immediately.
			token = ""
			log.WithError(err).Warn("CHRob access token in environment is invalid; will refresh")
		}
	}
	// Returns a new instance of TokenStore with the token and expiration time
	return &TokenStore{
		token:     token,
		expiresAt: expiresAt,
	}
}

// GenerateToken retrieves an OAuth token from C.H. Robinson's authentication endpoint.
func GenerateToken() (*types.TokenResponse, error) {
	auth := types.Auth{
		ClientID:     config.GetEnv(config.CHRobClientID, ""),
		ClientSecret: config.GetEnv(config.CHRobClientSecret, ""),
		Audience:     config.GetEnv(config.CHRobAudience, ""),
		GrantType:    config.GetEnv(config.CHRobGrantType, ""),
	}
	tokenURL := config.GetEnv(config.CHRobTokenUrl, "")
	if strings.TrimSpace(auth.GrantType) == "" {
		auth.GrantType = "client_credentials"
	}

	log.WithFields(log.Fields{
		"client_id":  auth.ClientID,
		"token_url":  tokenURL,
		"audience":   auth.Audience,
		"grant_type": auth.GrantType,
	}).Info("Requesting CHRob token")

	timeout := 20 * time.Second
	if v := config.GetEnv("CHROB_TOKEN_TIMEOUT_SECONDS", ""); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	requestBody, err := json.Marshal(auth)
	if err != nil {
		log.WithError(err).Error("Failed to marshal CHRob token request body")
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithError(err).Error("Failed to create CHRob token request")
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		log.WithError(err).Error("Failed to request CHRob token")
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithError(err).Error("Failed to read CHRob token response body")
		return nil, err
	}

	tokenResponse := &types.TokenResponse{}
	if err := json.Unmarshal(rawBody, tokenResponse); err != nil {
		log.WithError(err).WithField("response_body", string(rawBody)).Error("Failed to parse CHRob token response")
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{
			"status_code":        resp.StatusCode,
			"error":              tokenResponse.Error,
			"error_description":  tokenResponse.ErrorDescription,
			"response_body":      string(rawBody),
			"response_body_size": len(rawBody),
		}).Error("CHRob token request returned non-200 status")
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(rawBody)))
	}

	if strings.TrimSpace(tokenResponse.AccessToken) == "" {
		return nil, fmt.Errorf("token request succeeded but access_token is empty")
	}

	log.WithFields(log.Fields{
		"token_type": tokenResponse.TokenType,
		"expires_in": tokenResponse.ExpiresIn,
		"scope":      tokenResponse.Scope,
	}).Info("CHRob token retrieved")

	return tokenResponse, nil
}

// SetToken sets the token and its expiration time.
func (store *TokenStore) SetToken(token string, expiresIn time.Duration) {
	// Acquire the mutex lock to ensure thread-safety for access to the tokenStore
	store.Lock()
	// defers the Unlock statement so it is executed last
	defer store.Unlock()
	// Update the token field to the new token
	store.token = token
	if expiresIn <= 0 {
		expiresIn = fallbackTokenTTL
		log.WithField("expires_in_seconds", int(expiresIn.Seconds())).Warn("Token expiry not provided by CHRob token endpoint; using 24h fallback")
	}
	// Set the expiration time based on token response TTL
	store.expiresAt = time.Now().UTC().Add(expiresIn)

	// Save the token to the environment variable
	config.SetEnv(config.CHRobAccessToken, token)
	if err := config.SaveEnv(".env"); err != nil {
		log.Errorf("Failed to save updated environment variables to .env file: %v", err)
	}

	log.WithFields(log.Fields{
		"expires_in_seconds": int(expiresIn.Seconds()),
		"token_length":       len(token),
	}).Info("Token set and saved")
}

func tokenExpiryFromJWT(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("token is not a JWT")
	}
	claimsPart := strings.TrimSpace(parts[1])
	if claimsPart == "" {
		return time.Time{}, fmt.Errorf("token payload is empty")
	}
	payload, err := base64.RawURLEncoding.DecodeString(claimsPart)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to decode JWT payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse JWT payload: %w", err)
	}
	if claims.Exp <= 0 {
		return time.Time{}, fmt.Errorf("token payload missing exp")
	}
	return time.Unix(claims.Exp, 0).UTC(), nil
}

// GetToken returns the current token if it's not expired.
func (store *TokenStore) GetToken() (string, bool) {
	// Acquire mutex lock
	store.Lock()
	// defer the unlock to execute last
	defer store.Unlock()
	// If the current time is earlier than the expiration time then we use the current token
	if time.Now().UTC().Before(store.expiresAt.UTC()) {
		log.Debug("Using existing token") // Avoid spamming logs at info level
		return store.token, true          // Return the token and true bool indicating the token is valid
	}
	return "", false // Return an empty string and a false bool if token is expired.
}

func (store *TokenStore) RefreshToken() error {
	log.Info("Refreshing token")
	tokenResponse, err := GenerateToken()
	if err != nil {
		log.WithError(err).Error("Failed to generate new token")
		return err
	}
	store.SetToken(tokenResponse.AccessToken, time.Duration(tokenResponse.ExpiresIn)*time.Second)
	log.Info("Token refreshed successfully")
	return nil
}

// IsTokenExpired checks if the current token is expired.
func (store *TokenStore) IsTokenExpired() bool {
	// Acquire mutex lock
	store.Lock()
	// defer the unlock to execute last
	defer store.Unlock()
	// Return true if the current time is after the token's expiration time
	return time.Now().UTC().After(store.expiresAt)
}

// GetValidToken retrieves a valid token, refreshing it if expired.
func (store *TokenStore) GetValidToken() (string, error) {
	token, valid := store.GetToken()
	if !valid {
		if err := store.RefreshToken(); err != nil {
			return "", err
		}
		token, _ = store.GetToken() // Get the new token
	}
	return token, nil
}
