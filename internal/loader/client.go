package loader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
		return fmt.Errorf("loader create order returned %d: %s", resp.StatusCode, string(body))
	}

	log.WithFields(log.Fields{
		"source":       order.Source,
		"orderNumber":  order.OrderNumber,
		"status_code":  resp.StatusCode,
		"loaderApiUrl": url,
	}).Info("✅ Posted order to Loader API")

	return nil
}
