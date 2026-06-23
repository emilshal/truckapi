package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"truckapi/pkg/config"

	"github.com/gofiber/fiber/v2"
)

// blankOAuthEnv forces the OAuth issuer to read as "not configured" even when
// .env loaded values into config.Env at startup. t.Setenv("",  "") alone is
// insufficient because config.GetEnv falls back to the Env map when os.Getenv
// is empty.
func blankOAuthEnv(t *testing.T) {
	t.Helper()
	origID := config.Env["CALLBACK_OAUTH_CLIENT_ID"]
	origSecret := config.Env["CALLBACK_OAUTH_CLIENT_SECRET"]
	origJWT := config.Env["CALLBACK_OAUTH_JWT_SECRET"]
	config.Env["CALLBACK_OAUTH_CLIENT_ID"] = ""
	config.Env["CALLBACK_OAUTH_CLIENT_SECRET"] = ""
	config.Env["CALLBACK_OAUTH_JWT_SECRET"] = ""
	t.Setenv("CALLBACK_OAUTH_CLIENT_ID", "")
	t.Setenv("CALLBACK_OAUTH_CLIENT_SECRET", "")
	t.Setenv("CALLBACK_OAUTH_JWT_SECRET", "")
	t.Cleanup(func() {
		config.Env["CALLBACK_OAUTH_CLIENT_ID"] = origID
		config.Env["CALLBACK_OAUTH_CLIENT_SECRET"] = origSecret
		config.Env["CALLBACK_OAUTH_JWT_SECRET"] = origJWT
	})
}

func setOAuthEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CALLBACK_OAUTH_CLIENT_ID", "chrob-callback-client")
	t.Setenv("CALLBACK_OAUTH_CLIENT_SECRET", "chrob-callback-secret")
	t.Setenv("CALLBACK_OAUTH_JWT_SECRET", "jwt-signing-key-test-only-do-not-use-in-prod")
	t.Setenv("CALLBACK_OAUTH_TOKEN_TTL_SECONDS", "3600")
}

func newOAuthTestApp() *fiber.App {
	app := fiber.New()
	app.Post("/oauth/token", OAuthTokenHandler())
	return app
}

func TestOAuthTokenHandler_JSONClientCredentials(t *testing.T) {
	setOAuthEnv(t)
	app := newOAuthTestApp()

	body := `{"client_id":"chrob-callback-client","client_secret":"chrob-callback-secret","grant_type":"client_credentials"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tr.AccessToken == "" {
		t.Fatal("access_token empty")
	}
	if tr.TokenType != "Bearer" {
		t.Fatalf("token_type=%q, want Bearer", tr.TokenType)
	}
	if tr.ExpiresIn != 3600 {
		t.Fatalf("expires_in=%d, want 3600", tr.ExpiresIn)
	}
	// Smoke check: looks like a JWT (three base64 segments).
	if strings.Count(tr.AccessToken, ".") != 2 {
		t.Fatalf("access_token does not look like a JWT: %q", tr.AccessToken)
	}
}

func TestOAuthTokenHandler_FormEncoded(t *testing.T) {
	setOAuthEnv(t)
	app := newOAuthTestApp()

	form := "grant_type=client_credentials&client_id=chrob-callback-client&client_secret=chrob-callback-secret"
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}
}

func TestOAuthTokenHandler_InvalidClientReturns401(t *testing.T) {
	setOAuthEnv(t)
	app := newOAuthTestApp()

	body := `{"client_id":"chrob-callback-client","client_secret":"WRONG","grant_type":"client_credentials"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", resp.StatusCode)
	}

	var er struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error != "invalid_client" {
		t.Fatalf("error=%q, want invalid_client", er.Error)
	}
}

func TestOAuthTokenHandler_UnsupportedGrantTypeReturns400(t *testing.T) {
	setOAuthEnv(t)
	app := newOAuthTestApp()

	body := `{"client_id":"chrob-callback-client","client_secret":"chrob-callback-secret","grant_type":"password"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}

	var er struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if er.Error != "unsupported_grant_type" {
		t.Fatalf("error=%q, want unsupported_grant_type", er.Error)
	}
}

func TestOAuthTokenHandler_MissingFieldsReturns400(t *testing.T) {
	setOAuthEnv(t)
	app := newOAuthTestApp()

	body := `{"grant_type":"client_credentials"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestOAuthTokenHandler_NotConfiguredReturns503(t *testing.T) {
	blankOAuthEnv(t)
	app := newOAuthTestApp()

	body := `{"client_id":"any","client_secret":"any","grant_type":"client_credentials"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", resp.StatusCode)
	}
}
