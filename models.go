package main

import "time"

type User struct {
	ID                     uint
	Email                  string     `gorm:"not null;uniqueIndex:idx_users_email,where:email <> ''"` // stored lowercased; empty means a guest
	PasswordHash           string     // empty if they only use magic links
	EmailVerifiedAt        *time.Time // nil until they confirm the address, or log in with a magic link
	EmailPromptDismissedAt *time.Time // set when a guest dismisses the add-email prompt
	TimeZone               string     `gorm:"not null;default:UTC"` // IANA name, e.g. "America/Los_Angeles"
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (u User) HasPassword() bool {
	return u.PasswordHash != ""
}

// IsGuest is an account with no email. It is not a separate kind of login.
func (u User) IsGuest() bool { return u.Email == "" }

// HasVerifiedEmail is the check for invite, join, and a future paid checkout.
func (u User) HasVerifiedEmail() bool { return u.EmailVerifiedAt != nil }

// Label is the address, or "Guest" when there is no address.
func (u User) Label() string {
	if u.Email == "" {
		return "Guest"
	}
	return u.Email
}

type Session struct {
	ID        string `gorm:"primaryKey"` // sha256 hex of the cookie value; the raw token is only in the cookie
	UserID    uint   `gorm:"index;not null"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

// LoginToken is a one-time magic login link. Only the hash is stored,
// so a database leak can't be used to log in.
type LoginToken struct {
	ID               uint
	UserID           uint   `gorm:"index;not null"`
	TokenHash        string `gorm:"uniqueIndex;not null"` // sha256 hex of the token in the emailed link
	TransferIntentID *uint  `gorm:"index"`                // set only for a guest transfer; not permission to move data
	ExpiresAt        time.Time
	UsedAt           *time.Time
	CreatedAt        time.Time
}

// VerificationToken is a one-time email confirmation link. It lives in its own
// table so it cannot be presented as a login link. Only the hash is stored.
type VerificationToken struct {
	ID        uint
	UserID    uint   `gorm:"index;not null"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	Email     string // proposed address for a guest claim; empty confirms the address already on the user
	ClaimID   *uint  `gorm:"index"`
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
	Position    int    // this member's dashboard order; lower comes first. Not shared.
	CreatedAt   time.Time
}

type Tracker struct {
	ID             uint
	HouseholdID    uint       `gorm:"index;not null"`
	Name           string     `gorm:"not null"`
	Icon           string     // empty means the tally mark; see trackerIcons
	Accent         string     // empty means none; see trackerAccents
	LogLabel       string     // empty means "+ Log"
	SummaryDisplay string     `gorm:"not null;default:times"` // "times", "done", or "last". Empty means times.
	Kind           string     `gorm:"not null;default:count"` // count, number, duration, or cumulative
	Unit           string     // number and cumulative trackers, such as "lb" or "points". Not a per-entry choice.
	Goal           *float64   // cumulative trackers only. Nil means no repeating goal.
	ShareToken     *string    `gorm:"uniqueIndex"` // nil = no share link
	Position       int        // order within the household; lower comes first. Shared.
	ArchivedAt     *time.Time // archiving also clears ShareToken
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TrackerOrder is one person's place for a tracker on their home list, across
// households. Not shared. A tracker without a row comes after the placed ones.
type TrackerOrder struct {
	UserID    uint `gorm:"primaryKey"`
	TrackerID uint `gorm:"primaryKey;index"`
	Position  int  `gorm:"not null"`
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
	Number       *float64  // number trackers, and the amount on a cumulative tracker
	DurationSec  *int      // duration trackers, whole seconds
	CreatedAt    time.Time // drives the undo window
	UpdatedAt    time.Time
}

const (
	kindCount      = "count"
	kindNumber     = "number"
	kindDuration   = "duration"
	kindCumulative = "cumulative"
)

func (t Tracker) IsCount() bool      { return t.Kind == "" || t.Kind == kindCount }
func (t Tracker) IsNumber() bool     { return t.Kind == kindNumber }
func (t Tracker) IsDuration() bool   { return t.Kind == kindDuration }
func (t Tracker) IsCumulative() bool { return t.Kind == kindCumulative }

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

// Redemption is one reward taken from a cumulative tracker's completed goals.
// Goals earned stay derived from entries. This row is not a point balance.
// UserID is who redeemed it. It is not a foreign key: deleting a user later
// must not delete the redemption. The email is looked up while the user exists.
type Redemption struct {
	ID         uint
	TrackerID  uint  `gorm:"index;not null"`
	UserID     *uint `gorm:"index"`
	Note       string
	RequestKey string    `gorm:"uniqueIndex;not null"`
	RedeemedAt time.Time `gorm:"not null"`
	CreatedAt  time.Time
}

// RewardRequest remembers a redeem submission after its redemption is undone.
// A retry of that same request does not create another redemption.
type RewardRequest struct {
	RequestKey string `gorm:"primaryKey"`
	TrackerID  uint   `gorm:"not null;index"`
	CreatedAt  time.Time
}

// StartAttempt dedupes the first POST /start for a few minutes.
// It is not a session and cannot sign in a user who is no longer a guest.
type StartAttempt struct {
	TokenHash  string `gorm:"primaryKey"`
	UserID     *uint  `gorm:"index"`
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}

// GuestClaim is one guest's request to add an email.
// A new address and a transfer into an existing account are its two outcomes.
type GuestClaim struct {
	ID          uint
	GuestUserID uint `gorm:"index;not null"`
	Email       string
	Next        string
	ExpiresAt   time.Time
	CancelledAt *time.Time
	ConsumedAt  *time.Time
	CreatedAt   time.Time
}

// TransferIntent authorizes a move only together with the source-proof cookie,
// a verified destination mailbox, and an explicit confirm post.
type TransferIntent struct {
	ID              uint
	ClaimID         uint   `gorm:"uniqueIndex;not null"`
	SourceProofHash string `gorm:"uniqueIndex;not null"`
	SourceUserID    uint   `gorm:"index;not null"`
	SourceSessionID string `gorm:"not null"`
	DestUserID      uint   `gorm:"index;not null"`
	DestVerifiedAt  *time.Time
	ExpiresAt       time.Time
	CancelledAt     *time.Time
	ConsumedAt      *time.Time
	CreatedAt       time.Time
}

// TransferReceipt survives the guest so a repeated confirm can succeed
// after the source user row is gone.
type TransferReceipt struct {
	SourceProofHash string `gorm:"primaryKey"`
	DestUserID      uint   `gorm:"not null;index"`
	CompletedAt     time.Time
}

// TrackerCreate remembers one create submission so a retry does not insert again.
type TrackerCreate struct {
	RequestKey string `gorm:"primaryKey"`
	UserID     uint   `gorm:"not null;index"`
	TrackerID  uint   `gorm:"not null"`
	CreatedAt  time.Time
}
