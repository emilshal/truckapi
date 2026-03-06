package db

import (
	"os"
	"strings"
)

const sqliteDBPathEnv = "TRUCKAPI_SQLITE_PATH"

func sqliteDBPath() string {
	if path := strings.TrimSpace(os.Getenv(sqliteDBPathEnv)); path != "" {
		return path
	}
	return "truckapi.db"
}
