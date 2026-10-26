package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func emailIndexSQL(t *testing.T, db *gorm.DB, name string) string {
	t.Helper()
	rows, err := userEmailIndexes(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Name == name {
			return row.SQL
		}
	}
	t.Fatalf("index %s missing in %+v", name, rows)
	return ""
}

func TestFreshDatabaseUsesPartialEmailIndex(t *testing.T) {
	a := newTestApp(t)
	sql := emailIndexSQL(t, a.db, "idx_users_email")
	if !indexIsPartialEmail(sql) {
		t.Fatalf("index sql %q", sql)
	}
	if err := a.db.Exec(`INSERT INTO users (email, time_zone) VALUES ('', 'UTC'), ('', 'UTC')`).Error; err != nil {
		t.Fatal(err)
	}
	err := a.db.Exec(`INSERT INTO users (email, time_zone) VALUES ('same@example.com', 'UTC'), ('same@example.com', 'UTC')`).Error
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate address err %v", err)
	}
}

func TestEmailIndexUpgradeReplacesFullUniqueIndex(t *testing.T) {
	a := newTestApp(t)
	if err := a.db.Exec(`DROP INDEX idx_users_email`).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Exec(`CREATE UNIQUE INDEX idx_users_email ON users(email)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := ensurePartialEmailIndex(a.db); err != nil {
		t.Fatal(err)
	}
	sql := emailIndexSQL(t, a.db, "idx_users_email")
	if !indexIsPartialEmail(sql) {
		t.Fatalf("upgraded sql %q", sql)
	}
	rows, err := userEmailIndexes(a.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if indexCoversEmail(row.SQL) && strings.Contains(strings.ToLower(row.SQL), "unique") && !indexIsPartialEmail(row.SQL) {
			t.Fatalf("full unique index remained: %s %s", row.Name, row.SQL)
		}
	}
}

func TestEmailIndexReplacementRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE users (id integer primary key, email text not null)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_users_email ON users(email)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users (email) VALUES ('a@example.com')`).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := replaceEmailIndex(tx); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatal(err)
	}
	sql := emailIndexSQL(t, db, "idx_users_email")
	if indexIsPartialEmail(sql) || !strings.Contains(strings.ToLower(sql), "unique") {
		t.Fatalf("rolled back sql %q", sql)
	}
	err = db.Exec(`INSERT INTO users (email) VALUES ('a@example.com')`).Error
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("uniqueness lost: %v", err)
	}
}
