package main

import "time"

type User struct {
	ID              uint
	Email           string     `gorm:"uniqueIndex;not null"` // stored lowercased
	PasswordHash    string     // empty if they only use magic links
	EmailVerifiedAt *time.Time // nil until they confirm the address, or log in with a magic link
	TimeZone        string     `gorm:"not null;default:UTC"` // IANA name, e.g. "America/Los_Angeles"
	CreatedAt       time.Time
	UpdatedAt       time.Time
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

// VerificationToken is a one-time email confirmation link. It lives in its own
// table so it cannot be presented as a login link. Only the hash is stored.
type VerificationToken struct {
	ID        uint
	UserID    uint   `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

const (
	roleOwner  = "owner"
	roleMember = "member"

	personalHouseholdName = "My trackers"
)

type Household struct {
	ID          uint
	Name        string  `gorm:"not null"`
	InviteToken *string `gorm:"uniqueIndex"` // nil = invites off
	CreatedAt   time.Time
}

type HouseholdMember struct {
	HouseholdID uint   `gorm:"primaryKey"`
	UserID      uint   `gorm:"primaryKey;index"`
	Role        string `gorm:"not null"` // roleOwner or roleMember
	CreatedAt   time.Time
}

type Tracker struct {
	ID             uint
	HouseholdID    uint    `gorm:"index;not null"`
	Name           string  `gorm:"not null"`
	Icon           string  // empty means the tally mark; see trackerIcons
	Accent         string  // empty means none; see trackerAccents
	LogLabel       string  // empty means "+ Log"
	SummaryDisplay string  `gorm:"not null;default:times"` // "times", "done", or "last". Empty means times.
	Kind           string  `gorm:"not null;default:count"`
	ShareToken     *string `gorm:"uniqueIndex"` // nil = no share link
	Position       int
	ArchivedAt     *time.Time // archiving also clears ShareToken
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (t Tracker) Label() string {
	if t.LogLabel == "" {
		return defaultLogLabel
	}
	return t.LogLabel
}

// SummaryMode is the summary-display value the page should mark, with empty read as times.
func (t Tracker) SummaryMode() string {
	if t.SummaryDisplay == "" {
		return summaryTimes
	}
	return t.SummaryDisplay
}

type Entry struct {
	ID           uint
	TrackerID    uint      `gorm:"index;not null"`
	OccurredAt   time.Time `gorm:"index;not null"` // UTC; only owners choose or edit it
	RecordedByID *uint     // nil when logged through a share link
	ViaLink      bool
	Note         string
	CreatedAt    time.Time // drives the undo window
	UpdatedAt    time.Time
}

// RecordedZero is a deliberate "none" for one local calendar day. It is not an event.
// No entries and no RecordedZero for a day means nothing was logged.
type RecordedZero struct {
	ID           uint
	TrackerID    uint   `gorm:"uniqueIndex:idx_tracker_day;not null"`
	Day          string `gorm:"uniqueIndex:idx_tracker_day;not null"` // YYYY-MM-DD in the logger's zone
	RecordedByID *uint
	ViaLink      bool
	CreatedAt    time.Time // drives the undo window
}
