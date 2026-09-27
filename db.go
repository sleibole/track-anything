package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Pragmas go in the DSN so they apply to every pooled connection, not just the first.
const sqlitePragmas = "?_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(ON)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=synchronous(NORMAL)"

func openDB(path string) (*gorm.DB, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := gorm.Open(sqlite.Open(path+sqlitePragmas), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// One connection keeps writes in order. A pool of deferred transactions can
	// hit SQLITE_BUSY when one connection reads and another writes. This app is
	// a single process and does not need concurrent SQLite writers.
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("sql db: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := backfillPersonalHouseholds(db); err != nil {
		return nil, fmt.Errorf("backfill households: %w", err)
	}
	return db, nil
}

// migrate creates or updates tables for every model. Add models here as phases introduce them.
func migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{}, &Session{}, &LoginToken{}, &VerificationToken{},
		&Household{}, &HouseholdMember{}, &Tracker{}, &Entry{}, &RecordedZero{},
	)
}

// backfillPersonalHouseholds gives accounts created before households existed their
// "My trackers" household. It is a one-time phase 1 → phase 2 step: delete it once every
// existing database has run it. Signup creates the household itself.
func backfillPersonalHouseholds(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		err := tx.Model(&User{}).
			Where("id NOT IN (?)", tx.Model(&HouseholdMember{}).Select("user_id")).
			Pluck("id", &ids).Error
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := createPersonalHousehold(tx, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func createPersonalHousehold(tx *gorm.DB, userID uint) error {
	h := Household{Name: personalHouseholdName}
	if err := tx.Create(&h).Error; err != nil {
		return err
	}
	return tx.Create(&HouseholdMember{HouseholdID: h.ID, UserID: userID, Role: roleOwner}).Error
}
