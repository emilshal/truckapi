package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gorm.io/gorm"
)

// Constants for environment variable keys
const (
	CHRobClientID            = "CHROB_CLIENT_ID"
	CHRobClientSecret        = "CHROB_CLIENT_SECRET"
	CHRobBaseURL             = "CHROB_API_BASE_URL"
	CHRobTokenUrl            = "CHROB_TOKEN_URL"
	CHRobAudience            = "CHROB_AUDIENCE"
	CHRobGrantType           = "CHROB_GRANT_TYPE"
	ServerListenAddr         = "SERVER_LISTEN_ADDR"
	CHRobAccessToken         = "CHROB_ACCESS_TOKEN"
	CHRobCarrierCode         = "CHROB_CARRIER_CODE"
	CHRobEnv                 = "CHROB_ENV"
	CHRobCallbackBearerToken = "CHROB_CALLBACK_BEARER_TOKEN"
	CHRobCallbackAllowAPIKey = "CHROB_CALLBACK_ALLOW_API_KEY"
	APIKey                   = "API_KEY"
	OpenAIAPIKey             = "OPENAI_API_KEY"
	EnableDatabases          = "ENABLE_DATABASES"
	EnableSQLiteDB           = "ENABLE_SQLITE_DB"
	EnablePlatformDB         = "ENABLE_PLATFORM_DB"
	BidIdempotencyTTLMinutes = "BID_IDEMPOTENCY_TTL_MINUTES"
	// 🔧 Truckstop config
	TruckstopUsername      = "TRUCKSTOP_USERNAME"
	TruckstopPassword      = "TRUCKSTOP_PASSWORD"
	TruckstopIntegrationID = "TRUCKSTOP_INTEGRATION_ID"
	TruckstopLoadSearchURL = "TRUCKSTOP_LOAD_SEARCH_URL"
	LoaderAPIBaseURL       = "LOADER_API_BASE_URL"
	LoaderOrdersBaseURL    = "LOADER_ORDERS_BASE_URL"
	LoaderAPIKey           = "LOADER_API_KEY"
	LoaderPostWorkers      = "LOADER_POST_WORKERS"
	LoaderPostMaxRetries   = "LOADER_POST_MAX_RETRIES"
)

var (
	Env              map[string]string
	DB               *gorm.DB
	DefaultEnvValues = map[string]string{
		"CHROB_CLIENT_ID":        "0oas6jwy40YOo5T4g357",
		"CHROB_CLIENT_SECRET":    "_a67bEwQ-gTeS5FP0TnYn2iJXe1xefNnn3RSw7Wz",
		"CHROB_API_BASE_URL":     "https://api.navisphere.com",                // ✅ prod
		"CHROB_TOKEN_URL":        "https://api.navisphere.com/v1/oauth/token", // ✅ prod
		"CHROB_AUDIENCE":         "https://inavisphere.chrobinson.com",
		"CHROB_GRANT_TYPE":       "client_credentials",
		"SERVER_LISTEN_ADDR":     ":8081",
		"CHROB_ACCESS_TOKEN":     "",
		"CHROB_CARRIER_CODE":     "T6263835",
		CHRobEnv:                 "",
		CHRobCallbackBearerToken: "",
		CHRobCallbackAllowAPIKey: "true",
		"API_KEY":                "",
		"OPENAI_API_KEY":         "",
		EnableDatabases:          "false",
		EnableSQLiteDB:           "",
		EnablePlatformDB:         "",
		BidIdempotencyTTLMinutes: "60",
		LoaderAPIBaseURL:         "https://core.hfield.net",
		LoaderOrdersBaseURL:      "",
		LoaderAPIKey:             "loaderBMwuIUZKtyH8fetLykDch07dxfciUZZ8lrGqOfmVaAjnXAhcwIRIdBCyhg",
		LoaderPostWorkers:        "16",
		LoaderPostMaxRetries:     "3",
	}

	EnvKeys = []string{
		"CHROB_CLIENT_ID",
		"CHROB_CLIENT_SECRET",
		"CHROB_API_BASE_URL",
		"CHROB_TOKEN_URL",
		"CHROB_AUDIENCE",
		"CHROB_GRANT_TYPE",
		"SERVER_LISTEN_ADDR",
		"CHROB_ACCESS_TOKEN",
		"CHROB_CARRIER_CODE",
		CHRobEnv,
		CHRobCallbackBearerToken,
		CHRobCallbackAllowAPIKey,
		"API_KEY",
		"OPENAI_API_KEY",
		EnableDatabases,
		EnableSQLiteDB,
		EnablePlatformDB,
		BidIdempotencyTTLMinutes,
		TruckstopUsername,
		TruckstopPassword,
		TruckstopIntegrationID,
		TruckstopLoadSearchURL,
		LoaderAPIBaseURL,
		LoaderOrdersBaseURL,
		LoaderAPIKey,
		LoaderPostWorkers,
		LoaderPostMaxRetries,
	}

	EnvFilePath string
)

type TruckstopConfig struct {
	Username      string
	Password      string
	IntegrationID int
	LoadSearchURL string
}

func LoadTruckstopConfig() (*TruckstopConfig, error) {
	username := GetEnv(TruckstopUsername, "")
	password := GetEnv(TruckstopPassword, "")
	integrationIDStr := GetEnv(TruckstopIntegrationID, "")
	loadSearchURL := GetEnv(TruckstopLoadSearchURL, "")

	missing := make([]string, 0, 4)
	if username == "" {
		missing = append(missing, TruckstopUsername)
	}
	if password == "" {
		missing = append(missing, TruckstopPassword)
	}
	if integrationIDStr == "" {
		missing = append(missing, TruckstopIntegrationID)
	}
	if loadSearchURL == "" {
		missing = append(missing, TruckstopLoadSearchURL)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required truckstop environment variables: %s", strings.Join(missing, ", "))
	}

	integrationID, err := strconv.Atoi(integrationIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid TRUCKSTOP_INTEGRATION_ID: %w", err)
	}

	return &TruckstopConfig{
		Username:      username,
		Password:      password,
		IntegrationID: integrationID,
		LoadSearchURL: loadSearchURL,
	}, nil
}

func init() {
	// Initialize the Env map
	Env = make(map[string]string)

	projectRoot, err := findProjectRoot()
	if err != nil {
		log.WithError(err).Fatal("Failed to determine project root")
	}

	// Change to project root directory
	err = os.Chdir(projectRoot)
	if err != nil {
		log.WithError(err).Fatal("Failed to change working directory to project root")
	}

	EnvFilePath = filepath.Join(projectRoot, ".env")

	// Initialize the API key
	InitializeAPIKey(EnvFilePath)

	// Load environment variables
	loadEnvironmentVariables()

	// Initialize logging after loading environment variables
	initializeLogging()

}

// findProjectRoot determines the project root by locating the go.mod file
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			return "", fmt.Errorf("project root not found")
		}
		dir = parentDir
	}
}

func loadEnvironmentVariables() {
	Env = make(map[string]string) // Initialize the map for environment variables

	// Set default values for all environment variables
	for key, val := range DefaultEnvValues {
		Env[key] = val
	}

	// Load variables from .env file, overriding defaults if present
	err := godotenv.Load(EnvFilePath)
	if err != nil {
		log.Infof("No .env file found or error loading: %s", err)
	} else {
		log.Info("Environment variables loaded from .env file")
	}

	// Overwrite with system environment variables if present
	for _, key := range EnvKeys {
		if val, exists := os.LookupEnv(key); exists {
			Env[key] = val
		}
	}

	// // Save environment variables to ensure they are persisted
	// err = SaveEnv(EnvFilePath)
	// if err != nil {
	// 	log.WithError(err).Fatal("Failed to save environment variables")
	// }
}

// Set up logging configuration
func initializeLogging() {
	logDir := filepath.Join(filepath.Dir(EnvFilePath), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		// Fall back to stderr-only logging if we can't create the dir.
		log.WithError(err).Warn("Failed to create logs directory; using stderr only")
		log.SetOutput(os.Stderr)
	} else {
		logFile := GetEnv("LOG_FILE", filepath.Join(logDir, "app.log"))
		rotate := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    25, // MB
			MaxBackups: 10,
			MaxAge:     14, // days
			Compress:   true,
		}

		// Allow overrides.
		if v := strings.TrimSpace(GetEnv("LOG_MAX_SIZE_MB", "")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				rotate.MaxSize = n
			}
		}
		if v := strings.TrimSpace(GetEnv("LOG_MAX_BACKUPS", "")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				rotate.MaxBackups = n
			}
		}
		if v := strings.TrimSpace(GetEnv("LOG_MAX_AGE_DAYS", "")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				rotate.MaxAge = n
			}
		}
		if v := strings.TrimSpace(GetEnv("LOG_COMPRESS", "")); v != "" {
			switch strings.ToLower(v) {
			case "1", "true", "yes", "y", "on":
				rotate.Compress = true
			case "0", "false", "no", "n", "off":
				rotate.Compress = false
			}
		}

		log.SetOutput(io.MultiWriter(os.Stderr, rotate))
	}
	log.SetFormatter(&log.JSONFormatter{})

	switch strings.ToLower(strings.TrimSpace(GetEnv("LOG_LEVEL", ""))) {
	case "trace":
		log.SetLevel(log.TraceLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "warn", "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	case "fatal":
		log.SetLevel(log.FatalLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}

	log.WithFields(log.Fields{
		"log_file": GetEnv("LOG_FILE", filepath.Join(filepath.Join(filepath.Dir(EnvFilePath), "logs"), "app.log")),
	}).Info("Logging initialized to stderr + rotating file")
}

// Utility to get environment variables with a fallback default
func GetEnv(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}

	if val, ok := Env[key]; ok {
		return val
	}
	return def
}

// Set a single environment variable in the map
func SetEnv(key, value string) {
	Env[key] = value
}

// Save all managed environment variables to the .env file
func SaveEnv(envFile string) error {
	file, err := os.Create(envFile) // Create or truncate .env file
	if err != nil {
		log.Fatalf("Failed to open or create .env file: %v", err)
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file) // Use buffered writer for efficiency
	for _, key := range EnvKeys {
		value, exists := Env[key]
		if !exists {
			value = "" // Use empty string if key is not set
		}
		_, err := writer.WriteString(key + "=" + value + "\n") // Write each key-value pair
		if err != nil {
			log.Fatalf("Failed to write to .env file: %v", err)
			return err
		}
	}
	err = writer.Flush() // Flush to ensure all data is written to file
	if err != nil {
		log.Fatalf("Failed to flush data to .env file: %v", err)
		return err
	}
	log.Println("Environment variables successfully saved to .env file")
	return nil
}

// GenerateAPIKey generates a new API key.
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 16) // 16 bytes will give you a 32-character key
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// WriteAPIKeyToEnvFile writes the generated API key to the .env file.
func WriteAPIKeyToEnvFile(apiKey string, envFilePath string) error {
	err := godotenv.Load(envFilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error loading .env file: %w", err)
	}

	envMap, err := godotenv.Read(envFilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error reading .env file: %w", err)
	}

	envMap["API_KEY"] = apiKey
	envFile, err := os.Create(envFilePath)
	if err != nil {
		return fmt.Errorf("error creating .env file: %w", err)
	}
	defer envFile.Close()

	for key, value := range envMap {
		_, err := fmt.Fprintf(envFile, "%s=%s\n", key, value)
		if err != nil {
			return fmt.Errorf("error writing to .env file: %w", err)
		}
	}

	return nil
}

// InitializeAPIKey initializes the API key by generating a new one and writing it to the .env file if it doesn't already exist.
func InitializeAPIKey(envFilePath string) {
	err := godotenv.Load(envFilePath)
	if err == nil {
		if apiKey := os.Getenv("API_KEY"); apiKey != "" {
			log.Println("API key already exists in .env file")
			return
		}
	}

	apiKey, err := GenerateAPIKey()
	if err != nil {
		log.Fatalf("Error generating API key: %v", err)
	}
	fmt.Println("Generated API key:", apiKey)

	err = WriteAPIKeyToEnvFile(apiKey, envFilePath)
	if err != nil {
		log.Fatalf("Error writing API key to .env file: %v", err)
	}
	fmt.Println("API key written to .env file")
}
