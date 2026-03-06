package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
	"truckapi/internal/httpdebug"
	"truckapi/pkg/config"

	log "github.com/sirupsen/logrus"
)

type APIClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("loader api returned %d: %s", e.StatusCode, e.Body)
}

func loaderSuccessBodyLoggingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(config.GetEnv("LOADER_LOG_SUCCESS_BODY", ""))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func loader2xxBodyExplicitFailure(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}

	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return false
	}

	if v, ok := obj["success"].(bool); ok && !v {
		return true
	}
	if v, ok := obj["ok"].(bool); ok && !v {
		return true
	}
	if v, ok := obj["status"].(string); ok && strings.EqualFold(strings.TrimSpace(v), "error") {
		return true
	}
	return false
}

func NewAPIClient(baseURL, apiKey string, httpClient *http.Client) *APIClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	if httpClient.Transport == nil {
		httpClient.Transport = httpdebug.NewTransport(http.DefaultTransport)
	}
	return &APIClient{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: httpClient,
	}
}

func NewAPIClientFromEnv(httpClient *http.Client) *APIClient {
	baseURL := strings.TrimSpace(config.GetEnv(config.LoaderOrdersBaseURL, ""))
	if baseURL == "" {
		baseURL = config.GetEnv(config.LoaderAPIBaseURL, "https://core.hfield.net")
	}
	return NewAPIClient(
		baseURL,
		config.GetEnv(config.LoaderAPIKey, ""),
		httpClient,
	)
}

func (client *APIClient) CreateOrder(order LoaderOrder) error {
	if client == nil {
		return fmt.Errorf("loader client is nil")
	}
	if client.BaseURL == "" {
		return fmt.Errorf("loader base url is empty")
	}
	if strings.TrimSpace(client.APIKey) == "" {
		return fmt.Errorf("loader api key is empty")
	}

	payload, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("marshal loader order: %w", err)
	}

	url := client.BaseURL + "/api/v1/loader/orders"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build loader create order request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", client.APIKey)

	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("loader create order request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	if loader2xxBodyExplicitFailure(body) {
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	if loaderSuccessBodyLoggingEnabled() {
		if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			log.WithFields(log.Fields{
				"source":       order.Source,
				"orderNumber":  order.OrderNumber,
				"status_code":  resp.StatusCode,
				"loaderApiUrl": url,
				"resp_body":    trimmed,
			}).Info("Loader API success response body")
		}
	}

	// Per-order success logs can be extremely noisy at scale; keep them at debug.
	log.WithFields(log.Fields{
		"source":       order.Source,
		"orderNumber":  order.OrderNumber,
		"status_code":  resp.StatusCode,
		"loaderApiUrl": url,
	}).Debug("✅ Posted order to Loader API")

	return nil
}

type PostPool struct {
	Client     *APIClient
	Workers    int
	MaxRetries int

	QueueSize int
}

type PostResult struct {
	Index int
	Order LoaderOrder
	Err   error
}

func (p PostPool) postWithRetry(ctx context.Context, order LoaderOrder) error {
	maxRetries := p.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	baseDelay := 250 * time.Millisecond
	for attempt := 0; ; attempt++ {
		attemptStart := time.Now()
		err := p.Client.CreateOrder(order)
		if err == nil {
			log.WithFields(log.Fields{
				"orderNumber": order.OrderNumber,
				"source":      order.Source,
				"attempt":     attempt + 1,
				"duration_ms": time.Since(attemptStart).Milliseconds(),
			}).Debug("Loader post succeeded")
			return nil
		}

		// Decide if we should retry.
		retry := false
		statusCode := 0
		if apiErr, ok := err.(*APIError); ok {
			statusCode = apiErr.StatusCode
			if apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500 {
				retry = true
			}
		} else {
			// Network / unknown errors: retry.
			retry = true
		}

		if !retry || attempt >= maxRetries {
			log.WithError(err).WithFields(log.Fields{
				"orderNumber": order.OrderNumber,
				"source":      order.Source,
				"attempt":     attempt + 1,
				"status_code": statusCode,
				"retry":       retry,
			}).Error("Loader post failed (giving up)")
			return err
		}

		// Exponential backoff with jitter.
		delay := baseDelay * time.Duration(1<<attempt)
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		jitter := time.Duration(rand.Int63n(int64(delay / 3)))
		sleep := delay - jitter

		log.WithError(err).WithFields(log.Fields{
			"orderNumber": order.OrderNumber,
			"source":      order.Source,
			"attempt":     attempt + 1,
			"status_code": statusCode,
			"sleep_ms":    sleep.Milliseconds(),
		}).Warn("Loader post failed; retrying")

		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).WithFields(log.Fields{
				"orderNumber": order.OrderNumber,
				"source":      order.Source,
				"attempt":     attempt + 1,
			}).Error("Loader post canceled by context")
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

// PostAll concurrently posts orders from `orders` and blocks until all have been processed.
// It returns the number of successful posts and a slice of errors (one per failed post).
func (p PostPool) PostAll(ctx context.Context, orders []LoaderOrder) (int, []error) {
	results := p.PostAllDetailed(ctx, orders)
	var okCount int
	var errs []error
	for _, r := range results {
		if r.Err != nil {
			errs = append(errs, r.Err)
			continue
		}
		okCount++
	}
	return okCount, errs
}

// PostAllDetailed concurrently posts orders and returns per-order outcomes.
func (p PostPool) PostAllDetailed(ctx context.Context, orders []LoaderOrder) []PostResult {
	if p.Client == nil {
		return []PostResult{{Err: fmt.Errorf("post pool client is nil")}}
	}
	workers := p.Workers
	if workers <= 0 {
		if s := strings.TrimSpace(config.GetEnv(config.LoaderPostWorkers, "")); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				workers = n
			}
		}
		if workers <= 0 {
			workers = 16
		}
	}

	maxRetries := p.MaxRetries
	if maxRetries == 0 {
		if s := strings.TrimSpace(config.GetEnv(config.LoaderPostMaxRetries, "")); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				maxRetries = n
			}
		}
	}

	queueSize := p.QueueSize
	if queueSize <= 0 {
		queueSize = workers * 50
	}

	type result struct {
		index int
		order LoaderOrder
		err   error
	}

	type job struct {
		index int
		order LoaderOrder
	}

	jobs := make(chan job, queueSize)
	results := make(chan result, queueSize)

	worker := func() {
		for item := range jobs {
			results <- result{
				index: item.index,
				order: item.order,
				err:   PostPool{Client: p.Client, MaxRetries: maxRetries}.postWithRetry(ctx, item.order),
			}
		}
	}

	start := time.Now()
	log.WithFields(log.Fields{
		"orders":      len(orders),
		"workers":     workers,
		"queue_size":  queueSize,
		"max_retries": maxRetries,
	}).Info("Loader post batch start")

	for i := 0; i < workers; i++ {
		go worker()
	}

	go func() {
		defer close(jobs)
		for i, o := range orders {
			select {
			case <-ctx.Done():
				return
			case jobs <- job{index: i, order: o}:
			}
		}
	}()

	outcomes := make([]PostResult, 0, len(orders))
	for i := 0; i < len(orders); i++ {
		select {
		case <-ctx.Done():
			var okCount int
			var errCount int
			for _, o := range outcomes {
				if o.Err != nil {
					errCount++
				} else {
					okCount++
				}
			}
			log.WithError(ctx.Err()).WithFields(log.Fields{
				"ok":          okCount,
				"failed":      errCount,
				"total":       len(orders),
				"duration_ms": time.Since(start).Milliseconds(),
			}).Error("Loader post batch canceled")
			return append(outcomes, PostResult{Err: ctx.Err()})
		case r := <-results:
			outcomes = append(outcomes, PostResult{
				Index: r.index,
				Order: r.order,
				Err:   r.err,
			})
		}
	}

	var okCount int
	var errCount int
	for _, o := range outcomes {
		if o.Err != nil {
			errCount++
		} else {
			okCount++
		}
	}
	log.WithFields(log.Fields{
		"ok":          okCount,
		"failed":      errCount,
		"total":       len(orders),
		"duration_ms": time.Since(start).Milliseconds(),
	}).Info("Loader post batch complete")

	return outcomes
}
