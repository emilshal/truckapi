package chrobinson

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// HTTPStatusError preserves an upstream CHRob HTTP status/body so handlers can
// return accurate status codes and useful diagnostics to callers.
type HTTPStatusError struct {
	StatusCode int
	Operation  string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	op := strings.TrimSpace(e.Operation)
	if op == "" {
		op = "chrob api request"
	}
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("%s failed with status code %d", op, e.StatusCode)
	}
	return fmt.Sprintf("%s failed with status code %d: %s", op, e.StatusCode, body)
}

func (e *HTTPStatusError) HTTPStatusCode() int {
	if e == nil {
		return 0
	}
	return e.StatusCode
}

func (e *HTTPStatusError) ResponseBody() string {
	if e == nil {
		return ""
	}
	return e.Body
}

// ErrorStatusCode extracts an HTTP status code from known error types.
func ErrorStatusCode(err error) int {
	if err == nil {
		return 0
	}

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return fiberErr.Code
	}

	type statusCoder interface {
		HTTPStatusCode() int
	}
	var sc statusCoder
	if errors.As(err, &sc) {
		return sc.HTTPStatusCode()
	}

	return 0
}

// ErrorResponseBody extracts an upstream response body from known error types.
func ErrorResponseBody(err error) string {
	if err == nil {
		return ""
	}

	type responseBodyer interface {
		ResponseBody() string
	}
	var rb responseBodyer
	if errors.As(err, &rb) {
		return rb.ResponseBody()
	}

	return ""
}

// APIErrorSchema represents CHRob's documented error payload shape.
type APIErrorSchema struct {
	StatusCode int    `json:"statusCode"`
	Error      string `json:"error"`
	Message    string `json:"message"`
}

// ParseAPIErrorSchema parses a CHRob error JSON payload.
func ParseAPIErrorSchema(raw string) (APIErrorSchema, bool) {
	var out APIErrorSchema
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return out, false
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return out, false
	}
	if out.StatusCode == 0 && strings.TrimSpace(out.Error) == "" && strings.TrimSpace(out.Message) == "" {
		return out, false
	}
	return out, true
}

// ParseAPIErrorSchemaFromError parses CHRob error schema fields from an error value.
func ParseAPIErrorSchemaFromError(err error) (APIErrorSchema, bool) {
	return ParseAPIErrorSchema(ErrorResponseBody(err))
}
