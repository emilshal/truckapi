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
	return NewAPIClient(
		config.GetEnv(config.LoaderAPIBaseURL, "https://core.hfield.net"),
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	log.WithFields(log.Fields{
		"source":       order.Source,
		"orderNumber":  order.OrderNumber,
		"status_code":  resp.StatusCode,
		"loaderApiUrl": url,
	}).Info("✅ Posted order to Loader API")

	return nil
}

type PostPool struct {
	Client     *APIClient
	Workers    int
	MaxRetries int

	QueueSize int
}

func (p PostPool) postWithRetry(ctx context.Context, order LoaderOrder) error {
	maxRetries := p.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	baseDelay := 250 * time.Millisecond
	for attempt := 0; ; attempt++ {
		err := p.Client.CreateOrder(order)
		if err == nil {
			return nil
		}

		// Decide if we should retry.
		retry := false
		if apiErr, ok := err.(*APIError); ok {
			if apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500 {
				retry = true
			}
		} else {
			// Network / unknown errors: retry.
			retry = true
		}

		if !retry || attempt >= maxRetries {
			return err
		}

		// Exponential backoff with jitter.
		delay := baseDelay * time.Duration(1<<attempt)
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
		jitter := time.Duration(rand.Int63n(int64(delay / 3)))
		sleep := delay - jitter

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

// PostAll concurrently posts orders from `orders` and blocks until all have been processed.
// It returns the number of successful posts and a slice of errors (one per failed post).
func (p PostPool) PostAll(ctx context.Context, orders []LoaderOrder) (int, []error) {
	if p.Client == nil {
		return 0, []error{fmt.Errorf("post pool client is nil")}
	}
	workers := p.Workers
	if workers <= 0 {
		if s := strings.TrimSpace(config.GetEnv(config.LoaderPostWorkers, "")); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				workers = n
			}
		}
		if workers <= 0 {
			workers = 8
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
		err error
	}

	jobs := make(chan LoaderOrder, queueSize)
	results := make(chan result, queueSize)

	worker := func() {
		for order := range jobs {
			results <- result{err: PostPool{Client: p.Client, MaxRetries: maxRetries}.postWithRetry(ctx, order)}
		}
	}

	for i := 0; i < workers; i++ {
		go worker()
	}

	go func() {
		defer close(jobs)
		for _, o := range orders {
			select {
			case <-ctx.Done():
				return
			case jobs <- o:
			}
		}
	}()

	var okCount int
	var errs []error
	for i := 0; i < len(orders); i++ {
		select {
		case <-ctx.Done():
			return okCount, append(errs, ctx.Err())
		case r := <-results:
			if r.err != nil {
				errs = append(errs, r.err)
			} else {
				okCount++
			}
		}
	}

	return okCount, errs
}
