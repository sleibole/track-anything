package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if err := backfillOrder(db); err != nil {
		return nil, fmt.Errorf("backfill order: %w", err)
	}
	return db, nil
}

// migrate creates or updates tables for every model. Add models here as phases introduce them.
func migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&User{}, &Session{}, &LoginToken{}, &VerificationToken{},
		&Household{}, &HouseholdMember{}, &Tracker{}, &Entry{}, &RecordedZero{}, &TrackerOrder{},
		&Redemption{}, &RewardRequest{},
		&StartAttempt{}, &GuestClaim{}, &TransferIntent{}, &TransferReceipt{}, &TrackerCreate{},
	); err != nil {
		return err
	}
	return ensurePartialEmailIndex(db)
}

type userEmailIndex struct {
	Name string
	SQL  string
}

func userEmailIndexes(tx *gorm.DB) ([]userEmailIndex, error) {
	var rows []userEmailIndex
	err := tx.Raw(`SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'users' AND sql IS NOT NULL`).Scan(&rows).Error
	return rows, err
}

func indexCoversEmail(sql string) bool {
	return strings.Contains(strings.ToLower(sql), "email")
}

func indexIsPartialEmail(sql string) bool {
	low := strings.ToLower(sql)
	return strings.Contains(low, "unique") && indexCoversEmail(low) && strings.Contains(low, "where") && strings.Contains(low, "email <>")
}

func quoteSQLiteIdent(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty index name")
	}
	for _, r := range name {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return "", fmt.Errorf("unexpected index name %q", name)
		}
	}
	return `"` + name + `"`, nil
}

// replaceEmailIndex creates the partial unique index before dropping a full one.
// The caller must run it inside a transaction so a failure restores the old index.
func replaceEmailIndex(tx *gorm.DB) error {
	rows, err := userEmailIndexes(tx)
	if err != nil {
		return err
	}
	var full []string
	partialOK := false
	for _, row := range rows {
		if !indexCoversEmail(row.SQL) || !strings.Contains(strings.ToLower(row.SQL), "unique") {
			continue
		}
		if row.Name == "idx_users_email" && indexIsPartialEmail(row.SQL) {
			partialOK = true
			continue
		}
		full = append(full, row.Name)
	}
	if partialOK && len(full) == 0 {
		return nil
	}
	if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_partial ON users(email) WHERE email <> ''`).Error; err != nil {
		return err
	}
	for _, name := range full {
		quoted, err := quoteSQLiteIdent(name)
		if err != nil {
			return err
		}
		if err := tx.Exec("DROP INDEX " + quoted).Error; err != nil {
			return err
		}
	}
	if !partialOK {
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email <> ''`).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`DROP INDEX IF EXISTS idx_users_email_partial`).Error
}

func ensurePartialEmailIndex(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		return replaceEmailIndex(tx)
	})
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
	pos, err := nextMemberPosition(tx, userID)
	if err != nil {
		return err
	}
	return tx.Create(&HouseholdMember{
		HouseholdID: h.ID, UserID: userID, Role: roleOwner, Position: pos,
	}).Error
}

// nextMemberPosition is one past the last household on this person's dashboard.
func nextMemberPosition(tx *gorm.DB, userID uint) (int, error) {
	var last int
	err := tx.Model(&HouseholdMember{}).Where("user_id = ?", userID).
		Select("COALESCE(MAX(position), 0)").Scan(&last).Error
	return last + 1, err
}

// backfillOrder gives existing rows the order the dashboard already showed.
// It is idempotent: a saved order is left alone, and running it again changes nothing.
// Memberships still at position 0, for one user, were never reordered. They keep
// created_at, then household id. Tracker positions that are 0 or duplicated keep
// position, then id, which is how the dashboard and household page already sort.
func backfillOrder(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := backfillMemberPositions(tx); err != nil {
			return err
		}
		return backfillTrackerPositions(tx)
	})
}

func backfillMemberPositions(tx *gorm.DB) error {
	var members []HouseholdMember
	if err := tx.Order("user_id, created_at, household_id").Find(&members).Error; err != nil {
		return err
	}
	var batch []HouseholdMember
	flush := func() error {
		defer func() { batch = nil }()
		if len(batch) == 0 {
			return nil
		}
		for _, m := range batch {
			if m.Position != 0 {
				return nil
			}
		}
		for i, m := range batch {
			err := tx.Model(&HouseholdMember{}).
				Where("user_id = ? AND household_id = ?", m.UserID, m.HouseholdID).
				Update("position", i+1).Error
			if err != nil {
				return err
			}
		}
		return nil
	}
	var userID uint
	for _, m := range members {
		if len(batch) > 0 && m.UserID != userID {
			if err := flush(); err != nil {
				return err
			}
		}
		userID = m.UserID
		batch = append(batch, m)
	}
	return flush()
}

func backfillTrackerPositions(tx *gorm.DB) error {
	var trackers []Tracker
	if err := tx.Order("household_id, position, id").Find(&trackers).Error; err != nil {
		return err
	}
	var batch []Tracker
	flush := func() error {
		defer func() { batch = nil }()
		if !trackerPositionsNeedBackfill(batch) {
			return nil
		}
		for i, t := range batch {
			if err := tx.Model(&Tracker{}).Where("id = ?", t.ID).Update("position", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	}
	var householdID uint
	for _, t := range trackers {
		if len(batch) > 0 && t.HouseholdID != householdID {
			if err := flush(); err != nil {
				return err
			}
		}
		householdID = t.HouseholdID
		batch = append(batch, t)
	}
	return flush()
}

func trackerPositionsNeedBackfill(trackers []Tracker) bool {
	seen := make(map[int]bool, len(trackers))
	for _, t := range trackers {
		if t.Position == 0 || seen[t.Position] {
			return true
		}
		seen[t.Position] = true
	}
	return false
}
