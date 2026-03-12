package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newMiddlewareTestApp(mw fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Post("/", mw, func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func TestOfferCallbackAuthMiddleware_AllowsBearer(t *testing.T) {
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

func TestOfferCallbackAuthMiddleware_AllowsNoAuthWhenNoBearerConfigured(t *testing.T) {
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "")
	t.Setenv("CHROB_CALLBACK_ALLOW_API_KEY", "true")
	t.Setenv("API_KEY", "api-key")

	app := newMiddlewareTestApp(OfferCallbackAuthMiddleware())

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestOfferCallbackAuthMiddleware_RejectsInvalidAPIKeyWhenProvided(t *testing.T) {
	t.Setenv("CHROB_CALLBACK_BEARER_TOKEN", "")
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
