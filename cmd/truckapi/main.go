package main

import (
	"net/http"
	"time"
	"truckapi/db"
	"truckapi/internal/auth"
	"truckapi/internal/chrobinson"
	"truckapi/internal/chrobrunner"
	"truckapi/internal/routes"
	"truckapi/internal/truckstop"
	"truckapi/pkg/config"

	log "github.com/sirupsen/logrus"
)

func main() {
	// Initialize TokenStore, HTTP Client, and APIClient
	tokenStore := auth.NewTokenStore()
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	apiClient := chrobinson.NewAPIClient(config.GetEnv(config.CHRobBaseURL, "https://api.navisphere.com"), tokenStore, httpClient)

	// Save the environment variables to the .env file
	err := config.SaveEnv("./.env")
	if err != nil {
		log.Fatalf("Failed to save environment variables: %v", err)
	}

	// Start the periodic milestone updater
	// chrobinson.StartMilestoneUpdater(apiClient)

	// Initialize the SQLite database
	db.InitializeDatabase()

	// Initialize the MySQL database
	err = db.InitializePlatformDatabase()
	if err != nil {
		log.Fatalf("Failed to initialize platform database: %v", err)
	}

	// Load Truckstop configuration
	truckstopConfig, err := config.LoadTruckstopConfig()
	if err != nil {
		log.WithError(err).Fatal("❌ Failed to load Truckstop config")
	}

	// Initialize Truckstop SOAP client
	truckstopClient := truckstop.NewLoadSearchClient(truckstopConfig)

	// Start periodic Truckstop runner
	truckstop.StartTruckstopRunner(truckstopClient)

	// Start periodic CHRob runner
	chrobrunner.StartChrobRunner(apiClient)

	// Initialize Fiber app with API routes
	fiberApp := routes.InitializeRoutes(apiClient)
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
