# Track Anything — Architecture

How Track Anything is built today. What the product is, and what is still planned, is `PLAN.md`. How it should look and behave is `DESIGN.md`. User help, when it exists, belongs in `docs/help/`.

Headings marked **Not built** are decisions for later. They are not in the code.

If `PLAN.md` or this document describes a current behavior, `make test` covers it. Later enhancements (offline logging, event-relative summaries) are specified here and join the suite when they are built. Recurring, scheduled, and goal-based trackers are a later product direction in `PLAN.md`. They are not specified here: no schema, no notification channel, and no routes until that direction is ready to build.

## Constraints

- **Boring technology.** Standard Go, server-rendered HTML, SQLite, straightforward code.
- **One process, one database.** No microservices, distributed databases, server-side queues, or a separate worker. Mail is sent in the request. The rate limiter is in memory. Expired sessions and login and verification tokens are deleted by a goroutine in this process, and that goroutine stops on shutdown. Do not add a job runner without a demonstrated need. The later offline queue lives in the browser. See Offline logging.
- **No schema language or plugin system** before a concrete tracker type requires one.
- **Do not optimize for hypothetical scale** before the product has users. Exactly one instance, because of SQLite.
- **Direct Go dependencies:** GORM, `github.com/glebarez/sqlite`, and `golang.org/x/crypto`. Everything else is the standard library, vendored static files, or HTTP calls. Prometheus is not a dependency. See Metrics.
- **No public API, and no offline mode through phase 5.** Basic offline logging is a later enhancement: a small cache and a browser queue around the existing log action. The app stays server-rendered. See Offline logging.
- **JavaScript is an enhancement.** Forms use `POST` and work without it. HTMX upgrades the same forms. Chart.js is the planned chart library for phase 3. It is not vendored, and phase 2 does not load it. There is no React, Vue, SPA, or second charting stack. Signup, login, and change password include a Cloudflare Turnstile widget. The form is still a normal POST. The server rejects the action when the token does not verify. The later offline log path is the same kind of enhancement: without the script, the button is an ordinary POST and nothing is queued.

## Stack

| Concern | Choice | Notes |
| --- | --- | --- |
| Language | Go (latest stable, 1.22+) | Needed for method + wildcard routing in `net/http` |
| HTTP routing | `net/http` `ServeMux` | Patterns like `GET /trackers/{id}` |
| Templates | `html/template` | Auto-escaping, layouts via `{{block}}` / `{{template}}` |
| Static files and templates | `embed` | Ship a single binary |
| Logging | `log/slog` | Structured logs to stdout |
| Metrics | Not built | A Prometheus scrape is a later idea. The process does not expose `/metrics`. See Metrics |
| ORM | GORM (`gorm.io/gorm`) | `AutoMigrate` for schema |
| Database | SQLite, one shared app database | A database per user was considered and rejected: it complicates migrations, backups, and especially sharing |
| SQLite driver | `github.com/glebarez/sqlite` | Pure-Go GORM driver, so no CGO and easy cross-compiling |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Email (magic links) | `net/smtp` to a transactional email relay | In development, links are logged to the console instead of sent |
| Frontend | HTMX (vendored `htmx.min.js`) | Served from `/static`, no CDN |
| Charts | Not built | Chart.js is the planned library for phase 3. It is not vendored. Logging does not depend on it. |
| CSS | Pico.css (vendored `pico.min.css`) | Classless-first, minimal custom CSS. Visual rules are in `DESIGN.md` |
| Icons | Tabler, individual SVGs vendored and embedded | MIT. Inlined so `stroke="currentColor"` follows Pico, including dark mode. A tracker's own icon is an allow-listed Tabler name or emoji. |
| Payments | Not built | Paddle is the planned merchant of record for phase 5. There is no billing code |
| Ads | Not built | AdSense is phase 5, after the app is live |
| Bot protection | Cloudflare Turnstile | Managed mode on signup, login (password and magic link), and change password. The server calls Siteverify. This does not replace CSRF or rate limits |

## Project layout

Start flat: one `main` package, split only when navigation or coupling becomes painful. If it grows, move toward `cmd/web` (startup), `internal/app` (server, routes, shared helpers), `internal/tracker`, `internal/store`, and `ui/templates` + `ui/static`, organized by feature rather than handler/service/repository layers.

```
track-anything/
├── docs/
│   ├── development/    # PLAN.md, DESIGN.md, ARCHITECTURE.md, SUMMARY-DISPLAY.md
│   └── help/           # future user-facing Markdown; none yet
├── go.mod
├── main.go             # config, load .env, open DB, migrate, build mux, start server
├── db.go               # GORM setup, AutoMigrate, SQLite pragmas, one-time data backfills
├── models.go           # structs below
├── days.go             # day boundaries, per-day counts, undo window, summaries
├── access.go           # trackerForUser, trackerForShareToken, archivedTrackerForOwner, household lookups
├── auth.go             # signup, login, logout, magic links, email verification, sessions
├── turnstile.go        # Siteverify check for signup, login, and change password
├── mail.go             # net/smtp sender, console sender for development
├── env.go              # load .env at startup; existing environment variables win
├── .env.example        # local MailHog settings; copy to .env (gitignored)
├── settings.go         # time zone, password, resend verification
├── ratelimit.go        # in-memory fixed-window limiter and trusted client IP
├── handlers.go         # health check, dashboard, shared handler helpers
├── handlers_trackers.go # create, edit, archive, restore, share link, tracker page
├── handlers_entries.go  # log, undo, owner corrections, recorded zeros
├── handlers_households.go # create, rename, leave, members, invite link, join, archived trackers
├── handlers_share.go   # share-link pages, no login
├── render.go           # template loading + render helper (full page vs HTMX partial)
├── middleware.go       # request logging, panic recovery, security headers
├── templates/
│   ├── layout.html     # ad and consent scripts live here, outside any HTMX swap target
│   ├── dashboard.html
│   ├── tracker_show.html
│   ├── tracker_form.html # new and edit, plus the share link and archive
│   ├── share.html      # what a share-link visitor sees
│   ├── household.html
│   ├── join.html       # invitation confirm page
│   ├── verify.html     # email confirmation; GET does not consume the token
│   ├── message.html    # 404, 403, and other one-line answers
│   └── partials/       # fragments shared by pages: card, log control, entry
├── icons/              # Tabler SVGs we use, inlined by the icon template func
├── static/
│   ├── htmx.min.js
│   ├── pico.min.css
│   ├── app.css
│   ├── manifest.webmanifest
│   ├── icon.svg        # app icon source and favicon: Tabler tally marks on Pico blue
│   ├── icon-192.png
│   ├── icon-512.png    # also the maskable icon; the artwork has safe-zone padding
│   └── apple-touch-icon.png
├── .air.toml           # Air config for local autoreload
├── Dockerfile          # multi-stage build, used by Dokku now and Fly.io later
└── Makefile            # dev, run, build, test
```

## Local development

From the repo root:

```
make dev
```

That starts [Air](https://github.com/air-verse/air), which builds the server and listens on http://localhost:8080. `ADDR` changes the port. Go runs Air by version, so it does not need a separate install and it is not a module dependency. Copy `.env.example` to `.env` if you want the local settings in Config; a missing file is fine.

Saving a `.go` file, an HTML template, or CSS or JavaScript under `static/` makes Air rebuild and restart. Refresh the browser after the restart. Air does not reload the page. Templates, CSS, and JavaScript stay embedded with `go:embed`, so those edits are picked up by compiling a new binary into `tmp/`, which is gitignored.

`make run` starts the server once, without reload. `make build` and the Docker image do not use Air.

## Config

The process environment is the source of truth. On startup, `main` loads a gitignored `.env` if the file is present (copy `.env.example`). Variables already set are left alone, so a shell export or Dokku config wins over the file. A missing `.env` is fine. Tests do not load `.env`; they build a `config` directly. The Docker build ignores `.env`, so a local file is not copied into the image.

Local MailHog, when used, is `SMTP_HOST=127.0.0.1` and `SMTP_PORT=1025` (web inbox on port 8025). With `SMTP_HOST` unset, the mailer logs the link to the console.

Production sets the same names with `dokku config:set`: `ADDR`/`PORT`, `DB_PATH`, `BASE_URL`, `ENV`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`, `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET_KEY`, and, behind the proxy described under Deployment, `TRUSTED_IP_HEADER`. The app does not read Paddle settings. Billing is not built.

`ENV` is `dev` or `prod`. Any other value refuses to start. `ENV=dev` turns off the `Secure` cookie flag so browsers work on `http://localhost`.

In production, `BASE_URL` must be set and must be an `https` URL with a host. The process exits otherwise, so a missing public URL is not discovered when the first login email goes out.

Production also requires `TURNSTILE_SITE_KEY` and `TURNSTILE_SECRET_KEY`. The process exits if either is missing, and the error does not include the secret. The secret is not written to logs. Development leaves both unset and then uses Cloudflare's always-pass test keys, so local signup does not need production credentials. Setting only one of them is a configuration error. The trackanything.io widget is Managed mode. Verification is an HTTP POST to Siteverify from the request, with a 5 second timeout, before the protected action. The client address sent as `remoteip` is the same address rate limiting uses (`Fly-Client-IP` in production). A missing token, a failed check, a malformed body, a non-200 response, a timeout, or a network error all reject the action with the same page message and do not perform it. Tests stub that HTTP call and do not contact Cloudflare.

`TRUSTED_IP_HEADER` is empty unless this process sits behind a proxy that strips client-supplied forwarding headers and sets that one header itself. The only accepted values are empty, `Fly-Client-IP`, `CF-Connecting-IP`, and `X-Forwarded-For`. Empty means the client address is `RemoteAddr`. `Fly-Client-IP` and `CF-Connecting-IP` are a single address. `X-Forwarded-For` is a list, and only the first address is used. The app does not fall back from one header to another. Production sets `TRUSTED_IP_HEADER=Fly-Client-IP` because Fly Proxy is the trusted public ingress. See Deployment.

In production, a configured SMTP server must negotiate TLS. The mailer uses implicit TLS on port 465 and STARTTLS otherwise, and it refuses to send if neither happens. Development can use MailHog without TLS.

## Data model

Tokens are 32 random bytes from `crypto/rand`, base64url-encoded. A failure from that source returns an error and does not produce a token. Login tokens, verification tokens, and session IDs are stored only as a sha256 hash, so a database copy cannot be replayed as a cookie or a link. The raw session token exists only in the cookie. Share and invite tokens stay readable because the app shows those URLs again.

```go
type User struct {
    ID              uint
    Email           string     `gorm:"uniqueIndex;not null"` // stored lowercased
    PasswordHash    string     // empty until the address is verified and a password is set in settings
    EmailVerifiedAt *time.Time // nil until they confirm the address, or log in with a magic link
    TimeZone        string     `gorm:"not null;default:UTC"` // IANA name, e.g. "America/Los_Angeles"
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type Session struct {
    ID        string `gorm:"primaryKey"` // sha256 hex of the cookie value
    UserID    uint   `gorm:"index;not null"`
    ExpiresAt time.Time
    CreatedAt time.Time
}

// A one-time login link. Only the hash is stored, so a database leak can't be used to log in.
type LoginToken struct {
    ID        uint
    UserID    uint   `gorm:"index;not null"`
    TokenHash string `gorm:"uniqueIndex;not null"` // sha256 of the token in the emailed link
    ExpiresAt time.Time // 15 minutes after creation
    UsedAt    *time.Time
}

// A one-time email confirmation link. Its own table, so it cannot be presented as a login link.
// Only the hash is stored.
type VerificationToken struct {
    ID        uint
    UserID    uint   `gorm:"index;not null"`
    TokenHash string `gorm:"uniqueIndex;not null"`
    ExpiresAt time.Time // 24 hours after creation
    UsedAt    *time.Time
}

type Household struct {
    ID          uint
    Name        string  `gorm:"not null"` // "My trackers" for a personal household. Not globally unique. One owner cannot have two that match after trim and case folding. Owners can rename it
    InviteToken *string `gorm:"uniqueIndex"` // nil = invites off
    CreatedAt   time.Time
}

type HouseholdMember struct {
    HouseholdID uint   `gorm:"primaryKey"`
    UserID      uint   `gorm:"primaryKey;index"`
    Role        string `gorm:"not null"` // "owner" or "member"
    CreatedAt   time.Time
}

type Tracker struct {
    ID             uint
    HouseholdID    uint       `gorm:"index;not null"`
    Name           string     `gorm:"not null"`
    Icon           string     // empty means the tally-mark default. Allow-list only: a curated Tabler name or an emoji.
    Accent         string     // empty means no accent. Allow-list only.
    LogLabel       string     // empty means "+ Log". A few words, 24 characters at most.
    SummaryDisplay string     `gorm:"not null;default:times"` // "times", "done", or "last". Empty means times. Presentation only.
    Kind           string     `gorm:"not null;default:count"` // "count", later "number" and "duration"
    Unit           string     // number trackers only: "kg", "lb"
    ShareToken     *string    `gorm:"uniqueIndex"` // nil = no share link
    Position       int
    ArchivedAt     *time.Time // archived trackers are hidden; archiving also clears ShareToken
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type Entry struct {
    ID           uint
    TrackerID    uint      `gorm:"index;not null"`
    OccurredAt   time.Time `gorm:"index;not null"` // when it happened (UTC). Owners choose or edit it. Later offline sync stores the tap time.
    RecordedByID *uint     // nil when logged through a share link
    ViaLink      bool
    Note         string
    Number       *float64 // number trackers (phase 4)
    DurationSec  *int     // duration trackers (phase 4)
    CreatedAt    time.Time // drives the 15-minute undo window
    UpdatedAt    time.Time
}

// A deliberate "none" for one local calendar day of a count tracker.
// Not an event, so it does not increase that day's count.
// No entries and no row here means nothing was logged.
type RecordedZero struct {
    ID           uint
    TrackerID    uint   `gorm:"uniqueIndex:idx_tracker_day;not null"`
    Day          string `gorm:"uniqueIndex:idx_tracker_day;not null"` // YYYY-MM-DD, the page's local calendar day
    RecordedByID *uint  // nil when marked through a share link
    ViaLink      bool
    CreatedAt    time.Time // drives the 15-minute undo window
}
```

### Decisions worth calling out

- **`OccurredAt` vs `CreatedAt`**: owners can backfill, so when it happened is separate from when the row was written. The undo window uses `CreatedAt`. Later offline sync uses the same split: `OccurredAt` is the tap, and `CreatedAt` is when the server writes the row.
- **Only owners set `OccurredAt` on an ordinary post.** `POST /trackers/{id}/entries` reads the time field only when the role is owner, interpreting it in the owner's stored zone. For a member it ignores any submitted time and uses the server's current time, so a hand-built request can't backfill. The note is accepted from both. Share-link posts never read a time. The later offline sync is the exception, and only for a new client id. See Offline logging.
- **Personal household.** Signup creates the user, a `Household` named "My trackers", and an owner `HouseholdMember` row in one transaction. Nothing marks it as personal. It is an ordinary household that has one member until someone is invited. Shared trackers go in a separately created household (`PLAN.md`, Sharing). There is no personal-tracker flag and no per-tracker access list.
- **Phase 1 → phase 2 backfill.** Accounts created in phase 1 have no household. After `AutoMigrate`, `db.go` runs a one-time step in a transaction: every user with no `household_members` row gets a "My trackers" household with that user as owner. It is idempotent, so running it again changes nothing. Request handlers do not check for a missing household. Once every existing database has run the step (local development databases; production first launches with phase 2), the step is deleted.
- **Ownership stays small.** Removing deletes a `member` row. If invites are on, the same transaction replaces the invite token, so the old link cannot be used to rejoin. The handler refuses to remove any owner, including the person asking. There is no demotion. Leaving is for members only (below). Phase 2 had no leave route. Owner departure, ownership transfer, sole-owner cases, and household deletion are not designed. They belong with phase 5 account deletion.
- **Household organization.** `Household`, `HouseholdMember`, and `Tracker.HouseholdID` are enough. There are no new tables or columns.
  - **Create** inserts a `Household` and an owner `HouseholdMember` row for the creator in one transaction, the same pair signup makes. The name is trimmed, required, and limited to the tracker name length (`maxTrackerName`). Invites start off. Before the insert, the trimmed name is compared, case-insensitively, with the names of households that user already owns. A match is rejected and nothing is created. A household they only belong to does not count, so two people can each own "Family".
  - **Rename** updates `Name` with the same validation and the same owned-name check, excluding the household being renamed. Saving its own name, including a spacing or case change, is allowed. There is no unique index on `Name`. The check is in the handler, because it is per owner rather than global.
  - **Leave** deletes the caller's own row only when that row's role is `member`, as one conditional delete. If the caller is an owner, nothing changes. Leaving does not rotate the invite token, because the person left on their own and is not being kept out. It touches no other household, so a personal household and other memberships are untouched.
  - **Move** changes only `Tracker.HouseholdID`.
    - The source tracker comes from `trackerForUser` and must have the role `owner`. An archived tracker is not movable.
    - The destination comes from `householdForUser` and must also have the role `owner` for the same user. A destination where the user is only a member is refused and nothing changes. So is a destination the user cannot see, or the tracker's current household.
    - In one transaction, the move sets `HouseholdID` and puts the tracker last in the destination's `Position` order.
    - `Entry` and `RecordedZero` rows belong to the tracker, not the household, so they are not touched or copied. `ShareToken`, `ArchivedAt`, and the presentation fields stay as they were. The move neither clears nor replaces `ShareToken`, so an active share link still resolves through `trackerForShareToken`.
    - After the move, access follows `household_members` for the destination. `trackerForUser` already joins through `trackers.household_id`, so nothing else has to change. Members of the old household who are not in the new one get a 404.
    - A share page uses the destination household's first owner's zone from then on.
  - **Dashboard disambiguation.** The dashboard still loads `householdCreator` for each group. Names match with the same rule as the owned-name check: trim surrounding whitespace, then compare case-insensitively. It shows the creator's email only on a household created by someone else whose name matches another household on the same dashboard. A household the viewer created never shows the email. One person cannot own two that match, so a collision on the dashboard is always between households created by different people.
- **Value columns on `Entry`** (`Number`, `DurationSec`) instead of a generic field system. Two nullable columns cover the planned tracker kinds with no joins. Fields and sub-trackers, if they are ever built, add a `Field` table, a `Value` table, and `ParentID` on `Tracker` and `Entry`.
- **Email is stored lowercased** so `Sheldon@…` and `sheldon@…` can't become two accounts.
- **Archive, not delete**, for trackers. Archiving sets `ArchivedAt` and clears `ShareToken`. Restoring clears `ArchivedAt` and leaves sharing off, so an old link can't come back. Entries are untouched either way. Permanent delete is a separate confirmed action in phase 5.
- **Icon, accent, and log label** are optional strings. Empty means the default: the tally-mark icon, no accent, and the button text "+ Log". Writes accept only the built-in allow-lists (icon and accent) and a log label of at most 24 characters. A tracker icon that later leaves the picker falls back to the tally mark when rendered. A missing chrome icon is still a template error.
- **Summary display** is `times`, `done`, or `last` on `Tracker`. Empty means `times`. The column default is `times`, so existing rows keep the phase 2 wording with no entry backfill. It does not change `Kind` or `Entry`. Done today does not enforce one entry per day. Last occurrence is the entry with the latest `OccurredAt`, rendered in the page's zone, and it does not clear at midnight. No entries at all read "Never logged". A recorded zero is not an entry, so those two modes ignore it. The wording is in `PLAN.md`. Create and edit send the field. A missing or empty value stores `times`, which is what a phase 2 form that omitted the field stored.
- **`RecordedZero.Day`** is the local calendar date that page already calls "today" or a backfilled day: the viewer's stored zone, or the household first owner's zone on a share page. It is not a UTC timestamp, because the mark is about a day rather than an instant. Logging an entry whose local day matches `Day` deletes that row in the same transaction as the insert. Recording none counts that day's entries and inserts the mark in one transaction, so the two cannot land together. A unique conflict on the day is treated as "already none" unless entries exist, in which case the mark is removed. Plan and Paddle columns are not on `User`. They arrive with billing, which is not built.

### Access checks

Every tracker lookup goes through one of three small functions, so access rules live in one place:

```go
// trackerForUser returns the tracker and the user's role in its household,
// or gorm.ErrRecordNotFound if the user isn't a member.
func trackerForUser(db *gorm.DB, userID, trackerID uint) (Tracker, string, error) {
    var row struct {
        Tracker
        Role string
    }
    err := db.Table("trackers").
        Select("trackers.*, household_members.role").
        Joins("JOIN household_members ON household_members.household_id = trackers.household_id").
        Where("trackers.id = ? AND household_members.user_id = ? AND trackers.archived_at IS NULL", trackerID, userID).
        Take(&row).Error
    return row.Tracker, row.Role, err
}

// trackerForShareToken returns the tracker behind a share link, if it's still active.
func trackerForShareToken(db *gorm.DB, token string) (Tracker, error)

// archivedTrackerForOwner returns an archived tracker only if the user owns its household.
// Only restore uses it; every other route sees archived trackers as missing.
func archivedTrackerForOwner(db *gorm.DB, userID, trackerID uint) (Tracker, error)
```

Handlers then check the role for owner-only actions. A tracker the user can't see is a 404, never a 403, so IDs don't leak. The test checks no row changed.

## Time zones

Track Anything is organized around calendar days, so "today" and the day an entry belongs to need a stable zone.

- Store timestamps in UTC.
- Store the account's IANA time zone on `User.TimeZone` (for example `America/Los_Angeles`).
- Detect it once, at signup, from `Intl.DateTimeFormat().resolvedOptions().timeZone` in a hidden field. Signup does not ask. Later requests do not replace it from the browser. Traveling must not move entries onto different days.
- Use that stored zone for every calendar boundary: today, per-day counts, charts, history, and overlays.
- Share-link visitors have no zone, so "today" on a share page uses the household's first owner's zone. Everyone in a house sees the same "today".
- Group by local date in Go. SQLite cannot convert to IANA zones itself, and grouping in Go stays correct across daylight-saving changes. The same per-day series feeds the charts.
- Settings does not offer the raw IANA name as a text field. If the detected zone is wrong, a low-profile control lists friendly names, such as "Pacific Time (US & Canada)", and `POST /settings/timezone` stores the matching IANA name. Most people never need to touch it. The control's presentation is in `DESIGN.md`.

Count "today" by computing day boundaries in that zone, then querying UTC:

```go
loc, _ := time.LoadLocation(user.TimeZone)
now := time.Now().In(loc)
start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
end := start.AddDate(0, 0, 1)

var count int64
db.Model(&Entry{}).
    Where("tracker_id = ? AND occurred_at >= ? AND occurred_at < ?", tracker.ID, start.UTC(), end.UTC()).
    Count(&count)
```

Counts per day (for example the last 30 days) load `occurred_at` for the range and group by local date in Go.

The binary embeds zone data with `import _ "time/tzdata"` so the IANA database exists in the container.

## Carrying forward last values (phase 4)

Prefill from the tracker's most recent entry by `OccurredAt`:

```go
var last Entry
err := db.Where("tracker_id = ?", tracker.ID).Order("occurred_at DESC").First(&last).Error
// gorm.ErrRecordNotFound means there's nothing to carry forward; show an empty form.
```

A backfilled older entry must not win. Only the value carries forward.

## Routes

Everything except auth, share links, the Paddle webhook, and static files requires a session.

```
GET    /                          logged out: what the app is + log in / sign up
                                  logged in: tracker cards grouped by household (name, icon, accent, summary, log button)
GET    /signup, POST /signup
GET    /login,  POST /login       password login
POST   /login/link                email a magic link
GET    /login/link/{token}        confirm page; the form posts itself (GET doesn't use the token)
POST   /login/link/{token}        use the magic link; the first use also confirms the address
GET    /verify/{token}            confirm page; GET does not consume the token
POST   /verify/{token}            confirm the address
GET    /verify/done               confirmation result
POST   /logout
GET    /settings                  password, low-profile time zone, resend verification
POST   /settings/timezone         set the stored IANA zone from a friendly name
POST   /settings/password         set or change, only after the email is verified; logs out other sessions
POST   /settings/verify           resend the confirmation email

POST   /households                            create a household; the creator is its owner
GET    /households/{hid}                      members, invite link, trackers; owners also see a collapsed archived list
POST   /households/{hid}                      rename (owner)
POST   /households/{hid}/leave                leave as a regular member; refused for owners
POST   /households/{hid}/invite               turn on or regenerate the invite link (owner)
POST   /households/{hid}/invite/delete        turn invites off (owner)
POST   /households/{hid}/members/{uid}/delete remove a member (owner; refused for any owner, including yourself)
POST   /households/{hid}/members/{uid}/owner  make a member an owner (owner; no demotion route)
GET    /join/{token}                          invitation page (log in or sign up first); does not join
POST   /join/{token}                          join as a member

GET    /trackers/new              new tracker form (household, icon, accent, log label, summary display)
GET    /trackers/{id}/edit        edit form, share link controls, archive (owner)
POST   /trackers                  create tracker
GET    /trackers/{id}             tracker page: log control and today's entries first, then history; trend from phase 3
POST   /trackers/{id}             rename, icon, accent, log label, summary display (owner)
POST   /trackers/{id}/archive     archive; clears the share link (owner)
POST   /trackers/{id}/restore     restore an archived tracker, sharing stays off (owner)
POST   /trackers/{id}/move        move to another household the user owns; entries untouched (owner of both)
POST   /trackers/{id}/delete      permanently delete, with confirmation (owner, phase 5, not built)
POST   /trackers/{id}/share       turn on or regenerate share link (owner)
POST   /trackers/{id}/share/delete turn share link off (owner)

POST   /trackers/{id}/quick       log "now" (the card's log button)
POST   /trackers/{id}/entries     log now with an optional note; owners may set the time (backfill)
POST   /trackers/{id}/repeat      log "now" with the last value (phase 4)
POST   /trackers/{id}/zero        record none for today (any member) or an earlier day (owner)
POST   /entries/{eid}/undo        delete an entry created in the last 15 minutes (any member)
POST   /entries/{eid}             edit time or note (owner)
POST   /entries/{eid}/delete      delete any entry (owner)
POST   /zeros/{zid}/undo          undo a recorded zero from the last 15 minutes (any member)
POST   /zeros/{zid}/delete        clear an older recorded zero (owner)

GET    /s/{token}                 share page: name, icon, the same summary line as the card, log button, today's entries
POST   /s/{token}/quick           log "now" via the link
POST   /s/{token}/zero            record none for today via the link
POST   /s/{token}/entries/{eid}/undo  undo a recent entry via the link
POST   /s/{token}/zeros/{zid}/undo    undo a recent recorded zero via the link

GET    /charts                    not registered; overlay is phase 3

GET    /billing                   not registered; upgrade, portal, and webhook are phase 5

GET    /static/...                embedded files
GET    /healthz                   no session; 200 `ok` when `SELECT 1` succeeds, otherwise 503 with no error text. There is no `/metrics` route
```

Later offline sync reuses `POST /trackers/{id}/quick`, `POST /s/{token}/quick`, and, once it exists, `POST /trackers/{id}/repeat`. Those requests add a client id and the tap time. Without them, the posts behave as they do now. See Offline logging.

## HTMX

The render helper checks `HX-Request` and returns either the full page or a partial. Ad and consent scripts live in `layout.html`, outside any element HTMX swaps, so they load once per page and are not re-run by partial updates.

- **Forms post, then redirect.** Every form works as a plain `POST` that redirects back to its page. With HTMX, the form also has `hx-post`; HTMX follows the redirect, the page comes back as its content block, and `<main>` is swapped in place. Handlers have one code path either way.
- **Log button**: a small delegated handler in `app.js` updates the summary on tap before the response arrives. The optimistic line follows the tracker's summary display: the next Times today count, "Done today", or "Last" at the current time. The server response replaces it. If the request fails or never reaches the server, the previous line is restored and a short inline message says it was not saved. Nothing is queued. The Done today attribute value is `done`. The later offline path is separate. See Offline logging.
- **Tracker pages refresh themselves.** Home, the tracker page, and the share page include a hidden element that re-fetches the page's own GET every 30 seconds, and when the tab becomes visible again (`hx-trigger="every 30s [trackerIdle()], visibilitychange[trackerIdle()] from:document"`). The response is the usual content block, swapped into `<main>`, so the element replaces itself and its timer starts over. `trackerIdle()` in `app.js` skips the refresh while the tab is hidden, a field in the page has focus, any `<details>` is open, or another HTMX request is in flight. Any other HTMX request aborts a refresh already on its way, so it cannot land after a log. Edit forms, the household page, settings, and account screens do not refresh.
- **Errors swap too.** The `htmx-config` meta tag swaps 4xx responses, so a validation message or "too late to undo" appears in place.
- Signup, login, and change password stay full-page posts. A successful login has to replace the header, which lives outside the HTMX swap target. The Turnstile script is in the layout of those pages only. A failed submit is a new page with a new widget, because the previous token cannot be reused. If an HTMX response does include the form, `app.js` renders a widget for any `.cf-turnstile` element that is not already mounted.
- Owner deletes, member removal, promotion, invite regeneration, leaving a household, and moving a tracker use `hx-confirm`.

## Authentication

- Signup stores an email and starts a session. It does not store a password. The person can use the app in that browser before the mailbox is confirmed.
- A password is set in Settings, and only after `EmailVerifiedAt` is set. Password login also requires that timestamp. An unverified address therefore has no password credential.
- The first proof of the mailbox, either the verification link or a magic link, runs in one transaction: clear `PasswordHash`, delete every session for that user, and set `EmailVerifiedAt`. A magic link then starts a new session for the person who opened it. A verification link started from the signup browser replaces that session so they stay signed in. Opening the link from anywhere else signs the previous browser out and does not log the new one in. An already-verified account is left alone: a later magic link does not clear the password or other sessions.
- Signup and resend send a confirmation link. The message tells the recipient to open it even if they did not sign up, because leaving it unused can leave someone else signed in on that address. A magic link for an address that is not confirmed yet says the same. A magic link for an address that is already confirmed still says an unexpected message can be ignored.
- `GET /login/link/{token}` and `GET /verify/{token}` do not consume the token. The confirm page submits its form when JavaScript runs, and the button still works without it. `POST` consumes the token, with a single conditional update so two submissions cannot both succeed.
- Sessions last 30 days. Renewal happens on the next request after half the lifetime, not on every request. The cookie holds the raw token. The `sessions` row holds its sha256 hash.
- Changing the password deletes other sessions.
- Requesting a magic link returns the same page whether or not the account exists. The same page is shown when the address has been sent too many authentication emails. Login and verification mail are also limited per normalized recipient address, 3 every 15 minutes, in addition to the per-IP limits.
- Passwords are hashed with bcrypt. Empty `PasswordHash` means magic links only.

Which relay sends production mail (Postmark, Amazon SES, Resend, or another SMTP provider) is still open. The mailer stays provider-neutral SMTP, so phase 2 is built and tested with the console logger or MailHog. The relay must be chosen and delivering real magic links before the first public deploy.

## Icons

Copy in only the SVGs a screen uses. Inline them with a template function so they are not `<img>` requests:

```go
//go:embed icons/*.svg
var iconFS embed.FS

func icon(name string) (template.HTML, error) {
    b, err := iconFS.ReadFile("icons/" + name + ".svg")
    return template.HTML(b), err
}
```

When copying an SVG in, add `aria-hidden="true"`. A missing chrome icon name is a template error, so a test that renders each page catches typos.

A tracker icon is not looked up the same way. `Icon` is either empty (render the tally mark), a name on the tracker-picker allow-list (inline that SVG), or an allow-listed emoji (write the character). An emoji is text, not a file in `icons/`. A stored name that is no longer on the list renders as the tally mark, so removing a picker option does not break old rows. UX rules are in `DESIGN.md`.

## Charts

**Not built.** Phase 3. Chart.js (`chart.umd.min.js`) is the planned library. It is not vendored. The server computes series in Go, using the same time-zone grouping as counts, and embeds them as JSON in a `<script type="application/json">` tag. A small script passes that JSON to Chart.js. The primary series includes a recorded zero as zero and omits a day with nothing logged.

Phase 3 has two steps. The tracker page loads Chart.js when it shows a historical chart (counts in phase 3; numbers and durations in phase 4). The overlay page loads it when a second tracker's events are placed on that series. How the mark is drawn is still open (`PLAN.md`); the handler supplies the second tracker's dates on the same axis. The log form does not depend on the script. Phase 2 does not load Chart.js. Event-relative alignment is a later idea in `PLAN.md` and is not computed here.

## Security

- Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`. The database stores the sha256 of the cookie value, with an expiry. `ENV=dev` turns off `Secure`.
- CSRF: `http.CrossOriginProtection` on all non-GET requests, including share-link posts.
- Response headers on every page: `X-Content-Type-Options: nosniff`, `Content-Security-Policy: frame-ancestors 'none'`, and `Referrer-Policy: same-origin`. Share pages replace the referrer policy with `no-referrer` and add `X-Robots-Tag: noindex`. There is no broader content-security policy.
- **Share links are bearer tokens.** Anyone holding the URL can log entries. Link visitors can only undo entries from the last 15 minutes.
- `html/template` escapes output.
- Request logs record the route pattern for login links, verification links, share links, and invite links (`/login/link/{token}`, `/verify/{token}`, `/s/{token}`, `/join/{token}`). The raw token is not written.
- Redirects after login use `safeNext`. It parses the value with `net/url` and allows only a local path. Absolute URLs, scheme-relative URLs, backslashes, and control characters fall back to `/`.
- Per-IP fixed-window rate limits on password login, magic-link requests, signup, and share-link posts. The client address is `RemoteAddr` unless `TRUSTED_IP_HEADER` names `Fly-Client-IP`, `CF-Connecting-IP`, or `X-Forwarded-For`. Nothing else is consulted, and there is no fallback from one header to the other. The in-memory map stops admitting new keys at 10,000. See Deployment for what the proxy must do before that header is set.
- Authentication and verification mail is also limited to 3 messages per recipient address every 15 minutes. When that limit is hit, the response is the same as a successful send.
- Cloudflare Turnstile protects signup, password login, magic-link requests, and change password. The browser widget is not sufficient: the server checks `cf-turnstile-response` with Siteverify before the action. Turnstile does not replace CSRF or the rate limits above. Those still run, and a failed verification still counts toward the rate limit.
- The Paddle webhook is not built. Signature checking for it belongs with billing, in phase 5.

## SQLite

Set on connect:

- `PRAGMA journal_mode=WAL;` (concurrent reads while writing)
- `PRAGMA foreign_keys=ON;`
- `PRAGMA busy_timeout=5000;`
- `PRAGMA synchronous=NORMAL;`

`database/sql` uses `SetMaxOpenConns(1)`. One connection avoids competing writers on a single-process SQLite database. Ordinary requests still issue the small queries they need. That is intentional.

`DB_PATH` defaults to `data/trackanything.db` locally and `/data/trackanything.db` in production. The `data/` directory is created if needed.

## Installable web app

- `manifest.webmanifest` with `name`, `start_url: "/"`, `display: "standalone"`, and 192 and 512 icons. The 512 icon is also the maskable icon; the artwork has safe-zone padding.
- Apple touch icon and `apple-mobile-web-app-capable`, since iOS uses Share → Add to Home Screen.
- No service worker through phase 5, so deploys do not leave stale CSS and JS. Add a minimal worker only if the install option is missing on a real phone, and that fallback does not cache pages. The later offline-logging worker is specified under Offline logging. While online it prefers the network, so a deploy still reaches the new pages.
- The manifest is served as `application/manifest+json`.

## Offline logging (later)

Specified now, built later. It is outside phases 0–5. Product scope is in `PLAN.md`. The pending mark is in `DESIGN.md`.

The app stays server-rendered Go templates and HTMX. Offline support is a thin addition: a small service worker, a queue in IndexedDB, and the existing log handler in `app.js`. It adds no Go dependency, no server-side queue, and no frontend framework. There is no local copy of the database.

**Cache.** The worker stores the shell (the HTML, CSS, and script needed to show tracker cards and the log button) and a snapshot of trackers already loaded: id, name, icon, accent, log label, summary display, and the summary state the optimistic update needs. That is enough to recognize a tracker and tap Log. A share page already loaded contributes that one tracker. History, charts, forms, households, settings, and account pages stay out of this cache. While a request can reach the server, the worker uses the network, so a deploy is what the browser runs. The cache is the fallback when that request fails.

**Queue.** Each pending entry is one row in IndexedDB on that device: the tracker id, the one-tap POST the card already uses (`/trackers/{id}/quick`, `/s/{token}/quick`, or `/trackers/{id}/repeat` once phase 4 exists), a client-generated id (`crypto.randomUUID()`), and the occurrence time from the device clock at the tap. When that tap repeats a value, the value is stored on the same row. The script writes the row and updates the summary in the same tap that already does the optimistic line. If today's entries are already on the page, it adds the pending row there too. Another browser does not see the queue.

**Sync.** When the open app can reach the server again, and on the next load if rows are still pending, each row is sent with an ordinary POST. The request carries the client id and the recorded occurrence time, and it follows the same CSRF and permission checks as the live log button. A success response removes that row from IndexedDB. The server reply replaces the summary, as it already does online. A failure, including a lost session or a rate limit, leaves the row queued and the card on the quiet "Not synced" status. The next successful reach tries again. Sync does not use the browser Background Sync API and does not add a server worker. A synced entry clears a recorded zero on the local day of its occurrence time, in that same request, as an online log does.

**Idempotency.** The server stores the client id on the entry. A unique index means a retry for an id that already exists returns that row and inserts nothing. Ordinary online logs leave the id unset. SQLite allows many NULLs in a unique index, so those rows stay out of it.

```go
ClientID *string `gorm:"uniqueIndex"` // set by offline sync; nil for an ordinary online log
```

**Time.** The queued occurrence time is an absolute UTC instant from the device clock at the tap. The server stores it as `OccurredAt`. `CreatedAt` is when the server writes the row during sync. Which calendar day the entry belongs to still uses the account zone, or the household first owner's zone on a share page. The optimistic clock uses that same stored zone. The 15-minute undo window still uses `CreatedAt`, and only after the entry exists on the server. This path is the one place a non-owner occurrence time is accepted, and only together with a new client id. It is separate from owner backfill on `POST /trackers/{id}/entries`. A normal log POST from a member or a share link still ignores a submitted time. Whether to reject a tap time far from the server clock is open. See Open questions.

The cases to pin down are under Testing. They wait until this enhancement is built.

## Billing (phase 5)

**Not built.**

- Checkout: `/billing` loads Paddle.js from Paddle's CDN, only on that page, and opens overlay checkout for `PADDLE_PRICE_ID` with the user's email and `customData: {"user_id": ...}`.
- `PADDLE_ENV=sandbox` points the API at `sandbox-api.paddle.com` and tells Paddle.js to use sandbox.
- Webhook events `subscription.created`, `subscription.updated`, and `subscription.canceled` set `Plan`, `PaddleCustomerID`, `PaddleSubscriptionID`, and `SubscriptionEndsAt` from the current billing period end.
- `POST /billing/portal` asks Paddle for a customer portal session and redirects there. The app builds no card, invoice, or cancellation UI.
- Grace period: 2 days past `SubscriptionEndsAt` before treating the user as free.
- Local webhook testing uses a tunnel (for example `cloudflared tunnel --url http://localhost:8080`) or Paddle's simulator against the deployed app. Automated tests sign payloads themselves against a test secret and point the portal handler at a stub API URL.
- Ad and consent scripts are omitted entirely for ad-free users, users in the grace period, and on share, login, and settings pages.

## Metrics

**Not built.** There is no `/metrics` route and no Prometheus client in the module. What follows is the intended shape if it is added later. It is not required for the first public deploy.

Server metrics would be collected in Prometheus. The process would expose them on `GET /metrics` in the Prometheus text format. A scraper would pull that endpoint. The app would not run Prometheus, and it would not push metrics.

What would be collected is operational: request counts, latency, and errors, and whether the database check behind `/healthz` succeeds. Nothing about a person, a household, or a tracker would be included. Logs stay `log/slog` to stdout. Product charts stay Chart.js, as in `PLAN.md`. Prometheus is not that analytics, and it is not a second charting stack.

`github.com/prometheus/client_golang` would be the exposition library. On the homelab, Prometheus would scrape the Dokku app over the private network. The public ingress would not need to publish `/metrics`.

## Deployment

### Initial: homelab behind Fly.io ingress

The app runs on the homelab server ("box") under Dokku. Fly.io provides only a stable public IP and TLS termination, and reaches the house over WireGuard. The router has no public port forwarding. The app itself stays unaware of this topology.

```
Browser
  │ HTTPS
  ▼
Fly Proxy (trusted public ingress; sets Fly-Client-IP)
  │
  ▼
Fly nginx (overwrites Fly-Client-IP before forwarding)
  │ plain HTTP over WireGuard
  ▼
box:80 (Dokku's nginx, routes by Host header)
  │
  ▼
trackanything container (Go binary, plain HTTP on $PORT)
```

- **No TLS in the Go server.** It listens on plain HTTP (`ADDR`, or Dokku's `$PORT`).
- **Health check**: `GET /healthz` needs no session. `200` and a body of `ok` means the process is up and SQLite answered `SELECT 1`. A failed database check is `503` with a generic body; the error is written to the process log. Dokku or the ingress can probe this path.
- **Build**: a multi-stage `Dockerfile` (`CGO_ENABLED=0`, then a minimal image with the binary). Templates, static files, and time zone data are embedded.
- **Deploy**: `git push dokku main`.
- **SQLite storage**: `dokku storage:mount` a host directory, e.g. `/var/lib/dokku/data/storage/trackanything:/data`, with `DB_PATH=/data/trackanything.db`. Exactly one instance.
- **Host header must survive the chain**, for Dokku's routing and the CSRF origin check.
- **`Secure` cookies still work**, because the browser sees HTTPS.
- **Turnstile.** Production sets `TURNSTILE_SITE_KEY` and `TURNSTILE_SECRET_KEY`. The trackanything.io widget is Managed mode. Signup, login, and change password verify the token server-side. Turnstile does not replace CSRF or rate limiting.
- **Client IP.** The current production deployment sets `TRUSTED_IP_HEADER=Fly-Client-IP` because Fly Proxy is the trusted public ingress. Fly Proxy writes the original visitor address in `Fly-Client-IP`. Fly nginx overwrites that header before the request crosses WireGuard to Dokku, so a client-supplied value does not survive. `Fly-Client-IP` is one address, the same shape as `CF-Connecting-IP`. Leave `TRUSTED_IP_HEADER` empty until the origin cannot be reached except through the proxy that sets the header. `CF-Connecting-IP` stays valid when that proxy replaces the header itself. `X-Forwarded-For` is a list; the app uses the first address only when `TRUSTED_IP_HEADER` is set to `X-Forwarded-For` on purpose, after a proxy replaces the header rather than appending to it. The app does not fall back from one header to another and does not assume the configured header is present.
- **Backups**: [Litestream](https://litestream.io) to S3-compatible storage, or a nightly `sqlite3 .backup` cron job on box to start. The choice can wait while phase 2 is built, but before the first public deploy a method and destination are chosen, configured, and tested by restoring a copy. Live from the first deploy, no exceptions. How long backups keep deleted data stays open until the phase 5 privacy policy.
- **Before the first public deploy**: the production email relay sends a magic link end to end, and backups run and restore.

### Later: hosting directly on Fly.io

If real usage justifies it, move the app itself to Fly.io. The same Docker image runs there, with SQLite on a Fly volume (still one instance) and Litestream for backups. Because the app has no TLS or host-specific setup, the move is a redeploy plus a data copy.

## Testing

Standard library only: `testing` and `net/http/httptest`. Each test gets its own SQLite database (`:memory:` or a temp file) with `AutoMigrate`. Tests use a cookie jar so a flow can log in and keep going. The mailer is swapped for an in-memory one that records sent links.

Three layers:

1. **Pure logic**, table-driven, no HTTP: day boundaries in an IANA time zone including DST spring-forward and fall-back; per-day series for charts, with a recorded zero as zero and an unlogged day omitted; the undo window; which entry carries forward (a backfilled older entry must not win).
2. **Handlers**: every route has a happy path and the failures that matter: bad input, CSRF rejection, logged-out redirect, expired session, a non-member, and a member attempting an owner-only action.
3. **Flows** through the cookie jar, matching each phase's "done when" in `PLAN.md`.

What the suite has to pin down:

- **Accounts.** Signup does not store a password. Password login works after the email is confirmed and a password is set in settings. A password or session created before that confirmation does not survive the verification link or the first magic link. Magic link (works once, fails when expired or reused, stored only as a hash), logout, session expiry, session IDs stored only as a hash, rate limits, email case-insensitivity. Turnstile: production config requires both keys and development can omit them; a successful Siteverify response allows signup, login, and password change; a missing token, `success: false`, a network error, a malformed response, and a non-200 response reject the action. CSRF and rate limits still apply. The suite does not call Cloudflare.
- **Households.** A new user gets a "My trackers" household as owner. The phase 1 backfill gives each user without a household a "My trackers" household as owner, and running it twice creates nothing new. Invite link joins as member; a regenerated or disabled link fails. Owners can remove members and promote members to owner; members can't. Removing an owner, including yourself, is refused and nothing changes. When two households on the dashboard share a name, the one someone else created shows that person's email in its heading. A name that matches nothing else shows no email.
- **Household organization.** Creating a household makes the creator its owner. Owners can rename one, including a personal "My trackers". Members can't, and an empty or too-long name is rejected with nothing changed. Creating or renaming so the trimmed, case-insensitive name matches another household that person owns is rejected and nothing changes. "Family" and " family " cannot both be owned by one person. Another user can own "Family". Belonging to someone else's "Family" does not block owning your own. Renaming a household to its own current name is allowed. When two households on the same dashboard share a name under that comparison, the one someone else created shows its creator's email. A name that matches nothing else shows no email. Moving a tracker between two households the user owns keeps the same tracker id, every entry, and every recorded zero, and it creates no copies. An active share link keeps its token and still opens the tracker after the move. The move does not turn the link off or replace it. Moving into a household where the user is only a member, or one they can't see, is refused and nothing changes. A member of the old household who isn't in the new one gets a 404 for the tracker and its entries afterward. A member of the destination can see and log it. A regular member can leave, and their row is gone. An owner posting to the leave route is refused and nothing changes. Leaving one household leaves the user's personal household and other memberships as they were.
- **Archive and restore.** Archiving hides the tracker from the dashboard, makes its tracker page and entry routes 404, and clears its share link. The owner's household page lists it under archived trackers; a member's doesn't. Restore brings it back with its entries and with sharing off. Members can't archive or restore.
- **Permissions.** For every owner-only route, a member gets refused and nothing changes. Non-members get 404 for trackers, entries, and households.
- **Share links.** Logging and undo work without a session. The share page shows only that tracker. Undo fails after 15 minutes. Regenerated, disabled, and archived-tracker links return 404, and so does the old link after a restore. Owner-only actions are unreachable through a link.
- **Logging.** The log button records now, owner backfill, notes from members and owners, undo within the window, undo refused after it, owner delete, `RecordedByID` and `ViaLink` set correctly. A member who submits a time on an ordinary log gets an entry at the current time, never the submitted one. The member's tracker page has no time field. An empty log label renders as "+ Log". A label longer than 24 characters is rejected. The log label and the summary display are stored independently.
- **Summary display.** Omitted or empty stores `times`. `done` and `last` store as sent. Any other value, including `boolean`, is rejected and nothing changes. The summary line on the home card, the tracker page, and the share page follows `PLAN.md`: Times today ("Nothing logged today", "1 time today", "3 times today", "None today"); Done today ("Done today" with one or many entries today, "Not done today" with none, including when today is a recorded zero or the only entries are on other days); Last occurrence ("Never logged", "Last: 6:42 AM" today, "Last: Yesterday, 8:15 PM" on the previous local day, "Last: Mon, Jan 2, 8:15 PM" when older). A second entry on a Done today tracker is stored. Last occurrence uses the latest `OccurredAt`, stays after midnight, and ignores a recorded zero. Undo and delete recompute the line. History rows stay count, none, or nothing logged. The default `times` column leaves existing phase 2 cards on the Times today wording.
- **Icons and accents.** An empty icon renders as the tally mark and an empty accent adds none. A picker value is stored. A value outside the allow-list is rejected. The name is present wherever the icon is.
- **Recorded zeros.** Marking today as none does not increment the count. An event that day deletes the mark. A day with neither is absent from the per-day series. A recorded zero is present as zero. A member or share link can undo a mark from the last 15 minutes. Clearing an older mark, or marking an earlier day, is owner-only, and a member's attempt changes nothing.
- **Time.** "Today" and per-day grouping follow the viewer's stored time zone (or the household owner's on share pages), not the browser's current zone. DST changes don't double-count or skip a day.
- **Charts.** The embedded series matches the per-day counts, omits unlogged days, and includes a recorded zero as zero. Overlay data is the other tracker's event days on that same axis. The tracker page's log form does not require Chart.js. Event-relative alignment is not part of this suite until that later idea is built.
- **Values (phase 4).** Number and duration entries, prefill from the last entry, **Log again**.
- **Deletion (phase 5).** Permanent tracker delete removes the tracker and its entries, owner only, with confirmation.
- **Ads and billing (phase 5).** No ad markup for ad-free users, users in the grace period, or on share/login/settings pages. The Paddle webhook rejects bad signatures and old timestamps, applies an event once even if delivered twice, ignores an older event arriving after a newer one, finds the user through `custom_data`, and sets and clears the plan. Tests build the `Paddle-Signature` header the way Paddle does, against a test secret, and the portal handler talks to a stub API URL.
- **Install.** The manifest is served with the right content type and names `standalone` and both icon sizes.
- **Offline logging (later).** A repeated sync with the same client id inserts one entry and returns it again on retry. `OccurredAt` is the tap time sent with that id. `CreatedAt` is the sync, and the undo window uses it. A member or share-link log that omits the client id still stores the server's current time. The cached snapshot is enough to show the tracker name, icon, and log button. History and charts are not required for the log tap.
- **Health.** `GET /healthz` needs no session. It returns 200 `ok` when `SELECT 1` succeeds, and 503 with no database error text when that query fails. The failure is logged.
- **Metrics.** Not built. No test requests `/metrics`.

## Decision log

| Topic | Current decision | Notes |
| --- | --- | --- |
| Implementation | Go web app | One binary, standard library where it is enough |
| Frontend | Go templates + HTMX + Pico.css | No SPA. Forms work without JavaScript |
| Charts | Chart.js, vendored | JSON embedded by the server. Historical chart, then overlay, both phase 3. Not loaded in phase 2. Logging works without it. No frontend framework. |
| Tracker icon | Optional allow-listed string | Empty renders the tally mark. Emoji are text. A removed picker name falls back to the tally mark. |
| Summary display | `times`, `done`, or `last` on `Tracker` | Empty and the column default are `times`. Presentation only. Wording in `PLAN.md`. Shipped after phase 2. |
| Schedules and reminders | Not specified | Later product direction in `PLAN.md`. No schema, notification channel, or routes until that direction is ready to build. |
| Recorded zero | One row per tracker per local day | Not an entry. Cleared when an event is logged that day. |
| Entry time | Server sets `OccurredAt` to now for members and share links | Only owners' submitted times are read on an ordinary post. Later offline sync stores the tap time for a new client id. See Offline logging |
| Archive | `ArchivedAt` plus clearing `ShareToken` | Restore leaves sharing off. Permanent delete is phase 5 |
| Phase 1 households | One-time idempotent backfill after `AutoMigrate` | No request-time fallback. Deleted once existing databases have run it |
| Household organization | Existing `Household`, `HouseholdMember`, and `Tracker.HouseholdID` | Shipped after phase 2. No new tables or columns. One owner cannot have two names that match after trim and case folding. Move updates `HouseholdID` in one transaction, requires owner on both sides, and keeps `ShareToken`. Member-only leave. No household deletion, owner leave, demotion, or transfer |
| Persistence | SQLite, one shared database | Database per user rejected: migrations, backups, and sharing all get worse |
| Dependencies | GORM, pure-Go SQLite, `x/crypto` | No Paddle SDK. No Prometheus client. No server-side queue |
| Metrics | Not built | A Prometheus scrape is a later idea, not part of the running app |
| Time zone | UTC timestamps, stored IANA zone | Detected once at signup. Not revised from the browser. Grouped in Go |
| Email | `net/smtp`, console logger in dev | Optional `.env`. Relay vendor still open |
| Bot protection | Cloudflare Turnstile, Managed mode | Signup, login, and change password. Server-side Siteverify. Does not replace CSRF or rate limits |
| First public deploy | Dokku on the homelab, Fly.io as ingress only | App has no TLS and no host-specific code, so a later move to Fly.io is a redeploy |
| Offline logging | Later, around the existing log POST | Browser cache and IndexedDB queue. Client id for idempotent retry. `OccurredAt` is the tap; `CreatedAt` is the sync. No server queue and no client framework |

## Open questions

- **Email relay** for magic links: Postmark, Amazon SES, Resend, or another provider with SMTP? Doesn't block building phase 2; must be answered and working before the first public deploy.
- **Backup and export**: method and destination (before the first public deploy), format, and how long backups retain deleted rows (with the phase 5 privacy policy). The privacy policy has to state the retention; that product question is also in `PLAN.md`.
- **Offline tap time:** sync stores the device's tap time. Whether to reject a time far from the server clock is undecided. Ordinary log posts still ignore a submitted time.
