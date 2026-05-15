package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"truckapi/internal/auth"
	"truckapi/internal/chrobinson"
	"truckapi/internal/loader"

	"github.com/gofiber/fiber/v2"
)

func testJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp.Unix(), 10) + `}`))
	return header + "." + payload + ".sig"
}

func newTestCHRobAPIClient(t *testing.T, h http.HandlerFunc) (*chrobinson.APIClient, *httptest.Server) {
	t.Helper()
	t.Setenv("CHROB_ACCESS_TOKEN", testJWT(time.Now().Add(1*time.Hour)))

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	tokenStore := auth.NewTokenStore()
	client := chrobinson.NewAPIClient(srv.URL, tokenStore, srv.Client())
	return client, srv
}

func newOfferTestApp(apiClient *chrobinson.APIClient) *fiber.App {
	app := fiber.New()
	app.Post("/v1/shipments/:loadNumber/offers", SubmitLoadOfferHandler(apiClient))
	app.Post("/v1/shipments/books", BookLoadHandler(apiClient))
	app.Post("/offerResponse/callback/here", OfferResponseHandler)
	app.Get("/v1/bookings", FetchAllBookingsHandler)
	app.Get("/v1/shipment-details", FetchAllShipmentDetailsHandler)
	app.Post("/shipmentDetails/callback/here", ShipmentDetailsHandler)
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

func TestSubmitLoadOfferHandler_StoresOrderBidID(t *testing.T) {
	setupOfferResponseDB(t)
	resetOfferSubmitIdempotencyForTests()

	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"offerRequestId":"offer-req-bid"}`))
	})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers",
		bytes.NewBufferString(`{"carrierCode":"T100","offerPrice":500,"order_bid_id":11852585}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	offers := runtimeStore.listOffers()
	if len(offers) != 1 {
		t.Fatalf("expected 1 offer record, got %d", len(offers))
	}
	if offers[0].OrderBidID != 11852585 {
		t.Fatalf("expected orderBidId=11852585, got %d", offers[0].OrderBidID)
	}
}

func setupOfferResponseDB(t *testing.T) {
	t.Helper()
	resetRuntimeStoreForTests()
	chrobinson.ResetRuntimeAvailableLoadCostsForTests()
}

func TestBookLoadHandler_TracksBookingRecordInMemory(t *testing.T) {
	setupOfferResponseDB(t)
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/shipments/books" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(`{
		"loadNumber": 546698145,
		"carrierCode": "T6263835",
		"emptyDateTime": "2026-03-13T15:00:00Z",
		"emptyLocation": {"city":"Kansas City","state":"MO","country":"US","zip":"64155"},
		"availableLoadCosts": [{"type":"LINEHAUL","code":"BIN","description":"BIN","sourceCostPerUnit":2100,"units":1,"currencyCode":"USD"}],
		"rateConfirmation": {"email":"ops@example.com","name":"Ops"}
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	records := runtimeStore.listBookings()
	if len(records) != 1 {
		t.Fatalf("expected 1 booking record, got %d", len(records))
	}
	record := records[0]
	if record.Status != "accepted" {
		t.Fatalf("expected status accepted, got %q", record.Status)
	}
	if !strings.Contains(record.RawRequest, "\"loadNumber\":546698145") {
		t.Fatalf("expected raw request to contain load number, got %q", record.RawRequest)
	}
}

func TestShipmentDetailsHandler_FetchEndpoint(t *testing.T) {
	setupOfferResponseDB(t)
	app := newOfferTestApp(nil)

	req := httptest.NewRequest(http.MethodPost, "/shipmentDetails/callback/here", bytes.NewBufferString(`{
		"time":"2026-03-13",
		"carrierCode":"T6263835",
		"scac":"ABCD",
		"loadNumber":"546698145",
		"clientId":"client-1",
		"eventTime":"2026-03-13",
		"event":{"eventType":"LOAD DETAIL CHANGED","eventSubType":"Stop Created","loadNumber":"546698145","activityDate":"2026-03-13T15:00:00Z","mode":"V"}
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("shipment details callback app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 from callback, got %d", resp.StatusCode)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/shipment-details", nil)
	listResp, err := app.Test(listReq, 5000)
	if err != nil {
		t.Fatalf("shipment details list app.Test: %v", err)
	}
	if listResp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 from list, got %d", listResp.StatusCode)
	}

	var body map[string][]map[string]interface{}
	if err := json.NewDecoder(listResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(body["shipmentDetails"]) != 1 {
		t.Fatalf("expected 1 shipment detail record, got %d", len(body["shipmentDetails"]))
	}
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

			offers := runtimeStore.listOffers()
			if len(offers) == 0 {
				t.Fatalf("expected at least 1 offer record")
			}
			record := offers[0]
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

func TestOfferResponseHandler_AcceptsStringNumbers(t *testing.T) {
	setupOfferResponseDB(t)
	app := newOfferTestApp(nil)

	req := httptest.NewRequest(http.MethodPost, "/offerResponse/callback/here",
		bytes.NewBufferString(`{"loadNumber":"546698145","carrierCode":"T6263835","offerRequestId":"req-string-numbers","offerId":"123","offerResult":"Accepted","price":"2100","currencyCode":"USD","rejectReasons":[]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	offers := runtimeStore.listOffers()
	if len(offers) == 0 {
		t.Fatalf("expected at least 1 offer record")
	}
	record := offers[0]
	if record.LoadNumber != 546698145 {
		t.Fatalf("expected loadNumber=546698145, got %d", record.LoadNumber)
	}
	if record.OfferId != 123 {
		t.Fatalf("expected offerId=123, got %d", record.OfferId)
	}
	if record.Price != 2100 {
		t.Fatalf("expected price=2100, got %d", record.Price)
	}
}

func TestOfferResponseHandler_ForwardsBrokerResponseToLoaderAPI(t *testing.T) {
	setupOfferResponseDB(t)

	var received loader.BrokerResponse
	loaderSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/loader/order-bids/broker-response" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-API-KEY") != "test-loader-key" {
			t.Fatalf("unexpected X-API-KEY: %q", r.Header.Get("X-API-KEY"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode broker response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer loaderSrv.Close()

	t.Setenv("LOADER_API_BASE_URL", loaderSrv.URL)
	t.Setenv("LOADER_API_KEY", "test-loader-key")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	runtimeStore.upsertOffer(chrobinson.OfferResponse{
		LoadNumber:     1,
		CarrierCode:    "T100",
		OfferRequestId: "req-counter",
		OrderBidID:     11852585,
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	app := newOfferTestApp(nil)
	req := httptest.NewRequest(http.MethodPost, "/offerResponse/callback/here",
		bytes.NewBufferString(`{"loadNumber":1,"carrierCode":"T100","offerRequestId":"req-counter","offerId":12,"offerResult":"Counter","price":900,"currencyCode":"USD","rejectReasons":[]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if received.OrderBidID != 11852585 {
		t.Fatalf("expected order_bid_id=11852585, got %d", received.OrderBidID)
	}
	if received.OfferResult != "Counter" {
		t.Fatalf("expected offerResult=Counter, got %q", received.OfferResult)
	}
	if received.Price != 900 {
		t.Fatalf("expected price=900, got %d", received.Price)
	}

	offers := runtimeStore.listOffers()
	if len(offers) == 0 {
		t.Fatalf("expected stored offer record")
	}
	if offers[0].BrokerResponseAt == "" {
		t.Fatalf("expected brokerResponseAt to be set")
	}
	if offers[0].BrokerResponseError != "" {
		t.Fatalf("expected empty brokerResponseError, got %q", offers[0].BrokerResponseError)
	}
}

func TestShipmentDetailsHandler_TracksCallbackInMemory(t *testing.T) {
	setupOfferResponseDB(t)

	app := fiber.New()
	app.Post("/shipmentDetails/callback/here", ShipmentDetailsHandler)

	body := `{"time":"2026-03-11","carrierCode":"T6263835","scac":"ABCD","loadNumber":"546698145","clientId":"client-1","eventTime":"2026-03-11","event":{"eventType":"LOAD DETAIL CHANGED","eventSubType":"Stop Created","loadNumber":"546698145","mode":"V","activityDate":"2026-03-11T16:04:15Z","notes":"detail callback"}}`
	req := httptest.NewRequest(http.MethodPost, "/shipmentDetails/callback/here", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	records := runtimeStore.listShipmentDetails()
	if len(records) != 1 {
		t.Fatalf("expected 1 shipment detail record, got %d", len(records))
	}
	record := records[0]
	if record.EventType != "LOAD DETAIL CHANGED" {
		t.Fatalf("expected eventType to persist, got %q", record.EventType)
	}
	if record.EventSubType != "Stop Created" {
		t.Fatalf("expected eventSubType to persist, got %q", record.EventSubType)
	}
	if !strings.Contains(record.RawPayload, `"loadNumber":"546698145"`) {
		t.Fatalf("expected raw payload to be stored, got %q", record.RawPayload)
	}
}

func TestBookLoadHandler_UsesCachedAvailableLoadCosts(t *testing.T) {
	setupOfferResponseDB(t)

	var upstreamRequest chrobinson.LoadBookingRequest
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/shipments/books" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamRequest); err != nil {
			t.Fatalf("decode booking request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	})

	now := time.Now().UTC().Format(time.RFC3339Nano)
	runtimeStore.upsertOffer(chrobinson.OfferResponse{
		LoadNumber:     546698145,
		CarrierCode:    "T6263835",
		OfferRequestId: "req-book",
		OfferResult:    "Counter",
		Price:          900,
		CurrencyCode:   "USD",
		Status:         "countered",
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	chrobinson.CacheAvailableLoadCosts(546698145, []chrobinson.AvailableLoadCost{{
		LoadNumber:        546698145,
		CarrierCode:       "T6263835",
		Type:              "Flat",
		Code:              "400",
		Description:       "Line Haul",
		SourceCostPerUnit: 900,
		Units:             1,
		CurrencyCode:      "USD",
		BinCostKey:        "bin-1",
	}})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(`{"loadNumber":546698145}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	if upstreamRequest.LoadNumber != 546698145 {
		t.Fatalf("expected loadNumber=546698145, got %d", upstreamRequest.LoadNumber)
	}
	if upstreamRequest.CarrierCode != "T6263835" {
		t.Fatalf("expected carrierCode=T6263835, got %q", upstreamRequest.CarrierCode)
	}
	if len(upstreamRequest.AvailableLoadCosts) != 1 {
		t.Fatalf("expected 1 cached load cost, got %d", len(upstreamRequest.AvailableLoadCosts))
	}
	cost := upstreamRequest.AvailableLoadCosts[0]
	if cost.Type != "Flat" || cost.Code != "400" || cost.Description != "Line Haul" {
		t.Fatalf("unexpected cached cost metadata: %+v", cost)
	}
	if cost.SourceCostPerUnit != 900 {
		t.Fatalf("expected cached price=900, got %v", cost.SourceCostPerUnit)
	}
	if cost.Units != 1 {
		t.Fatalf("expected cached units=1, got %d", cost.Units)
	}
	if cost.CurrencyCode != "USD" {
		t.Fatalf("expected cached currencyCode=USD, got %q", cost.CurrencyCode)
	}
}
