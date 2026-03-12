package db

import (
	"fmt"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type ChrobLoaderAudit struct {
	ID               uint      `gorm:"primaryKey"`
	OccurredAt       time.Time `gorm:"index"`
	Action           string    `gorm:"index"`
	Reason           string
	Source           string `gorm:"index"`
	LoadNumber       int    `gorm:"index"`
	OrderNumber      string `gorm:"index"`
	DedupeKey        string `gorm:"index"`
	OriginCity       string `gorm:"index"`
	OriginState      string
	OriginZip        string `gorm:"index"`
	DestinationCity  string
	DestinationState string
	DestinationZip   string
	SearchLat        float64 `gorm:"index"`
	SearchLng        float64 `gorm:"index"`
	PageIndex        int     `gorm:"index"`
}

func getChrobLoaderAuditDB() (*gorm.DB, error) {
	if DB != nil {
		return DB, nil
	}

	dbPath := sqliteDBPath()
	gdb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite for chrob loader audit: %w", err)
	}
	return gdb, nil
}

func ChrobInsertLoaderAuditBatch(rows []ChrobLoaderAudit) error {
	if len(rows) == 0 {
		return nil
	}

	gdb, err := getChrobLoaderAuditDB()
	if err != nil {
		return err
	}

	cleaned := make([]ChrobLoaderAudit, 0, len(rows))
	for _, row := range rows {
		row.Action = strings.TrimSpace(row.Action)
		if row.Action == "" {
			continue
		}
		if row.OccurredAt.IsZero() {
			row.OccurredAt = time.Now().UTC()
		}
		if strings.TrimSpace(row.Source) == "" {
			row.Source = "CHROBINSON"
		}
		cleaned = append(cleaned, row)
	}
	if len(cleaned) == 0 {
		return nil
	}

	if err := gdb.Create(&cleaned).Error; err != nil {
		return fmt.Errorf("insert chrob loader audit rows: %w", err)
	}

	log.WithField("rows", len(cleaned)).Debug("Inserted CHRob loader audit rows")
	return nil
}
