package routes

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestBidOfferRoute_DoesNotRequireAPIKeyMiddleware(t *testing.T) {
	app := InitializeRoutes(nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers",
		bytes.NewBufferString(`{"carrierCode":"T100","offerPrice":500}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode == fiber.StatusUnauthorized {
		t.Fatalf("expected non-401 when API key middleware is disabled, got %d", resp.StatusCode)
	}
}
