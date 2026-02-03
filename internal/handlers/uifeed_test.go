package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"truckapi/internal/loader"
	"truckapi/internal/uifeed"

	"github.com/gofiber/fiber/v2"
)

func TestUIOrdersFeedHandler_Basic(t *testing.T) {
	store := uifeed.NewStore(10)
	store.Add(loader.LoaderOrder{Source: string(uifeed.SourceTruckstop), OrderNumber: "TS-1"})

	app := fiber.New()
	app.Get("/api/orders", UIOrdersFeedHandler(store))

	req := httptest.NewRequest("GET", "/api/orders?source=TRUCKSTOP&page=1&pageSize=10", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var page uifeed.Page
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Source != uifeed.SourceTruckstop {
		t.Fatalf("expected source TRUCKSTOP, got %s", page.Source)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected total=1 items=1, got total=%d items=%d", page.Total, len(page.Items))
	}
	if page.Items[0].Order.OrderNumber != "TS-1" {
		t.Fatalf("expected TS-1, got %s", page.Items[0].Order.OrderNumber)
	}
}
