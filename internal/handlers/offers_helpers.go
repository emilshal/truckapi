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
	CarrierCode       string `json:"carrierCode"`
	OfferPrice        int    `json:"offerPrice"`
	OfferNote         string `json:"offerNote"`
	CurrencyCode      string `json:"currencyCode"`
	AvailableLoadCost *int   `json:"availableLoadCost"`
}

type offerSubmitResponse struct {
	Message          string `json:"message"`
	LoadNumber       string `json:"loadNumber"`
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

func validateAndBuildOfferRequest(raw []byte) (chrobinson.LoadOfferRequest, error) {
	var input offerRequestInput
	if err := decodeStrictJSON(raw, &input); err != nil {
		return chrobinson.LoadOfferRequest{}, fiber.NewError(fiber.StatusBadRequest, "Invalid request data")
	}

	req := chrobinson.LoadOfferRequest{
		CarrierCode:  strings.TrimSpace(input.CarrierCode),
		OfferPrice:   input.OfferPrice,
		OfferNote:    strings.TrimSpace(input.OfferNote),
		CurrencyCode: strings.ToUpper(strings.TrimSpace(input.CurrencyCode)),
	}

	if req.CurrencyCode == "" {
		req.CurrencyCode = "USD"
	}
	if !isAlpha3Currency(req.CurrencyCode) {
		return chrobinson.LoadOfferRequest{}, fiber.NewError(fiber.StatusBadRequest, "currencyCode must be a 3-letter code")
	}
	if len(req.OfferNote) > maxOfferNoteLength {
		return chrobinson.LoadOfferRequest{}, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("offerNote exceeds max length of %d", maxOfferNoteLength))
	}
	if input.AvailableLoadCost != nil {
		if *input.AvailableLoadCost <= 0 {
			return chrobinson.LoadOfferRequest{}, fiber.NewError(fiber.StatusBadRequest, "availableLoadCost must be greater than 0 when provided")
		}
		req.AvailableLoadCost = *input.AvailableLoadCost
	}

	return req, nil
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
