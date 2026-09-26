package main

import "time"

type User struct {
	ID           uint
	Email        string `gorm:"uniqueIndex;not null"` // stored lowercased
	PasswordHash string // empty if they only use magic links
	TimeZone     string `gorm:"not null;default:UTC"` // IANA name, e.g. "America/Los_Angeles"
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (u User) HasPassword() bool {
	return u.PasswordHash != ""
}

type Session struct {
	ID        string `gorm:"primaryKey"` // random 32-byte token, base64url; also the cookie value
	UserID    uint   `gorm:"index;not null"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

// LoginToken is a one-time magic login link. Only the hash is stored,
// so a database leak can't be used to log in.
type LoginToken struct {
	ID        uint
	UserID    uint   `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"` // sha256 hex of the token in the emailed link
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}
