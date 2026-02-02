package truckstop

import (
	"net/http"
	"time"
	"truckapi/pkg/config"
)

// LoadSearchClient wraps your config and HTTP client
type LoadSearchClient struct {
	Username      string
	Password      string
	IntegrationID int
	LoadSearchURL string
	Client        *http.Client
}

func NewLoadSearchClient(cfg *config.TruckstopConfig) *LoadSearchClient {
	return &LoadSearchClient{
		Username:      cfg.Username,
		Password:      cfg.Password,
		IntegrationID: cfg.IntegrationID,
		LoadSearchURL: cfg.LoadSearchURL,
		Client:        &http.Client{Timeout: 10 * time.Second},
	}
}
