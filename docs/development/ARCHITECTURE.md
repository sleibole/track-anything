# Track Anything — Architecture

How Track Anything is built. What the product is is `PLAN.md`. How it should look and behave is `DESIGN.md`. User help, when it exists, belongs in `docs/help/`.

If `PLAN.md` or this document describes a behavior, `make test` covers it.

## Constraints

- **Boring technology.** Standard Go, server-rendered HTML, SQLite, straightforward code.
- **One process, one database.** No microservices, distributed databases, queues, or background workers. Mail is sent in the request. The rate limiter is in memory. Do not add a job runner without a demonstrated need.
- **No schema language or plugin system** before a concrete tracker type requires one.
- **Do not optimize for hypothetical scale** before the product has users. Exactly one instance, because of SQLite.
- **Direct Go dependencies:** GORM, `github.com/glebarez/sqlite`, `golang.org/x/crypto`, and `github.com/prometheus/client_golang` for metrics. Everything else is the standard library, vendored static files, or HTTP calls.
- **No offline mode and no public API.**
- **JavaScript is an enhancement.** Forms use `POST` and work without it. HTMX upgrades the same forms. Chart.js is the only focused client-side library, and the only chart library. It loads only where a historical chart or an overlay is shown, from phase 3 on, and the log control works without it. Phase 2 does not load it. There is no React, Vue, SPA, or second charting stack. Signup, login, and change password are the exception: those forms require Cloudflare Turnstile, described under Security.

## Stack

| Concern | Choice | Notes |
| --- | --- | --- |
| Language | Go (latest stable, 1.22+) | Needed for method + wildcard routing in `net/http` |
| HTTP routing | `net/http` `ServeMux` | Patterns like `GET /trackers/{id}` |
| Templates | `html/template` | Auto-escaping, layouts via `{{block}}` / `{{template}}` |
| Static files and templates | `embed` | Ship a single binary |
| Logging | `log/slog` | Structured logs to stdout |
| Metrics | Prometheus | The process exposes metrics for a scraper. See Metrics |
| ORM | GORM (`gorm.io/gorm`) | `AutoMigrate` for schema |
| Database | SQLite, one shared app database | A database per user was considered and rejected: it complicates migrations, backups, and especially sharing |
| SQLite driver | `github.com/glebarez/sqlite` | Pure-Go GORM driver, so no CGO and easy cross-compiling |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Email (magic links) | `net/smtp` to a transactional email relay | In development, links are logged to the console instead of sent |
| Frontend | HTMX (vendored `htmx.min.js`) | Served from `/static`, no CDN |
| Charts | Chart.js (vendored `chart.umd.min.js`) | Fits server-rendered HTML and HTMX. Only where a historical chart or overlay is shown (phase 3+). Logging does not depend on it. Not loaded in phase 2. |
| CSS | Pico.css (vendored `pico.min.css`) | Classless-first, minimal custom CSS. Visual rules are in `DESIGN.md` |
| Icons | Tabler, individual SVGs vendored and embedded | MIT. Inlined so `stroke="currentColor"` follows Pico, including dark mode. A tracker's own icon is an allow-listed Tabler name or emoji. |
| Payments | Paddle Billing via `net/http` | No Paddle SDK. Paddle is the merchant of record |
| Ads | Google AdSense with a certified consent platform | Phase 5, after the app is live |
| Bot protection | Cloudflare Turnstile | Signup, login, and change password. Verified with an HTTP call; no SDK |

## Project layout

Start flat: one `main` package, split only when navigation or coupling becomes painful. If it grows, move toward `cmd/web` (startup), `internal/app` (server, routes, shared helpers), `internal/tracker`, `internal/store`, and `ui/templates` + `ui/static`, organized by feature rather than handler/service/repository layers.

```
track-anything/
├── docs/
│   ├── development/    # PLAN.md, DESIGN.md, ARCHITECTURE.md
│   └── help/           # future user-facing Markdown; none yet
├── go.mod
├── main.go             # config, load .env, open DB, migrate, build mux, start server
├── db.go               # GORM setup, AutoMigrate, SQLite pragmas, one-time data backfills
├── models.go           # structs below
├── days.go             # day boundaries, per-day counts, undo window, summaries
├── access.go           # trackerForUser, trackerForShareToken, archivedTrackerForOwner, household lookups
├── auth.go             # signup, login, logout, magic links, session middleware
├── mail.go             # net/smtp sender, console sender for development
├── env.go              # load .env at startup; existing environment variables win
├── .env.example        # local MailHog settings; copy to .env (gitignored)
├── settings.go         # time zone and password settings
├── ratelimit.go        # per-IP fixed-window limiter, client IP behind the proxies
├── handlers.go         # health check, dashboard, shared handler helpers
├── handlers_trackers.go # create, edit, archive, restore, share link, tracker page
├── handlers_entries.go  # log, undo, owner corrections, recorded zeros
├── handlers_households.go # members, invite link, join, archived trackers
├── handlers_share.go   # share-link pages, no login
├── handlers_charts.go
├── handlers_billing.go # upgrade page, Paddle webhook, customer portal link (phase 5)
├── render.go           # template loading + render helper (full page vs HTMX partial)
├── middleware.go       # logging, recover, auth, CSRF check
├── templates/
│   ├── layout.html     # ad and consent scripts live here, outside any HTMX swap target
│   ├── dashboard.html
│   ├── tracker_show.html
│   ├── tracker_form.html # new and edit, plus the share link and archive
│   ├── share.html      # what a share-link visitor sees
│   ├── household.html
│   ├── join.html       # invitation confirm page
│   ├── message.html    # 404, 403, and other one-line answers
│   ├── charts.html
│   └── partials/       # fragments shared by pages: card, log control, entry
├── icons/              # Tabler SVGs we use, inlined by the icon template func
├── static/
│   ├── htmx.min.js
│   ├── chart.umd.min.js
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

Production sets the same names with `dokku config:set`: `ADDR`/`PORT`, `DB_PATH`, `BASE_URL`, `ENV`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`, `TURNSTILE_SITE_KEY`, `TURNSTILE_SECRET_KEY`. Phase 5 adds `PADDLE_ENV` (`sandbox` or `production`), `PADDLE_API_KEY`, `PADDLE_CLIENT_TOKEN`, `PADDLE_WEBHOOK_SECRET`, `PADDLE_PRICE_ID`.

`ENV=dev` turns off the `Secure` cookie flag so browsers work on `http://localhost`.

## Data model

Tokens (`InviteToken`, `ShareToken`, session IDs, login tokens) are 32 random bytes from `crypto/rand`, base64url-encoded. Login tokens are stored only as a sha256 hash.

```go
type User struct {
    ID           uint
    Email        string `gorm:"uniqueIndex;not null"` // stored lowercased
    PasswordHash string // empty if they only use magic links
    TimeZone     string `gorm:"not null;default:UTC"` // IANA name, e.g. "America/Los_Angeles"
    Plan         string `gorm:"not null;default:free"` // "free" or "adfree" (phase 5)
    PaddleCustomerID     string // "ctm_..."
    PaddleSubscriptionID string // "sub_..."
    SubscriptionEndsAt   *time.Time
    CreatedAt    time.Time
}

type Session struct {
    ID        string `gorm:"primaryKey"` // random 32-byte token, base64url
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

type Household struct {
    ID          uint
    Name        string  `gorm:"not null"` // "My trackers" for a personal household; no rename UI in phase 2
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
    ID          uint
    HouseholdID uint    `gorm:"index;not null"`
    Name        string  `gorm:"not null"`
    Icon        string  // empty means the tally-mark default. Allow-list only: a curated Tabler name or an emoji.
    Accent      string  // empty means no accent. Allow-list only.
    LogLabel    string  // empty means "+ Log". A few words, 24 characters at most.
    Kind        string  `gorm:"not null;default:count"` // "count", later "number" and "duration"
    Unit        string  // number trackers only: "kg", "lb"
    ShareToken  *string `gorm:"uniqueIndex"` // nil = no share link
    Position    int
    ArchivedAt  *time.Time // archived trackers are hidden; archiving also clears ShareToken
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Entry struct {
    ID           uint
    TrackerID    uint      `gorm:"index;not null"`
    OccurredAt   time.Time `gorm:"index;not null"` // when it happened (UTC); only owners choose or edit it
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

- **`OccurredAt` vs `CreatedAt`**: owners can backfill, so when it happened is separate from when the row was written. The undo window uses `CreatedAt`.
- **Only owners set `OccurredAt`.** `POST /trackers/{id}/entries` reads the time field only when the role is owner, interpreting it in the owner's stored zone. For a member it ignores any submitted time and uses the server's current time, so a hand-built request can't backfill. The note is accepted from both. Share-link posts never read a time.
- **Personal household.** Signup creates the user, a `Household` named "My trackers", and an owner `HouseholdMember` row in one transaction.
- **Phase 1 → phase 2 backfill.** Accounts created in phase 1 have no household. After `AutoMigrate`, `db.go` runs a one-time step in a transaction: every user with no `household_members` row gets a "My trackers" household with that user as owner. It is idempotent, so running it again changes nothing. Request handlers do not check for a missing household. Once every existing database has run the step (local development databases; production first launches with phase 2), the step is deleted.
- **Ownership stays small.** Removing deletes a `member` row. The handler refuses to remove any owner, including the person asking. There is no demotion and no leave route. Account deletion (phase 5) handles sole-owner cases.
- **Value columns on `Entry`** (`Number`, `DurationSec`) instead of a generic field system. Two nullable columns cover the planned tracker kinds with no joins. Fields and sub-trackers, if they are ever built, add a `Field` table, a `Value` table, and `ParentID` on `Tracker` and `Entry`.
- **Email is stored lowercased** so `Sheldon@…` and `sheldon@…` can't become two accounts.
- **Archive, not delete**, for trackers. Archiving sets `ArchivedAt` and clears `ShareToken`. Restoring clears `ArchivedAt` and leaves sharing off, so an old link can't come back. Entries are untouched either way. Permanent delete is a separate confirmed action in phase 5.
- **Icon, accent, and log label** are optional strings. Empty means the default: the tally-mark icon, no accent, and the button text "+ Log". Writes accept only the built-in allow-lists (icon and accent) and a log label of at most 24 characters. A tracker icon that later leaves the picker falls back to the tally mark when rendered. A missing chrome icon is still a template error.
- **`RecordedZero.Day`** is the local calendar date that page already calls "today" or a backfilled day: the viewer's stored zone, or the household first owner's zone on a share page. It is not a UTC timestamp, because the mark is about a day rather than an instant. Logging an entry whose local day matches `Day` deletes that row in the same request. A day with events cannot also have a recorded zero.

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
                                  logged in: tracker cards grouped by household (name, icon, accent, today's summary, log button)
GET    /signup, POST /signup
GET    /login,  POST /login       password login
POST   /login/link                email a magic link
GET    /login/link/{token}        confirm page; the form posts itself (GET doesn't use the token)
POST   /login/link/{token}        use the magic link
POST   /logout
GET    /settings                  password, low-profile time zone, (phase 5) plan, delete account
POST   /settings/timezone         set the stored IANA zone from a friendly name
POST   /settings/password         set or change; logs out other sessions

GET    /households/{hid}                      members, invite link, trackers; owners also see a collapsed archived list
POST   /households/{hid}/invite               turn on or regenerate the invite link (owner)
POST   /households/{hid}/invite/delete        turn invites off (owner)
POST   /households/{hid}/members/{uid}/delete remove a member (owner; refused for any owner, including yourself)
POST   /households/{hid}/members/{uid}/owner  make a member an owner (owner; no demotion route)
GET    /join/{token}                          invitation page (log in or sign up first); does not join
POST   /join/{token}                          join as a member

GET    /trackers/new              new tracker form (pick household, icon, accent, log label)
GET    /trackers/{id}/edit        edit form, share link controls, archive (owner)
POST   /trackers                  create tracker
GET    /trackers/{id}             tracker page: log control and today's entries first, then history; trend from phase 3
POST   /trackers/{id}             rename, icon, accent, log label (owner)
POST   /trackers/{id}/archive     archive; clears the share link (owner)
POST   /trackers/{id}/restore     restore an archived tracker, sharing stays off (owner)
POST   /trackers/{id}/delete      permanently delete, with confirmation (owner, phase 5)
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

GET    /s/{token}                 share page: name, icon, today's summary, log button, today's entries
POST   /s/{token}/quick           log "now" via the link
POST   /s/{token}/zero            record none for today via the link
POST   /s/{token}/entries/{eid}/undo  undo a recent entry via the link
POST   /s/{token}/zeros/{zid}/undo    undo a recent recorded zero via the link

GET    /charts                    overlay a related tracker's events on a historical chart (phase 3, after the per-tracker chart)

GET    /billing                   upgrade page: loads Paddle.js and opens checkout (phase 5)
POST   /billing/portal            redirect to Paddle's customer portal to manage or cancel (phase 5)
POST   /billing/webhook           Paddle webhook, verified by signature (phase 5)

GET    /static/...                embedded files
GET    /metrics                   Prometheus text format, no session
GET    /healthz
```

## HTMX

The render helper checks `HX-Request` and returns either the full page or a partial. Ad and consent scripts live in `layout.html`, outside any element HTMX swaps, so they load once per page and are not re-run by partial updates.

- **Forms post, then redirect.** Every form works as a plain `POST` that redirects back to its page. With HTMX, the form also has `hx-post`; HTMX follows the redirect, the page comes back as its content block, and `<main>` is swapped in place. Handlers have one code path either way.
- **Log button**: a small delegated handler in `app.js` updates the summary on tap before the response arrives.
- **Errors swap too.** The `htmx-config` meta tag swaps 4xx responses, so a validation message or "too late to undo" appears in place.
- Owner deletes, member removal, promotion, and invite regeneration use `hx-confirm`.

## Authentication

- Passwords hashed with bcrypt. Empty `PasswordHash` means magic links only.
- `GET /login/link/{token}` does not consume the token. The confirm page submits its form when JavaScript runs, and the button still works without it. `POST` consumes the token, with a single conditional update so two submissions cannot both succeed.
- Sessions last 30 days. Renewal happens on the next request after half the lifetime, not on every request.
- Changing the password deletes other sessions.
- Requesting a magic link returns the same page whether or not the account exists.

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

Chart.js (`chart.umd.min.js`, vendored) is the chart library. The server computes series in Go, using the same time-zone grouping as counts, and embeds them as JSON in a `<script type="application/json">` tag. A small script passes that JSON to Chart.js. The primary series includes a recorded zero as zero and omits a day with nothing logged.

Phase 3 has two steps. The tracker page loads Chart.js when it shows a historical chart (counts in phase 3; numbers and durations in phase 4). The overlay page loads it when a second tracker's events are placed on that series. How the mark is drawn is still open (`PLAN.md`); the handler supplies the second tracker's dates on the same axis. The log form does not depend on the script. Phase 2 does not load Chart.js. Event-relative alignment is a later idea in `PLAN.md` and is not computed here.

## Security

- Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, stored in `sessions` with an expiry. `ENV=dev` turns off `Secure`.
- CSRF: `http.CrossOriginProtection` on all non-GET requests, including share-link posts.
- **Share links are bearer tokens.** Anyone holding the URL can log entries. Share pages send `Referrer-Policy: no-referrer` and `X-Robots-Tag: noindex` so the token doesn't leak to other sites or search engines. Link visitors can only undo entries from the last 15 minutes.
- `html/template` escapes output.
- Per-IP fixed-window rate limits on password login, magic-link requests, signup, and share-link posts. The client IP is `CF-Connecting-IP`, falling back to the first `X-Forwarded-For` hop. That is trusted only because the app is unreachable except through the proxy chain described under Deployment.
- **Cloudflare Turnstile** on signup, login, and change password. Login covers both the password form and the magic-link request. Each of those forms includes Cloudflare's widget, loaded only on those pages. The handler posts the token to Cloudflare's siteverify endpoint and rejects the request when the token is missing or invalid. Rate limits stay. There is no Turnstile SDK; verification is an HTTP call, same as Paddle. Tests stub that call. Local development uses Cloudflare's always-pass test keys.
- The Paddle webhook verifies `Paddle-Signature` (`ts=...;h1=...`, an HMAC-SHA256 of `ts:rawbody` with the notification secret, checked with `crypto/hmac`) and rejects old timestamps. It is safe to receive the same event twice. Events can arrive out of order, so each update compares the event's `occurred_at` with the last one applied. The user is found through `custom_data.user_id`, falling back to `PaddleSubscriptionID`.

## SQLite

Set on connect:

- `PRAGMA journal_mode=WAL;` (concurrent reads while writing)
- `PRAGMA foreign_keys=ON;`
- `PRAGMA busy_timeout=5000;`
- `PRAGMA synchronous=NORMAL;`

`DB_PATH` defaults to `data/trackanything.db` locally and `/data/trackanything.db` in production. The `data/` directory is created if needed.

## Installable web app

- `manifest.webmanifest` with `name`, `start_url: "/"`, `display: "standalone"`, and 192 and 512 icons. The 512 icon is also the maskable icon; the artwork has safe-zone padding.
- Apple touch icon and `apple-mobile-web-app-capable`, since iOS uses Share → Add to Home Screen.
- No service worker, so deploys do not leave stale CSS and JS. Add a minimal worker only if the install option is missing on a real phone.
- The manifest is served as `application/manifest+json`.

## Billing (phase 5)

- Checkout: `/billing` loads Paddle.js from Paddle's CDN, only on that page, and opens overlay checkout for `PADDLE_PRICE_ID` with the user's email and `customData: {"user_id": ...}`.
- `PADDLE_ENV=sandbox` points the API at `sandbox-api.paddle.com` and tells Paddle.js to use sandbox.
- Webhook events `subscription.created`, `subscription.updated`, and `subscription.canceled` set `Plan`, `PaddleCustomerID`, `PaddleSubscriptionID`, and `SubscriptionEndsAt` from the current billing period end.
- `POST /billing/portal` asks Paddle for a customer portal session and redirects there. The app builds no card, invoice, or cancellation UI.
- Grace period: 2 days past `SubscriptionEndsAt` before treating the user as free.
- Local webhook testing uses a tunnel (for example `cloudflared tunnel --url http://localhost:8080`) or Paddle's simulator against the deployed app. Automated tests sign payloads themselves against a test secret and point the portal handler at a stub API URL.
- Ad and consent scripts are omitted entirely for ad-free users, users in the grace period, and on share, login, and settings pages.

## Metrics

Server metrics are collected in Prometheus. The process exposes them on `GET /metrics` in the Prometheus text format. A scraper pulls that endpoint. The app does not run Prometheus, and it does not push metrics.

What is collected is operational: request counts, latency, and errors, and whether the database check behind `/healthz` succeeds. Nothing about a person, a household, or a tracker is included. Logs stay `log/slog` to stdout. Product charts stay Chart.js, as in `PLAN.md`. Prometheus is not that analytics, and it is not a second charting stack.

`github.com/prometheus/client_golang` is the exposition library. On the homelab, Prometheus scrapes the Dokku app over the private network. The public ingress does not need to publish `/metrics`.

## Deployment

### Initial: homelab behind Fly.io ingress

The app runs on the homelab server ("box") under Dokku. Fly.io provides only a stable public IP and TLS termination, and reaches the house over WireGuard. The router has no public port forwarding. The app itself stays unaware of this topology.

```
Browser
  │ HTTPS
  ▼
Cloudflare (DNS, proxy, browser-facing TLS)
  │ HTTPS
  ▼
Fly.io VM :443 (public ingress, TLS termination, reverse proxy)
  │ plain HTTP over WireGuard
  ▼
box:80 (Dokku's nginx, routes by Host header)
  │
  ▼
trackanything container (Go binary, plain HTTP on $PORT)
```

- **No TLS in the Go server.** It listens on plain HTTP (`ADDR`, or Dokku's `$PORT`).
- **Build**: a multi-stage `Dockerfile` (`CGO_ENABLED=0`, then a minimal image with the binary). Templates, static files, and time zone data are embedded.
- **Deploy**: `git push dokku main`.
- **SQLite storage**: `dokku storage:mount` a host directory, e.g. `/var/lib/dokku/data/storage/trackanything:/data`, with `DB_PATH=/data/trackanything.db`. Exactly one instance.
- **Host header must survive the chain**, for Dokku's routing and the CSRF origin check.
- **`Secure` cookies still work**, because the browser sees HTTPS.
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

- **Accounts.** Signup, password login, wrong password, magic link (works once, fails when expired or reused, stored only as a hash), logout, session expiry, rate limits, email case-insensitivity. Signup, login, and change password reject a missing or failed Turnstile token.
- **Households.** A new user gets a "My trackers" household as owner. The phase 1 backfill gives each user without a household a "My trackers" household as owner, and running it twice creates nothing new. Invite link joins as member; a regenerated or disabled link fails. Owners can remove members and promote members to owner; members can't. Removing an owner, including yourself, is refused and nothing changes. A household someone else created shows that person's email in its dashboard heading.
- **Archive and restore.** Archiving hides the tracker from the dashboard, makes its tracker page and entry routes 404, and clears its share link. The owner's household page lists it under archived trackers; a member's doesn't. Restore brings it back with its entries and with sharing off. Members can't archive or restore.
- **Permissions.** For every owner-only route, a member gets refused and nothing changes. Non-members get 404 for trackers, entries, and households.
- **Share links.** Logging and undo work without a session. The share page shows only that tracker. Undo fails after 15 minutes. Regenerated, disabled, and archived-tracker links return 404, and so does the old link after a restore. Owner-only actions are unreachable through a link.
- **Logging.** The log button records now, owner backfill, notes from members and owners, undo within the window, undo refused after it, owner delete, `RecordedByID` and `ViaLink` set correctly. A member who submits a time gets an entry at the current time, never the submitted one. The member's tracker page has no time field. An empty log label renders as "+ Log". A label longer than 24 characters is rejected.
- **Icons and accents.** An empty icon renders as the tally mark and an empty accent adds none. A picker value is stored. A value outside the allow-list is rejected. The name is present wherever the icon is.
- **Recorded zeros.** Marking today as none does not increment the count. An event that day deletes the mark. A day with neither is absent from the per-day series. A recorded zero is present as zero. A member or share link can undo a mark from the last 15 minutes. Clearing an older mark, or marking an earlier day, is owner-only, and a member's attempt changes nothing.
- **Time.** "Today" and per-day grouping follow the viewer's stored time zone (or the household owner's on share pages), not the browser's current zone. DST changes don't double-count or skip a day.
- **Charts.** The embedded series matches the per-day counts, omits unlogged days, and includes a recorded zero as zero. Overlay data is the other tracker's event days on that same axis. The tracker page's log form does not require Chart.js. Event-relative alignment is not part of this suite until that later idea is built.
- **Values (phase 4).** Number and duration entries, prefill from the last entry, **Log again**.
- **Deletion (phase 5).** Permanent tracker delete removes the tracker and its entries, owner only, with confirmation.
- **Ads and billing (phase 5).** No ad markup for ad-free users, users in the grace period, or on share/login/settings pages. The Paddle webhook rejects bad signatures and old timestamps, applies an event once even if delivered twice, ignores an older event arriving after a newer one, finds the user through `custom_data`, and sets and clears the plan. Tests build the `Paddle-Signature` header the way Paddle does, against a test secret, and the portal handler talks to a stub API URL.
- **Install.** The manifest is served with the right content type and names `standalone` and both icon sizes.
- **Metrics.** `GET /metrics` needs no session, is Prometheus text, and contains no account, household, or tracker data.

## Decision log

| Topic | Current decision | Notes |
| --- | --- | --- |
| Implementation | Go web app | One binary, standard library where it is enough |
| Frontend | Go templates + HTMX + Pico.css | No SPA. Forms work without JavaScript |
| Charts | Chart.js, vendored | JSON embedded by the server. Historical chart, then overlay, both phase 3. Not loaded in phase 2. Logging works without it. No frontend framework. |
| Tracker icon | Optional allow-listed string | Empty renders the tally mark. Emoji are text. A removed picker name falls back to the tally mark. |
| Recorded zero | One row per tracker per local day | Not an entry. Cleared when an event is logged that day. |
| Entry time | Server sets `OccurredAt` to now for members and share links | Only owners' submitted times are read |
| Archive | `ArchivedAt` plus clearing `ShareToken` | Restore leaves sharing off. Permanent delete is phase 5 |
| Phase 1 households | One-time idempotent backfill after `AutoMigrate` | No request-time fallback. Deleted once existing databases have run it |
| Persistence | SQLite, one shared database | Database per user rejected: migrations, backups, and sharing all get worse |
| Dependencies | GORM, pure-Go SQLite, `x/crypto`, Prometheus client | No Paddle SDK. No background queue. The client is only for exposing metrics |
| Metrics | Prometheus scrape | `GET /metrics`. Operational only: requests, latency, errors, database health. Not product analytics |
| Time zone | UTC timestamps, stored IANA zone | Detected once at signup. Not revised from the browser. Grouped in Go |
| Email | `net/smtp`, console logger in dev | Optional `.env`. Relay vendor still open |
| Bot protection | Cloudflare Turnstile | Signup, login (password and magic link), and change password. Siteverify over HTTP, no SDK. Rate limits stay |
| First public deploy | Dokku on the homelab, Fly.io as ingress only | App has no TLS and no host-specific code, so a later move to Fly.io is a redeploy |

## Open questions

- **Email relay** for magic links: Postmark, Amazon SES, Resend, or another provider with SMTP? Doesn't block building phase 2; must be answered and working before the first public deploy.
- **Backup and export**: method and destination (before the first public deploy), format, and how long backups retain deleted rows (with the phase 5 privacy policy). The privacy policy has to state the retention; that product question is also in `PLAN.md`.
