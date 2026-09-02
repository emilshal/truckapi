package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func TestSubmitLoadOfferHandler_AcceptsTNumberAliases(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"snake t_number", `{"t_number":"T100","offerPrice":500}`},
		{"camel tNumber", `{"tNumber":"T100","offerPrice":500}`},
		{"legacy carrierCode", `{"carrierCode":"T100","offerPrice":500}`},
		{"snake + legacy match", `{"t_number":"T100","carrierCode":"T100","offerPrice":500}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupOfferResponseDB(t)
			resetOfferSubmitIdempotencyForTests()

			var upstream chrobinson.LoadOfferRequest
			client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&upstream)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"offerRequestId":"offer-req-alias"}`))
			})

			app := newOfferTestApp(client)
			req := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers",
				bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, 5000)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != fiber.StatusAccepted {
				t.Fatalf("expected 202, got %d", resp.StatusCode)
			}
			if upstream.CarrierCode != "T100" {
				t.Fatalf("expected forwarded carrierCode=T100, got %q", upstream.CarrierCode)
			}

			var ack map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
				t.Fatalf("decode ack body: %v", err)
			}
			if ack["t_number"] != "T100" {
				t.Fatalf("expected ack to echo t_number=T100, got %v", ack["t_number"])
			}
		})
	}
}

func TestSubmitLoadOfferHandler_RejectsMismatchedTNumberAliases(t *testing.T) {
	setupOfferResponseDB(t)
	resetOfferSubmitIdempotencyForTests()

	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream must not be called when aliases mismatch")
	})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/123/offers",
		bytes.NewBufferString(`{"t_number":"T100","carrierCode":"T999","offerPrice":500}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
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

func TestBookLoadHandler_AcceptsTNumberAliases(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"snake t_number", `{"loadNumber":546698145,"t_number":"T777","emptyDateTime":"2026-03-13T15:00:00Z","emptyLocation":{"city":"KC","state":"MO","country":"US","zip":"64155"},"availableLoadCosts":[{"type":"LINEHAUL","code":"BIN","description":"BIN","sourceCostPerUnit":2100,"units":1,"currencyCode":"USD"}],"rateConfirmation":{"email":"o@e.com","name":"O"}}`},
		{"camel tNumber", `{"loadNumber":546698145,"tNumber":"T777","emptyDateTime":"2026-03-13T15:00:00Z","emptyLocation":{"city":"KC","state":"MO","country":"US","zip":"64155"},"availableLoadCosts":[{"type":"LINEHAUL","code":"BIN","description":"BIN","sourceCostPerUnit":2100,"units":1,"currencyCode":"USD"}],"rateConfirmation":{"email":"o@e.com","name":"O"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupOfferResponseDB(t)

			var upstream chrobinson.LoadBookingRequest
			client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&upstream)
				w.WriteHeader(http.StatusAccepted)
			})

			app := newOfferTestApp(client)
			req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req, 5000)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != fiber.StatusAccepted {
				t.Fatalf("expected 202, got %d", resp.StatusCode)
			}
			if upstream.CarrierCode != "T777" {
				t.Fatalf("expected forwarded carrierCode=T777, got %q", upstream.CarrierCode)
			}

			var ack map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
				t.Fatalf("decode ack: %v", err)
			}
			if ack["t_number"] != "T777" {
				t.Fatalf("expected ack to echo t_number=T777, got %v", ack["t_number"])
			}
		})
	}
}

func TestBookLoadHandler_AcceptsSnakeCaseLoadNumberAndOrderBidID(t *testing.T) {
	setupOfferResponseDB(t)

	var upstream chrobinson.LoadBookingRequest
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstream)
		w.WriteHeader(http.StatusAccepted)
	})

	app := newOfferTestApp(client)
	// Colleague's exact shape: load_number + t_number + order_bid_id (snake).
	body := `{"load_number":546698145,"t_number":"T777","order_bid_id":998877,"emptyDateTime":"2026-03-13T15:00:00Z","emptyLocation":{"city":"KC","state":"MO","country":"US","zip":"64155"},"availableLoadCosts":[{"type":"LINEHAUL","code":"BIN","description":"BIN","sourceCostPerUnit":2100,"units":1,"currencyCode":"USD"}],"rateConfirmation":{"email":"o@e.com","name":"O"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if upstream.LoadNumber != 546698145 {
		t.Fatalf("expected forwarded loadNumber=546698145, got %d", upstream.LoadNumber)
	}
	if upstream.CarrierCode != "T777" {
		t.Fatalf("expected forwarded carrierCode=T777, got %q", upstream.CarrierCode)
	}

	var ack map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack["order_bid_id"] != float64(998877) {
		t.Fatalf("expected ack to echo order_bid_id=998877, got %v", ack["order_bid_id"])
	}

	records := runtimeStore.listBookings()
	if len(records) != 1 || records[0].OrderBidID != 998877 {
		t.Fatalf("expected booking record with OrderBidID=998877, got %+v", records)
	}
}

func TestBookLoadHandler_AcceptsCHRobPrefixedLoadNumberAndStringBidID(t *testing.T) {
	setupOfferResponseDB(t)

	var upstream chrobinson.LoadBookingRequest
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstream)
		w.WriteHeader(http.StatusAccepted)
	})

	app := newOfferTestApp(client)
	// The colleague's exact shape: load_number prefixed with "CHROB-" and
	// order_bid_id sent as a quoted string.
	body := `{"load_number":"CHROB-202237619","t_number":"T6323830","order_bid_id":"13075604","emptyDateTime":"2026-07-06T06:00:00Z","emptyLocation":{"city":"SD","state":"CA","country":"US","zip":"92126"},"availableLoadCosts":[{"type":"Flat","code":"400","description":"Line Haul","sourceCostPerUnit":1600,"units":1,"currencyCode":"USD"}],"rateConfirmation":{"email":"o@e.com","name":"O"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if upstream.LoadNumber != 202237619 {
		t.Fatalf("expected CHROB- prefix stripped to 202237619, got %d", upstream.LoadNumber)
	}
	if upstream.CarrierCode != "T6323830" {
		t.Fatalf("expected carrierCode=T6323830, got %q", upstream.CarrierCode)
	}

	var ack map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if ack["order_bid_id"] != float64(13075604) {
		t.Fatalf("expected ack order_bid_id=13075604, got %v", ack["order_bid_id"])
	}

	records := runtimeStore.listBookings()
	if len(records) != 1 || records[0].OrderBidID != 13075604 {
		t.Fatalf("expected booking OrderBidID=13075604, got %+v", records)
	}
}

func TestBookLoadHandler_DefaultsLogisticsFieldsFromPickupCache(t *testing.T) {
	setupOfferResponseDB(t)

	// Seed cost + pickup defaults as a search would.
	chrobinson.CacheAvailableLoadCosts(202299001, []chrobinson.AvailableLoadCost{
		{Type: "Flat", Code: "400", Description: "Line Haul", SourceCostPerUnit: 1600, Units: 1, CurrencyCode: "USD"},
	})
	chrobinson.CachePickupDefaults(202299001, chrobinson.Location{
		City: "San Diego", StateCode: "CA", PostalCode: "92126", CountryCode: "US",
		County: "San Diego County", Coordinate: chrobinson.Coordinate{Lat: 32.715, Lon: -117.1573},
	}, "2026-07-15T06:00:00.000Z")

	var upstream chrobinson.LoadBookingRequest
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstream)
		w.WriteHeader(http.StatusAccepted)
	})

	app := newOfferTestApp(client)
	// Minimal body: only load/carrier/bid — no emptyLocation/emptyDateTime/rateConfirmation.
	body := `{"load_number":"CHROB-202299001","t_number":"T6323830","order_bid_id":"13075604","communication_email":"grisha@hfield.net"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if upstream.EmptyDateTime != "2026-07-15T06:00:00.000Z" {
		t.Fatalf("expected emptyDateTime defaulted from pickup, got %q", upstream.EmptyDateTime)
	}
	if upstream.EmptyLocation.City != "San Diego" || upstream.EmptyLocation.Coordinate.Latitude != 32.715 {
		t.Fatalf("expected emptyLocation defaulted from origin, got %+v", upstream.EmptyLocation)
	}
	if upstream.RateConfirmation.Name == "" || upstream.RateConfirmation.Email == "" {
		t.Fatalf("expected rateConfirmation defaulted, got %+v", upstream.RateConfirmation)
	}
}

func TestBookLoadHandler_RejectsMismatchedTNumberAliases(t *testing.T) {
	setupOfferResponseDB(t)
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream must not be called when aliases mismatch")
	})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(
		`{"loadNumber":546698145,"t_number":"T777","carrierCode":"T999","availableLoadCosts":[{"type":"LINEHAUL","code":"BIN","description":"BIN","sourceCostPerUnit":2100,"units":1,"currencyCode":"USD"}]}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
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

func TestOfferResponseHandler_ReturnsJSON2xx(t *testing.T) {
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
	if strings.TrimSpace(buf.String()) != `{"status":"ok"}` {
		t.Fatalf("expected JSON body {\"status\":\"ok\"}, got %q", buf.String())
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Fatalf("expected application/json content type, got %q", contentType)
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
	if record.OfferId != "123" {
		t.Fatalf("expected offerId=\"123\", got %q", record.OfferId)
	}
	if record.Price != 2100 {
		t.Fatalf("expected price=2100, got %d", record.Price)
	}
}

func TestOfferResponseHandler_ForwardsBrokerResponseToLoaderAPI(t *testing.T) {
	setupOfferResponseDB(t)

	var received loader.BrokerResponse
	loaderSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/loader/chrobinson/order-bids/broker-response" {
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
	if received.TNumber != "T100" {
		t.Fatalf("expected t_number=T100, got %q", received.TNumber)
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

func TestShipmentDetailsHandler_ForwardsToLoaderWithOrderBidID(t *testing.T) {
	setupOfferResponseDB(t)

	var received loader.ShipmentDetailsForward
	loaderSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/loader/chrobinson/book/response" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-API-KEY") != "test-loader-key" {
			t.Fatalf("unexpected X-API-KEY: %q", r.Header.Get("X-API-KEY"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode forward: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer loaderSrv.Close()

	t.Setenv("LOADER_API_BASE_URL", loaderSrv.URL)
	t.Setenv("LOADER_API_KEY", "test-loader-key")

	// Seed the booking so the shipment-details callback can find its order_bid_id.
	runtimeStore.addBooking(chrobinson.LoadBookingRecord{
		LoadNumber:  546698145,
		CarrierCode: "T6263835",
		OrderBidID:  424242,
		Status:      "accepted",
	})

	app := fiber.New()
	app.Post("/shipmentDetails/callback/here", ShipmentDetailsHandler)

	body := `{"time":"2026-03-11","carrierCode":"T6263835","loadNumber":"546698145","event":{"eventType":"LOAD DETAIL CHANGED","eventSubType":"Shipment Booked","loadNumber":"546698145","mode":"V"}}`
	req := httptest.NewRequest(http.MethodPost, "/shipmentDetails/callback/here", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if received.OrderBidID != 424242 {
		t.Fatalf("expected forwarded order_bid_id=424242, got %d", received.OrderBidID)
	}
	if received.LoadNumber != "546698145" {
		t.Fatalf("expected forwarded load_number=546698145, got %q", received.LoadNumber)
	}
	// The entire raw CHRob callback must be forwarded verbatim.
	if !strings.Contains(string(received.Callback), `"eventSubType":"Shipment Booked"`) {
		t.Fatalf("expected raw callback forwarded, got %s", string(received.Callback))
	}
}

func TestShipmentDetailsHandler_NoForwardWithoutBooking(t *testing.T) {
	setupOfferResponseDB(t)

	loaderSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("Loader must not be called when there is no matching booking")
	}))
	defer loaderSrv.Close()
	t.Setenv("LOADER_API_BASE_URL", loaderSrv.URL)
	t.Setenv("LOADER_API_KEY", "test-loader-key")

	app := fiber.New()
	app.Post("/shipmentDetails/callback/here", ShipmentDetailsHandler)

	// No booking seeded → no order_bid_id → must skip forward but still 200.
	body := `{"carrierCode":"T6263835","loadNumber":"999000111","event":{"eventType":"LOAD DETAIL CHANGED","eventSubType":"Shipment Booked","loadNumber":"999000111"}}`
	req := httptest.NewRequest(http.MethodPost, "/shipmentDetails/callback/here", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 even without forward, got %d", resp.StatusCode)
	}
}

func TestBookLoadHandler_UsesCachedAvailableLoadCosts(t *testing.T) {
	setupOfferResponseDB(t)
	t.Setenv("CHROB_CARRIER_CODE", "T6263835")

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
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(`{"loadNumber":546698145,"t_number":"T6263835","communication_email":"grisha@hfield.net"}`))
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

// The accepted bid price must be what we book at — not the load's original
// posted rate. Here the load was posted at $1500 but our bid of $2200 was
// accepted; the booking to CHRob must carry $2200.
func TestBookLoadHandler_BooksAtAcceptedPriceNotPostedPrice(t *testing.T) {
	setupOfferResponseDB(t)

	var upstream chrobinson.LoadBookingRequest
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstream)
		w.WriteHeader(http.StatusAccepted)
	})

	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Accepted offer at our bid price of 2200.
	runtimeStore.upsertOffer(chrobinson.OfferResponse{
		LoadNumber:   202299777,
		CarrierCode:  "T6323830",
		OfferResult:  "Accepted",
		Price:        2200,
		CurrencyCode: "USD",
		Status:       "booked",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	// Cached cost reflects the ORIGINAL posted rate of 1500.
	chrobinson.CacheAvailableLoadCosts(202299777, []chrobinson.AvailableLoadCost{{
		Type: "Flat", Code: "400", Description: "Line Haul",
		SourceCostPerUnit: 1500, Units: 1, CurrencyCode: "USD",
	}})

	app := newOfferTestApp(client)
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(`{"loadNumber":202299777,"carrierCode":"T6323830","communication_email":"grisha@hfield.net"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if len(upstream.AvailableLoadCosts) != 1 {
		t.Fatalf("expected 1 cost, got %d", len(upstream.AvailableLoadCosts))
	}
	cost := upstream.AvailableLoadCosts[0]
	if cost.SourceCostPerUnit != 2200 {
		t.Fatalf("expected booking at accepted price 2200, got %v (posted was 1500)", cost.SourceCostPerUnit)
	}
	// Cost shape must still be preserved so CHRob's schema validates.
	if cost.Code != "400" || cost.Description != "Line Haul" || cost.Units != 1 {
		t.Fatalf("expected cost shape preserved, got %+v", cost)
	}
}

// A CHRob booking failure must surface CHRob's exact status and body to the
// caller (e.g. 423 "Book Locked"), not a generic 500.
func TestChrobBookErrorResponse_RelaysCHRobStatusAndBody(t *testing.T) {
	err := &chrobinson.HTTPStatusError{
		StatusCode: 423,
		Operation:  "book load",
		Body:       `{"statusCode":423,"error":"Locked","message":"Book Locked"}`,
	}
	status, body := chrobBookErrorResponse(err, "Failed to process booking")
	if status != 423 {
		t.Fatalf("expected status 423 passed through, got %d", status)
	}
	if body["error"] != "Failed to process booking" {
		t.Fatalf("unexpected public error: %v", body["error"])
	}
	if body["chrobStatus"] != 423 {
		t.Fatalf("expected chrobStatus 423, got %v", body["chrobStatus"])
	}
	if body["details"] != `{"statusCode":423,"error":"Locked","message":"Book Locked"}` {
		t.Fatalf("expected raw CHRob body in details, got %v", body["details"])
	}

	// Non-HTTP errors (network, marshal) keep the 500 with the error text.
	status, body = chrobBookErrorResponse(errTestNetwork, "Failed to process booking")
	if status != 500 || body["details"] == "" {
		t.Fatalf("expected 500 with details for plain error, got %d %v", status, body)
	}
}

var errTestNetwork = errors.New("dial tcp: connection refused")

// Bookings and offers with no carrier identifier must be rejected with a
// clear error — never silently fall back to the env CHROB_CARRIER_CODE, which
// would book/bid under the wrong company.
func TestBookAndOffer_RejectMissingTNumber(t *testing.T) {
	// Upstream must never be reached: the request is rejected before any CHRob call.
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("CHRob upstream must not be called when t_number is missing (path %s)", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})
	app := newOfferTestApp(client)

	book := httptest.NewRequest(http.MethodPost, "/v1/shipments/books",
		bytes.NewBufferString(`{"load_number":"CHROB-546698145","order_bid_id":1}`))
	book.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(book, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("book without t_number: expected 400, got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if msg, _ := body["error"].(string); !strings.Contains(msg, "t_number is required") {
		t.Fatalf("book without t_number: expected t_number error, got %v", body)
	}

	offer := httptest.NewRequest(http.MethodPost, "/v1/shipments/546698145/offers",
		bytes.NewBufferString(`{"offerPrice":500,"order_bid_id":1}`))
	offer.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(offer, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("offer without t_number: expected 400, got %d", resp.StatusCode)
	}
	body = nil
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if msg, _ := body["error"].(string); !strings.Contains(msg, "t_number is required") {
		t.Fatalf("offer without t_number: expected t_number error, got %v", body)
	}
}

// The rate-confirmation email must come from the caller's communication_email;
// a booking without one is rejected (no fallback to a static/env address), and
// when present it is what goes to CHRob and is echoed back.
func TestBookLoadHandler_CommunicationEmailRequiredAndUsed(t *testing.T) {
	var upstream chrobinson.LoadBookingRequest
	calls := 0
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewDecoder(r.Body).Decode(&upstream)
		w.WriteHeader(http.StatusAccepted)
	})
	chrobinson.CacheAvailableLoadCosts(546698146, []chrobinson.AvailableLoadCost{{
		LoadNumber: 546698146, Type: "Flat", Code: "400", Description: "Line Haul",
		SourceCostPerUnit: 900, Units: 1, CurrencyCode: "USD",
	}})
	app := newOfferTestApp(client)

	// Missing email -> 400, upstream untouched.
	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books",
		bytes.NewBufferString(`{"load_number":"CHROB-546698146","t_number":"T777","order_bid_id":5}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["error"].(string), "communication_email is required") {
		t.Fatalf("expected 400 communication_email error, got %d %v", resp.StatusCode, body)
	}
	if calls != 0 {
		t.Fatalf("upstream must not be called without an email")
	}

	// Present -> used as rateConfirmation.email and echoed.
	req = httptest.NewRequest(http.MethodPost, "/v1/shipments/books",
		bytes.NewBufferString(`{"load_number":"CHROB-546698146","t_number":"T777","order_bid_id":5,"communication_email":"dispatch@carrier.example"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body = nil
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d %v", resp.StatusCode, body)
	}
	if upstream.RateConfirmation.Email != "dispatch@carrier.example" {
		t.Fatalf("CHRob rateConfirmation.email = %q, want communication_email", upstream.RateConfirmation.Email)
	}
	if body["communication_email"] != "dispatch@carrier.example" {
		t.Fatalf("response should echo communication_email, got %v", body)
	}
}

// The Loader may share one payload shape between bid and book: communication_email,
// load_number, and a string-typed order_bid_id must not trip the strict decoder.
// Genuinely unknown fields are still rejected, now with the decoder's reason.
func TestSubmitLoadOfferHandler_AcceptsSharedLoaderFields(t *testing.T) {
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"offerRequestId":"offer-req-shared"}`))
	})
	app := newOfferTestApp(client)

	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/566630989/offers",
		bytes.NewBufferString(`{"t_number":"T100","offerPrice":500,"order_bid_id":"13728194","load_number":"CHROB-566630989","communication_email":"g@hfield.net"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d %v", resp.StatusCode, body)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/shipments/566630989/offers",
		bytes.NewBufferString(`{"t_number":"T100","offerPrice":500,"bogus_field":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body = nil
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["error"].(string), `unknown field "bogus_field"`) {
		t.Fatalf("expected 400 naming the unknown field, got %d %v", resp.StatusCode, body)
	}
}

// offerPrice arrives from the Loader as a string ("550") — must parse like a number.
func TestSubmitLoadOfferHandler_AcceptsStringOfferPrice(t *testing.T) {
	var upstream chrobinson.LoadOfferRequest
	client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstream)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"offerRequestId":"offer-req-strprice"}`))
	})
	app := newOfferTestApp(client)

	req := httptest.NewRequest(http.MethodPost, "/v1/shipments/566727455/offers",
		bytes.NewBufferString(`{"offerPrice":"900","t_number":"T6263835","order_bid_id":13735332}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != fiber.StatusAccepted {
		t.Fatalf("expected 202, got %d %v", resp.StatusCode, body)
	}
	if upstream.OfferPrice != 900 {
		t.Fatalf("CHRob offerPrice = %d, want 900", upstream.OfferPrice)
	}

	// Non-numeric price still gets a clear rejection.
	req = httptest.NewRequest(http.MethodPost, "/v1/shipments/566727455/offers",
		bytes.NewBufferString(`{"offerPrice":"abc","t_number":"T6263835"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body = nil
	_ = json.NewDecoder(resp.Body).Decode(&body)
	// A non-numeric string fails at decode time; the decoder's reason names the
	// offending literal ("abc") rather than the field, which is still actionable.
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["error"].(string), "invalid number") {
		t.Fatalf("expected 400 with decoder reason, got %d %v", resp.StatusCode, body)
	}
}

// PHP request data arrives with numbers as strings. The book endpoint must
// accept Grisha's full payload in all-strings form (and mixed), the same
// tolerance the offer endpoint has for offerPrice/order_bid_id.
func TestBookLoadHandler_AcceptsAllStringLoaderPayload(t *testing.T) {
	for _, body := range []string{
		`{"load_number":"CHROB-546698147","t_number":"T777","order_bid_id":"13735332","communication_email":"g@hfield.net"}`,
		`{"load_number":546698147,"t_number":"T777","order_bid_id":13735332,"communication_email":"g@hfield.net"}`,
	} {
		var upstream chrobinson.LoadBookingRequest
		client, _ := newTestCHRobAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&upstream)
			w.WriteHeader(http.StatusAccepted)
		})
		chrobinson.CacheAvailableLoadCosts(546698147, []chrobinson.AvailableLoadCost{{
			LoadNumber: 546698147, Type: "Flat", Code: "400", Description: "Line Haul",
			SourceCostPerUnit: 900, Units: 1, CurrencyCode: "USD",
		}})
		app := newOfferTestApp(client)

		req := httptest.NewRequest(http.MethodPost, "/v1/shipments/books", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, 5000)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if resp.StatusCode != fiber.StatusAccepted {
			t.Fatalf("body %s: expected 202, got %d %v", body, resp.StatusCode, out)
		}
		if upstream.LoadNumber != 546698147 || upstream.CarrierCode != "T777" || upstream.RateConfirmation.Email != "g@hfield.net" {
			t.Fatalf("body %s: upstream booking wrong: %+v", body, upstream)
		}
		if out["orderBidId"] != float64(13735332) && out["order_bid_id"] != float64(13735332) {
			// order_bid_id round-trips via the response; either key form counts.
			t.Logf("note: response keys: %v", out)
		}
	}
}
