package db

import (
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ChrobSentDedupe struct {
	Key         string    `gorm:"primaryKey;size:191"`
	OrderNumber string    `gorm:"index"`
	Source      string    `gorm:"index"`
	LastSentAt  time.Time `gorm:"index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ChrobSentMark struct {
	Key         string
	OrderNumber string
	Source      string
	LastSentAt  time.Time
}

var (
	chrobDedupeDB       *gorm.DB
	chrobDedupeInitOnce sync.Once
	chrobDedupeInitErr  error
)

func getChrobDedupeDB() (*gorm.DB, error) {
	chrobDedupeInitOnce.Do(func() {
		if DB != nil {
			chrobDedupeDB = DB
		} else {
			dbPath := sqliteDBPath()
			chrobDedupeDB, chrobDedupeInitErr = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
			if chrobDedupeInitErr != nil {
				chrobDedupeInitErr = fmt.Errorf("open sqlite for chrob dedupe: %w", chrobDedupeInitErr)
				return
			}
			log.WithField("path", dbPath).Info("SQLite connection established for CHRob dedupe store")
		}

		if err := chrobDedupeDB.AutoMigrate(&ChrobSentDedupe{}); err != nil {
			chrobDedupeInitErr = fmt.Errorf("migrate chrob dedupe table: %w", err)
			return
		}
	})
	return chrobDedupeDB, chrobDedupeInitErr
}

func ChrobSentKeysSince(keys []string, since time.Time) (map[string]struct{}, error) {
	if len(keys) == 0 {
		return map[string]struct{}{}, nil
	}

	db, err := getChrobDedupeDB()
	if err != nil {
		return nil, err
	}

	unique := make(map[string]struct{}, len(keys))
	dedupedKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, ok := unique[k]; ok {
			continue
		}
		unique[k] = struct{}{}
		dedupedKeys = append(dedupedKeys, k)
	}
	if len(dedupedKeys) == 0 {
		return map[string]struct{}{}, nil
	}

	var rows []ChrobSentDedupe
	if err := db.Where("key IN ? AND last_sent_at >= ?", dedupedKeys, since).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query chrob sent dedupe: %w", err)
	}

	out := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		out[row.Key] = struct{}{}
	}
	return out, nil
}

func ChrobMarkSentBatch(marks []ChrobSentMark) error {
	if len(marks) == 0 {
		return nil
	}

	db, err := getChrobDedupeDB()
	if err != nil {
		return err
	}

	rows := make([]ChrobSentDedupe, 0, len(marks))
	for _, m := range marks {
		if m.Key == "" {
			continue
		}
		ts := m.LastSentAt
		if ts.IsZero() {
			ts = time.Now()
		}
		rows = append(rows, ChrobSentDedupe{
			Key:         m.Key,
			OrderNumber: m.OrderNumber,
			Source:      m.Source,
			LastSentAt:  ts,
		})
	}
	if len(rows) == 0 {
		return nil
	}

	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"order_number", "source", "last_sent_at", "updated_at"}),
	}).Create(&rows).Error; err != nil {
		return fmt.Errorf("upsert chrob sent dedupe: %w", err)
	}

	return nil
}
