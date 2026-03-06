package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"truckapi/db"
	"truckapi/internal/auth"
	"truckapi/internal/chrobinson"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestCHRobAPIClient(t *testing.T, h http.HandlerFunc) (*chrobinson.APIClient, *httptest.Server) {
	t.Helper()
	t.Setenv("CHROB_ACCESS_TOKEN", "test-token")

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	tokenStore := auth.NewTokenStore()
	client := chrobinson.NewAPIClient(srv.URL, tokenStore, srv.Client())
	return client, srv
}

func newOfferTestApp(apiClient *chrobinson.APIClient) *fiber.App {
	app := fiber.New()
	app.Post("/v1/shipments/:loadNumber/offers", SubmitLoadOfferHandler(apiClient))
	app.Post("/offerResponse/callback/here", OfferResponseHandler)
	return app
}

func TestSubmitLoadOfferHandler_StrictJSONRejectsUnknownFields(t *testing.T) {
	resetOfferSubmitIdempotencyForTests()

	callCount := 0
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"offerRequestId":"abc"}`))
	})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers",
		bytes.NewBufferString(`{"carrierCode":"T100","offerPrice":500,"unknownField":1}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if callCount != 0 {
		t.Fatalf("expected upstream not called, got %d calls", callCount)
	}
}

func TestSubmitLoadOfferHandler_Passthrough422(t *testing.T) {
	resetOfferSubmitIdempotencyForTests()

	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"missing availableLoadCost"}`))
	})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers",
		bytes.NewBufferString(`{"carrierCode":"T100","offerPrice":500}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := int(body["chrobStatus"].(float64)); got != http.StatusUnprocessableEntity {
		t.Fatalf("expected chrobStatus=422, got %d", got)
	}
	if _, ok := body["details"]; !ok {
		t.Fatalf("expected details in response body")
	}
}

func TestSubmitLoadOfferHandler_IdempotencyReplay(t *testing.T) {
	resetOfferSubmitIdempotencyForTests()
	t.Setenv("BID_IDEMPOTENCY_TTL_MINUTES", "60")

	callCount := 0
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"offerRequestId":"offer-req-1"}`))
	})

	app := newOfferTestApp(client)
	body := `{"carrierCode":"T100","offerPrice":500,"offerNote":" test ","currencyCode":"usd"}`

	req1 := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", "idem-1")
	resp1, err := app.Test(req1, 5000)
	if err != nil {
		t.Fatalf("first app.Test: %v", err)
	}
	if resp1.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected first 202, got %d", resp1.StatusCode)
	}
	var r1 offerSubmitResponse
	if err := json.NewDecoder(resp1.Body).Decode(&r1); err != nil {
		t.Fatalf("decode first: %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "idem-1")
	resp2, err := app.Test(req2, 5000)
	if err != nil {
		t.Fatalf("second app.Test: %v", err)
	}
	if resp2.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected second 202, got %d", resp2.StatusCode)
	}
	if got := resp2.Header.Get("X-Idempotent-Replay"); got != "true" {
		t.Fatalf("expected X-Idempotent-Replay=true, got %q", got)
	}
	var r2 offerSubmitResponse
	if err := json.NewDecoder(resp2.Body).Decode(&r2); err != nil {
		t.Fatalf("decode second: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected exactly 1 upstream call, got %d", callCount)
	}
	if r1.OfferRequestID != r2.OfferRequestID {
		t.Fatalf("expected same offerRequestId, got %q vs %q", r1.OfferRequestID, r2.OfferRequestID)
	}
	if !r2.IdempotentReplay {
		t.Fatalf("expected idempotentReplay=true on cached response")
	}
}

func setupOfferResponseDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB := db.DB

	gdb, err := gorm.Open(sqlite.Open("file:offer_response_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&chrobinson.OfferResponse{}); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}
	db.DB = gdb

	t.Cleanup(func() {
		db.DB = oldDB
		sqlDB, _ := gdb.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

func TestOfferResponseHandler_StatusMapping(t *testing.T) {
	setupOfferResponseDB(t)
	app := newOfferTestApp(nil)

	tests := []struct {
		name           string
		offerRequestID string
		offerRequest   string
		expected       string
	}{
		{
			name:           "accepted maps to booked",
			offerRequestID: "req-accepted",
			offerRequest:   `{"loadNumber":1,"carrierCode":"T100","offerRequestId":"req-accepted","offerId":11,"offerResult":"Accepted","price":500,"currencyCode":"USD","rejectReasons":[]}`,
			expected:       "booked",
		},
		{
			name:           "counter maps to countered",
			offerRequestID: "req-counter",
			offerRequest:   `{"loadNumber":2,"carrierCode":"T100","offerRequestId":"req-counter","offerId":12,"offerResult":"Counter","price":700,"currencyCode":"USD","rejectReasons":[]}`,
			expected:       "countered",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/offerResponse/callback/here", bytes.NewBufferString(tc.offerRequest))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, 5000)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}

			var record chrobinson.OfferResponse
			if err := db.DB.Where("offer_request_id = ?", tc.offerRequestID).First(&record).Error; err != nil {
				t.Fatalf("query record: %v", err)
			}
			if record.Status != tc.expected {
				t.Fatalf("expected status=%q, got %q", tc.expected, record.Status)
			}
		})
	}
}

func TestOfferResponseHandler_ReturnsPlainText2xx(t *testing.T) {
	setupOfferResponseDB(t)
	app := newOfferTestApp(nil)

	req := httptest.NewRequest(http.MethodPost, "/offerResponse/callback/here",
		bytes.NewBufferString(`{"offerRequestId":"req-plain","offerResult":"Rejected","rejectReasons":["x"]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	if strings.TrimSpace(buf.String()) != "ok" {
		t.Fatalf("expected plain text ok, got %q", buf.String())
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", contentType)
	}
}
