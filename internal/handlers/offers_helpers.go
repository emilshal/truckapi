package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"truckapi/internal/chrobinson"
	"truckapi/pkg/config"

	"github.com/gofiber/fiber/v2"
)

const (
	maxOfferNoteLength        = 1000
	maxIdempotencyKeyLength   = 200
	maxCHRobErrorDetailLength = 4096
	defaultBidIdempotencyTTL  = 60 * time.Minute
)

type offerRequestInput struct {
	CarrierCode  string `json:"carrierCode"`
	TNumber      string `json:"t_number"`
	TNumberCamel string `json:"tNumber"`
	// offerPrice accepts a number or a numeric string (the Loader sends both).
	OfferPrice        json.Number `json:"offerPrice"`
	OfferNote         string      `json:"offerNote"`
	CurrencyCode      string      `json:"currencyCode"`
	AvailableLoadCost *int        `json:"availableLoadCost"`
	// order_bid_id accepts a number or a numeric string (the Loader sends both).
	OrderBidID      json.Number `json:"order_bid_id"`
	OrderBidIDCamel json.Number `json:"orderBidId"`
	// Fields the Loader shares between its bid and book payloads. Accepted so a
	// common payload shape doesn't trip the strict decoder; not used for offers
	// (the load number comes from the URL).
	CommunicationEmail      string          `json:"communication_email"`
	CommunicationEmailCamel string          `json:"communicationEmail"`
	LoadNumberSnake         json.RawMessage `json:"load_number"`
	LoadNumberCamel         json.RawMessage `json:"loadNumber"`
}

type offerSubmitResponse struct {
	Message          string `json:"message"`
	LoadNumber       string `json:"loadNumber"`
	TNumber          string `json:"t_number,omitempty"`
	OfferRequestID   string `json:"offerRequestId"`
	Status           string `json:"status"`
	Persisted        bool   `json:"persisted"`
	Warning          string `json:"warning,omitempty"`
	IdempotentReplay bool   `json:"idempotentReplay,omitempty"`
}

type cachedOfferSubmitResponse struct {
	Fingerprint string
	Response    offerSubmitResponse
	CreatedAt   time.Time
}

type offerSubmitIdempotencyStore struct {
	mu      sync.Mutex
	entries map[string]cachedOfferSubmitResponse
}

func newOfferSubmitIdempotencyStore() *offerSubmitIdempotencyStore {
	return &offerSubmitIdempotencyStore{
		entries: make(map[string]cachedOfferSubmitResponse),
	}
}

var offerSubmitIdempotency = newOfferSubmitIdempotencyStore()

func (s *offerSubmitIdempotencyStore) ttl() time.Duration {
	raw := strings.TrimSpace(config.GetEnv(config.BidIdempotencyTTLMinutes, ""))
	if raw == "" {
		return defaultBidIdempotencyTTL
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultBidIdempotencyTTL
	}
	return time.Duration(n) * time.Minute
}

func (s *offerSubmitIdempotencyStore) pruneLocked(now time.Time, ttl time.Duration) {
	cutoff := now.Add(-ttl)
	for k, v := range s.entries {
		if v.CreatedAt.Before(cutoff) {
			delete(s.entries, k)
		}
	}
}

func (s *offerSubmitIdempotencyStore) Get(key, fingerprint string, now time.Time) (offerSubmitResponse, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ttl := s.ttl()
	s.pruneLocked(now, ttl)

	rec, ok := s.entries[key]
	if !ok {
		return offerSubmitResponse{}, false, false
	}
	if rec.Fingerprint != fingerprint {
		return offerSubmitResponse{}, false, true
	}

	resp := rec.Response
	resp.IdempotentReplay = true
	return resp, true, false
}

func (s *offerSubmitIdempotencyStore) Put(key, fingerprint string, resp offerSubmitResponse, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneLocked(now, s.ttl())
	resp.IdempotentReplay = false
	s.entries[key] = cachedOfferSubmitResponse{
		Fingerprint: fingerprint,
		Response:    resp,
		CreatedAt:   now,
	}
}

func resetOfferSubmitIdempotencyForTests() {
	offerSubmitIdempotency.mu.Lock()
	defer offerSubmitIdempotency.mu.Unlock()
	offerSubmitIdempotency.entries = make(map[string]cachedOfferSubmitResponse)
}

func idempotencyKeyFromRequest(c *fiber.Ctx) (string, error) {
	key := strings.TrimSpace(c.Get("Idempotency-Key"))
	if key == "" {
		return "", nil
	}
	if len(key) > maxIdempotencyKeyLength {
		return "", fiber.NewError(fiber.StatusBadRequest, "Idempotency-Key is too long")
	}
	return key, nil
}

func offerSubmitFingerprint(loadNumber string, req chrobinson.LoadOfferRequest) string {
	offerNote := strings.TrimSpace(req.OfferNote)
	currency := strings.ToUpper(strings.TrimSpace(req.CurrencyCode))
	availableCost := ""
	if req.AvailableLoadCost > 0 {
		availableCost = fmt.Sprintf("%d", req.AvailableLoadCost)
	}
	return strings.Join([]string{
		strings.TrimSpace(loadNumber),
		strings.TrimSpace(req.CarrierCode),
		fmt.Sprintf("%d", req.OfferPrice),
		offerNote,
		currency,
		availableCost,
	}, "|")
}

func decodeStrictJSON(body []byte, dst interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected extra JSON content")
	}
	return nil
}

func isAlpha3Currency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 'A' || b > 'Z' {
			return false
		}
	}
	return true
}

type parsedOfferSubmitInput struct {
	Request    chrobinson.LoadOfferRequest
	OrderBidID int
}

func validateAndBuildOfferRequest(raw []byte) (parsedOfferSubmitInput, error) {
	var input offerRequestInput
	if err := decodeStrictJSON(raw, &input); err != nil {
		// Relay the decoder's reason (e.g. unknown field "x", type mismatch) so
		// the caller can see exactly which part of the payload we rejected.
		return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "Invalid request data: "+err.Error())
	}

	carrier, err := resolveCarrierAliases(input.TNumber, input.TNumberCamel, input.CarrierCode)
	if err != nil {
		return parsedOfferSubmitInput{}, err
	}

	offerPrice := 0
	if raw := strings.TrimSpace(input.OfferPrice.String()); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "offerPrice must be a whole number")
		}
		offerPrice = n
	}

	req := chrobinson.LoadOfferRequest{
		CarrierCode:  carrier,
		OfferPrice:   offerPrice,
		OfferNote:    strings.TrimSpace(input.OfferNote),
		CurrencyCode: strings.ToUpper(strings.TrimSpace(input.CurrencyCode)),
	}

	if req.CurrencyCode == "" {
		req.CurrencyCode = "USD"
	}
	if !isAlpha3Currency(req.CurrencyCode) {
		return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "currencyCode must be a 3-letter code")
	}
	if len(req.OfferNote) > maxOfferNoteLength {
		return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("offerNote exceeds max length of %d", maxOfferNoteLength))
	}
	if input.AvailableLoadCost != nil {
		if *input.AvailableLoadCost <= 0 {
			return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "availableLoadCost must be greater than 0 when provided")
		}
		req.AvailableLoadCost = *input.AvailableLoadCost
	}

	orderBidID := 0
	for _, raw := range []json.Number{input.OrderBidID, input.OrderBidIDCamel} {
		if strings.TrimSpace(raw.String()) == "" {
			continue
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(raw.String()))
		if convErr != nil {
			return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "order_bid_id must be an integer")
		}
		if orderBidID != 0 && n != 0 && orderBidID != n {
			return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "order_bid_id and orderBidId must match when both are provided")
		}
		if n != 0 {
			orderBidID = n
		}
	}
	if orderBidID < 0 {
		return parsedOfferSubmitInput{}, fiber.NewError(fiber.StatusBadRequest, "order_bid_id must be greater than or equal to 0")
	}

	return parsedOfferSubmitInput{
		Request:    req,
		OrderBidID: orderBidID,
	}, nil
}

func sanitizeCHRobErrorDetail(err error) string {
	detail := strings.TrimSpace(chrobinson.ErrorResponseBody(err))
	if detail == "" {
		return ""
	}
	if len(detail) > maxCHRobErrorDetailLength {
		return detail[:maxCHRobErrorDetailLength]
	}
	return detail
}

// chrobBookErrorResponse mirrors chrobOfferSubmitErrorResponse for bookings:
// relay CHRob's exact status and raw response body to the caller instead of a
// generic 500, so downstream (Loader) logs show precisely what CHRob said
// (e.g. 423 {"statusCode":423,"error":"Locked","message":"Book Locked"}).
func chrobBookErrorResponse(err error, publicError string) (int, fiber.Map) {
	status := chrobinson.ErrorStatusCode(err)
	detail := sanitizeCHRobErrorDetail(err)

	responseStatus := fiber.StatusInternalServerError
	if status >= 400 && status <= 599 {
		responseStatus = status
	}

	body := fiber.Map{
		"error": publicError,
	}
	if status > 0 {
		body["chrobStatus"] = status
	}
	if detail != "" {
		body["details"] = detail
	} else if msg := strings.TrimSpace(err.Error()); msg != "" {
		body["details"] = msg
	}
	return responseStatus, body
}

func chrobOfferSubmitErrorResponse(err error) (int, fiber.Map) {
	status := chrobinson.ErrorStatusCode(err)
	detail := sanitizeCHRobErrorDetail(err)

	responseStatus := fiber.StatusInternalServerError
	publicError := "Failed to process offer"
	switch status {
	case fiber.StatusBadRequest:
		responseStatus = fiber.StatusBadRequest
		publicError = "Bad request to CHRob API"
	case fiber.StatusUnauthorized:
		responseStatus = fiber.StatusUnauthorized
		publicError = "Unauthorized to CHRob API"
	case fiber.StatusForbidden:
		responseStatus = fiber.StatusForbidden
		publicError = "Forbidden by CHRob API"
	case fiber.StatusNotFound:
		responseStatus = fiber.StatusNotFound
		publicError = "Shipment not found in CHRob API"
	case fiber.StatusUnprocessableEntity:
		responseStatus = fiber.StatusUnprocessableEntity
		publicError = "CHRob API could not process the offer request"
	case fiber.StatusInternalServerError:
		responseStatus = fiber.StatusInternalServerError
		publicError = "CHRob API internal error"
	}

	body := fiber.Map{
		"error": publicError,
	}
	if status > 0 {
		body["chrobStatus"] = status
	}
	if detail != "" {
		body["details"] = detail
	} else {
		var fe *fiber.Error
		if errors.As(err, &fe) && strings.TrimSpace(fe.Message) != "" {
			body["details"] = fe.Message
		}
	}

	return responseStatus, body
}

// resolveCarrierAliases picks the carrier code from any of the accepted input
// keys: t_number (preferred), tNumber, or the legacy carrierCode. If more than
// one is provided they must all match; otherwise a 400 is returned so callers
// notice the ambiguity instead of silently losing one value.
func resolveCarrierAliases(tSnake, tCamel, carrierCode string) (string, error) {
	values := []string{}
	for _, v := range []string{tSnake, tCamel, carrierCode} {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		return "", nil
	}
	first := values[0]
	for _, v := range values[1:] {
		if v != first {
			return "", fiber.NewError(fiber.StatusBadRequest, "t_number, tNumber, and carrierCode must match when more than one is provided")
		}
	}
	return first, nil
}

// parseLoadNumber extracts a numeric CHRob load number from a raw JSON token
// that may be a JSON number (202237619), a quoted number ("202237619"), or the
// Loader's "CHROB-" prefixed order id ("CHROB-202237619"). The prefix match is
// case-insensitive. Returns (0, false) when the value is empty/absent or has no
// positive numeric load number.
func parseLoadNumber(raw json.RawMessage) (int, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return 0, false
	}
	// Unwrap a JSON string token ("...") to its inner value.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var unquoted string
		if err := json.Unmarshal(raw, &unquoted); err == nil {
			s = strings.TrimSpace(unquoted)
		}
	}
	// Strip a leading "CHROB-" (any case) if present.
	if len(s) >= 6 && strings.EqualFold(s[:6], "CHROB-") {
		s = s[6:]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
