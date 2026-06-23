package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"truckapi/internal/oauthissuer"
	"truckapi/pkg/config"

	"github.com/gofiber/fiber/v2"
)

func newMiddlewareTestApp(mw fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Post("/", mw, func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

// clearOAuthEnv ensures OAuth env vars don't leak into tests that don't care
// about them. Both os.Setenv and the config.Env map need to be cleared because
// config.GetEnv falls back to the Env map when os.Getenv is empty, and the Env
// map is populated from .env at package init time.
func clearOAuthEnv(t *testing.T) {
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
	t.Setenv("CALLBACK_OAUTH_JWT_SECRET", "test-signing-key-long-enough-for-hs256")
	t.Setenv("CALLBACK_OAUTH_TOKEN_TTL_SECONDS", "3600")
}

func TestOfferCallbackAuthMiddleware_AllowsBearer(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "callback-secret")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "false")
	t.Setenv("API_KEY", "unused")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer callback-secret")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_AllowsAPIKeyFallback(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "callback-secret")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "true")
	t.Setenv("API_KEY", "api-key")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-KEY", "api-key")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_RejectsMissingAuth(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "callback-secret")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "false")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_RejectsWhenBearerIsNotConfigured(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", " ")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "true")
	t.Setenv("API_KEY", "api-key")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_RejectsAPIKeyWhenBearerIsNotConfigured(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", " ")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "true")
	t.Setenv("API_KEY", "api-key")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-KEY", "api-key")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_RejectsInvalidAPIKeyWhenProvided(t *testing.T) {
	clearOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "callback-secret")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "true")
	t.Setenv("API_KEY", "api-key")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-API-KEY", "wrong")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_AllowsOAuthIssuedJWT(t *testing.T) {
	setOAuthEnv(t)
	// Static bearer cleared; only OAuth JWT path should accept.
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "false")

	tr, err := oauthissuer.IssueToken("chrob-callback-client", "chrob-callback-secret")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_OAuthAndStaticBearerCoexist(t *testing.T) {
	// Both auth schemes configured; the JWT path should still work.
	setOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "callback-secret")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "false")

	tr, err := oauthissuer.IssueToken("chrob-callback-client", "chrob-callback-secret")
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	// JWT works.
	req1 := httptest.NewRequest(http.MethodPost, "/", nil)
	req1.Header.Set("Authorization", "Bearer "+tr.AccessToken)
	resp1, err := app.Test(req1, 5000)
	if err != nil {
		t.Fatalf("app.Test (JWT): %v", err)
	}
	if resp1.StatusCode != fiber.StatusOK {
		t.Fatalf("JWT path: expected 200, got %d", resp1.StatusCode)
	}

	// Static bearer also still works.
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	req2.Header.Set("Authorization", "Bearer callback-secret")
	resp2, err := app.Test(req2, 5000)
	if err != nil {
		t.Fatalf("app.Test (static): %v", err)
	}
	if resp2.StatusCode != fiber.StatusOK {
		t.Fatalf("static bearer path: expected 200, got %d", resp2.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_RejectsForgedJWT(t *testing.T) {
	setOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "false")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	// A garbage string that vaguely looks like a JWT but isn't.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.foo.bar")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_OAuthOnly_StillFailsClosedWithoutAuth(t *testing.T) {
	// Only OAuth configured (no static bearer). Request without auth → 401, not 503.
	setOAuthEnv(t)
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "false")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
