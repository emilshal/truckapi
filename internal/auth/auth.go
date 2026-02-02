package auth

import (
	"context"
	"sync"
	"time"
	"truckapi/internal/types"
	"truckapi/pkg/config"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2/clientcredentials"
)

// Stores the OAuth token and its expiration time (which we track)
type TokenStore struct {
	sync.Mutex
	token     string
	expiresAt time.Time
}

// Function used to create a new instance of the TokenStore
func NewTokenStore() *TokenStore {
	// Retrieve the existing token from the environment variable
	token := config.GetEnv(config.CHRobAccessToken, "")
	// Declare a variable to hold the expiration time of the token
	var expiresAt time.Time
	// Check if the token was retrieved from the environment variable
	if token != "" {
		// Set expiresAt to a future time, e.g., 24 hours from now
		// This is just an example; adjust the duration based on your token's actual expiration
		// Using UTC because our timezone is different from CHRobinson
		expiresAt = time.Now().UTC().Add(24 * time.Hour)
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

	// Configure the OAuth 2.0 client
	clientConfig := clientcredentials.Config{
		ClientID:     auth.ClientID,
		ClientSecret: auth.ClientSecret,
		TokenURL:     tokenURL,
	}

	log.Infof("Requesting token with ClientID: %s, TokenURL: %s", auth.ClientID, tokenURL)

	// Obtain a token source from the OAuth 2.0 package (which automatically refreshes the token when it expires)
	tokenSource := clientConfig.TokenSource(context.Background())

	// Get an access token from the Token Source
	token, err := tokenSource.Token()
	if err != nil {
		log.WithError(err).Error("Failed to obtain token from token source")
		return nil, err
	}

	// Convert the *oauth2.Token to a *types.TokenResponse.
	// tokenResponse is based on the status 200 response from the CHRobinson authentication endpoint.
	tokenResponse := &types.TokenResponse{
		AccessToken: token.AccessToken,
		ExpiresIn:   int(token.Expiry.Sub(time.Now()).Seconds()),
		TokenType:   token.TokenType,
	}

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
	// Set the expiration time to the one that was created in GenerateToken
	store.expiresAt = time.Now().UTC().Add(expiresIn)

	// Save the token to the environment variable
	config.SetEnv(config.CHRobAccessToken, token)
	if err := config.SaveEnv(".env"); err != nil {
		log.Errorf("Failed to save updated environment variables to .env file: %v", err)
	}

	log.Infof("Token set and saved: %s", token)
}

// GetToken returns the current token if it's not expired.
func (store *TokenStore) GetToken() (string, bool) {
	// Acquire mutex lock
	store.Lock()
	// defer the unlock to execute last
	defer store.Unlock()
	// If the current time is earlier than the expiration time then we use the current token
	if time.Now().UTC().Before(store.expiresAt.UTC()) {
		log.Info("Using existing token") // Log that the existing token is used
		return store.token, true         // Return the token and true bool indicating the token is valid
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
