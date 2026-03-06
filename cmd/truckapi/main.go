package main

import (
	"net/http"
	"os"
	"strings"
	"time"
	"truckapi/db"
	"truckapi/internal/auth"
	"truckapi/internal/chrobinson"
	"truckapi/internal/chrobrunner"
	"truckapi/internal/httpdebug"
	"truckapi/internal/routes"
	"truckapi/internal/truckstop"
	"truckapi/internal/uifeed"
	"truckapi/pkg/config"

	log "github.com/sirupsen/logrus"
)

func envTruthy(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func inferCHRobEnv(baseURL string) string {
	u := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(u, "sandbox"):
		return "sandbox"
	case strings.Contains(u, "api.navisphere.com"):
		return "production"
	default:
		return "custom"
	}
}

func main() {
	// Initialize TokenStore, HTTP Client, and APIClient
	tokenStore := auth.NewTokenStore()
	httpClient := &http.Client{
		Timeout:   30 * time.Second,
		Transport: httpdebug.NewTransport(http.DefaultTransport),
	}
	chrobBaseURL := config.GetEnv(config.CHRobBaseURL, "https://api.navisphere.com")
	apiClient := chrobinson.NewAPIClient(chrobBaseURL, tokenStore, httpClient)

	chrobEnv := strings.TrimSpace(config.GetEnv(config.CHRobEnv, ""))
	if chrobEnv == "" {
		chrobEnv = inferCHRobEnv(chrobBaseURL)
	}
	log.WithFields(log.Fields{
		"chrob_base_url": chrobBaseURL,
		"chrob_env":      chrobEnv,
	}).Info("CHRob API client configured")
	if chrobEnv == "production" {
		log.Warn("CHRob API base URL points to production")
	}

	// Save the environment variables to the .env file
	err := config.SaveEnv("./.env")
	if err != nil {
		log.Fatalf("Failed to save environment variables: %v", err)
	}

	// Start the periodic milestone updater
	// chrobinson.StartMilestoneUpdater(apiClient)

	// Split local SQLite and platform MySQL initialization so offer tracking can use
	// SQLite without forcing the platform DB connection in local/prototype runs.
	enableDatabases := envTruthy(config.EnableDatabases, false)
	enableSQLiteDB := envTruthy(config.EnableSQLiteDB, enableDatabases)
	enablePlatformDB := envTruthy(config.EnablePlatformDB, enableDatabases)
	if enableSQLiteDB {
		db.InitializeDatabase()
	}

	if enablePlatformDB {
		err = db.InitializePlatformDatabase()
		if err != nil {
			log.Fatalf("Failed to initialize platform database: %v", err)
		}
	}

	// Load Truckstop configuration
	// In-memory prototype feed for verifying payloads in the UI.
	feed := uifeed.NewStore(2000)

	// Start periodic Truckstop runner
	if envTruthy("ENABLE_TRUCKSTOP", false) {
		log.WithField("runner", "TRUCKSTOP").Info("Starting runner")
		truckstopConfig, err := config.LoadTruckstopConfig()
		if err != nil {
			log.WithError(err).WithField("runner", "TRUCKSTOP").Warn("Runner not started: missing config")
		} else {
			truckstopClient := truckstop.NewLoadSearchClient(truckstopConfig)
			truckstop.StartTruckstopRunner(truckstopClient, feed)
		}
	} else {
		log.WithField("runner", "TRUCKSTOP").Info("Runner disabled (set ENABLE_TRUCKSTOP=true to enable)")
	}

	// Start periodic CHRob runner
	if envTruthy("ENABLE_CHROB", true) {
		log.WithField("runner", "CHROBINSON").Info("Starting runner")
		chrobrunner.StartChrobRunner(apiClient, feed)
	}

	// Initialize Fiber app with API routes
	fiberApp := routes.InitializeRoutes(apiClient, feed)
	fiberApp.Static("/", "./public")

	// Start the Fiber server
	go func() {
		if err := fiberApp.Listen(config.GetEnv(config.ServerListenAddr, ":8081")); err != nil {
			log.WithError(err).Fatal("Failed to start the Fiber server")
		}
	}()

	// Block forever to keep the application running
	select {}
}
