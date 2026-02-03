package httpdebug

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

type Transport struct {
	Base            http.RoundTripper
	MaxBodyBytes    int
	LogSuccessful   bool
	RedactPatterns  []*regexp.Regexp
	RedactReplacers []string
}

func NewTransport(base http.RoundTripper) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}

	// Enable verbose logging for non-error responses only when explicitly requested.
	// Errors (status >= 400 or transport errors) are always logged.
	logSuccessful := false
	if v := strings.TrimSpace(os.Getenv("HTTP_DEBUG")); v == "1" || strings.EqualFold(v, "true") {
		logSuccessful = true
	}

	return &Transport{
		Base:          base,
		MaxBodyBytes:  32 * 1024,
		LogSuccessful: logSuccessful,
		RedactPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(Authorization:\s*)([^\r\n]+)`),
			regexp.MustCompile(`(?i)(X-API-KEY:\s*)([^\r\n]+)`),
			regexp.MustCompile(`(?i)(\"password\"\\s*:\\s*\")([^\"]+)(\")`),
			regexp.MustCompile(`(?i)(\"client_secret\"\\s*:\\s*\")([^\"]+)(\")`),
			regexp.MustCompile(`(?i)(\"access_token\"\\s*:\\s*\")([^\"]+)(\")`),
			// SOAP/XML credentials (with or without namespace prefixes).
			regexp.MustCompile(`(?i)(<[^>]*:?Password>)([^<]+)(</[^>]*:?Password>)`),
			regexp.MustCompile(`(?i)(<[^>]*:?UserName>)([^<]+)(</[^>]*:?UserName>)`),
		},
		RedactReplacers: []string{
			`${1}***REDACTED***`,
			`${1}***REDACTED***`,
			`${1}***REDACTED***${3}`,
			`${1}***REDACTED***${3}`,
			`${1}***REDACTED***${3}`,
			`${1}***REDACTED***${3}`,
			`${1}***REDACTED***${3}`,
		},
	}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	reqBody, reqBodyTruncated := peekAndRestoreBody(&req.Body, t.MaxBodyBytes)
	reqHeaders := redactText(headersToText(req.Header), t.RedactPatterns, t.RedactReplacers)
	redactedReqBody := redactText(reqBody, t.RedactPatterns, t.RedactReplacers)

	resp, err := t.base().RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"method":             req.Method,
			"url":                req.URL.String(),
			"duration_ms":        elapsed.Milliseconds(),
			"req_headers":        reqHeaders,
			"req_body":           redactedReqBody,
			"req_body_truncated": reqBodyTruncated,
		}).Error("HTTP request failed")
		return nil, err
	}

	respBody, respBodyTruncated := peekAndRestoreBody(&resp.Body, t.MaxBodyBytes)
	redactedRespBody := redactText(respBody, t.RedactPatterns, t.RedactReplacers)

	shouldLog := resp.StatusCode >= 400 || t.LogSuccessful
	if shouldLog {
		entry := log.WithFields(log.Fields{
			"method":              req.Method,
			"url":                 req.URL.String(),
			"status":              resp.StatusCode,
			"duration_ms":         elapsed.Milliseconds(),
			"req_headers":         reqHeaders,
			"req_body":            redactedReqBody,
			"req_body_truncated":  reqBodyTruncated,
			"resp_headers":        redactText(headersToText(resp.Header), t.RedactPatterns, t.RedactReplacers),
			"resp_body":           redactedRespBody,
			"resp_body_truncated": respBodyTruncated,
		})

		if resp.StatusCode >= 400 {
			entry.Error("HTTP response error")
		} else {
			entry.Debug("HTTP response")
		}
	}

	return resp, nil
}

func (t *Transport) base() http.RoundTripper {
	if t.Base == nil {
		return http.DefaultTransport
	}
	return t.Base
}

type restoreReadCloser struct {
	r io.Reader
	c io.Closer
}

func (r restoreReadCloser) Read(p []byte) (int, error) { return r.r.Read(p) }
func (r restoreReadCloser) Close() error               { return r.c.Close() }

// peekAndRestoreBody reads up to maxBytes+1 bytes from the body for logging purposes,
// then restores the stream so the caller still receives the full, unmodified body.
func peekAndRestoreBody(bodyPtr *io.ReadCloser, maxBytes int) (string, bool) {
	if bodyPtr == nil || *bodyPtr == nil || *bodyPtr == http.NoBody || maxBytes <= 0 {
		return "", false
	}

	original := *bodyPtr
	limited := &io.LimitedReader{R: original, N: int64(maxBytes + 1)}
	consumed, err := io.ReadAll(limited)
	if err != nil {
		// Restore whatever remains (best-effort).
		*bodyPtr = original
		return "", false
	}

	truncated := len(consumed) > maxBytes
	preview := consumed
	if truncated {
		preview = consumed[:maxBytes]
	}

	// Put the consumed bytes back in front of the remaining body stream.
	*bodyPtr = restoreReadCloser{
		r: io.MultiReader(bytes.NewReader(consumed), original),
		c: original,
	}

	return string(preview), truncated
}

func headersToText(h http.Header) string {
	var sb strings.Builder
	for k, vals := range h {
		for _, v := range vals {
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(v)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func redactText(s string, patterns []*regexp.Regexp, replacers []string) string {
	if s == "" {
		return s
	}
	out := s
	for i, re := range patterns {
		if i < len(replacers) {
			out = re.ReplaceAllString(out, replacers[i])
		}
	}
	return out
}
