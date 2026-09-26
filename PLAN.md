# Track Anything — Project Plan

A tiny, general-purpose tracking app at **trackanything.io**, for things currently annoying enough to record on paper, in Notes, or in an ad-hoc spreadsheet. Creating a tracker and recording an observation should be almost frictionless; the accumulated history then turns into useful counts and simple charts.

The first real use: counting how many times the family dog eats each day, replacing tick marks on paper. Either person in the house can log a meal.

## North-star test

**Would we actually use Track Anything instead of the scrap of paper?**

If the answer is yes for dog meals, yes for recording a woods outing, and eventually yes for a simple workout measurement, the product is doing its job. Every feature should make tracking easier or make the accumulated history more useful.

## Product principles

- **Dogfood immediately.** The first version solves a real household problem from day one.
- **Tracking must be faster than thinking.** Common events are recordable with one or very few taps.
- **Generic underneath, concrete in the UI.** The data model supports many uses without making anyone understand a schema builder.
- **History becomes more valuable over time.** Charts, overlays, and comparisons are the payoff for consistently recording simple data.
- **Shared by default where it helps.** A tracker can be used by a whole household, or by anyone with its link, without everyone needing an account.
- **Boring technology wins.** Standard Go, server-rendered HTML, SQLite, straightforward code.
- **Micro-product economics.** Operating cost and complexity stay low enough that a $5/year product is viable.
- **Product is the point.** Use AI aggressively to deliver the product. Hand-building small tutorial projects is still valuable when learning Go is the goal, but Track Anything is about shipping.
- **Thorough tests.** Every behavior this plan describes has an automated test. `make test` fails when one of those behaviors breaks.

## Guardrails (non-goals)

- Do not become a full fitness platform.
- Do not become a medical diagnostic product or claim that correlations imply causes.
- Do not become a habit-coaching or gamification platform unless real users clearly demand it.
- Do not add microservices, distributed databases, queues, or elaborate frontend state management without a demonstrated need.
- Do not create a plugin or schema language before concrete tracker types require one.
- Do not sacrifice the one-tap tracking experience for generic configurability.
- Do not optimize architecture for hypothetical scale before the product has users.
- No native mobile apps. Home-screen install is a web app manifest, not a store app.
- No offline use. Logging still talks to the server.
- No public API.

## Reference use cases

**Dog eating and GI trouble.** Log each time the dog eats. Some days it's four meals, some days zero, and low intake sometimes comes with GI symptoms. A second tracker records woods/off-leash outings. The useful view isn't just both series on the same dates: it's being able to see whether a woods outing is followed by less eating or GI trouble one, two, or three days later. Simple inputs, then relationships that become visible in the history.

**Workout progression.** Movements, load, duration, or reps over time, to see progression in club, kettlebell, and related training (e.g. Mark Wildman's TOI program, McGill's Big 3) without turning Track Anything into a fitness app.

## Tracking model

Start with the smallest useful tracker and add value shapes only when a real use case needs them.

| Kind | Each entry records | Examples | Arrives in |
| --- | --- | --- | --- |
| Count | A timestamp | Dog ate, medication taken, symptom occurred | Phase 2 |
| Number | A timestamp and a number with the tracker's unit | Body weight, kettlebell weight, temperature | Phase 4 |
| Duration | A timestamp and a length of time | Off-leash time, workout length | Phase 4 |

- Any entry can have an optional **note**, but the note never slows down the normal one-tap path.
- For number trackers, **the last value carries forward**: if you used a 5lb club last time, the form already says 5, and **Log again** records it with one tap.
- **Fields and sub-trackers** (one workout containing several exercises, each with weight, reps, and sets) are designed below but deferred. They get built only if number and duration trackers prove painful for workouts in real use.

## Sharing

Trackers belong to a **household**, and a single tracker can also be shared by **link**.

**Households.** Every user gets a personal household at signup. The owner can invite people with an **invite link** (copied and sent by hand; the app sends no invite email). Opening it asks you to log in or sign up, then adds you as a member. You can belong to several households; the dashboard shows trackers from all of them, grouped by household.

**Share links.** The owner can turn on a share link for one tracker. Anyone with the link can use that tracker **without logging in**. That covers a kitchen tablet, a dog sitter, or a relative who will never make an account.

**What each person can do:**

| Action | Owner | Household member | Anyone with the share link |
| --- | --- | --- | --- |
| See the tracker and its recent entries | Yes | Yes | Yes (that one tracker only) |
| Log an entry | Yes | Yes | Yes |
| Undo a recent entry | Yes | Yes | Yes |
| Edit an entry's time or note, delete older entries | Yes | No | No |
| Create, rename, archive trackers | Yes | No | No |
| Turn on, regenerate, or turn off a share link | Yes | No | No |
| Invite or remove household members, regenerate the invite link | Yes | No | No |

- **Recent** means an entry created in the last 15 minutes (by `CreatedAt`, not `OccurredAt`). That covers "oops, I tapped twice" without letting a link holder wipe history. The window is a constant, easy to change.
- A household can have more than one owner, so ownership can be shared or handed over.
- **Regenerating** a share link or invite link makes the old one stop working immediately. Turning a share link off does the same.
- Members who want their own trackers make them in their personal household, where they are the owner.
- Entries record who logged them (`RecordedByID`, or "via link"). The UI can show it where useful without cluttering every view.

## Tech stack

| Concern | Choice | Notes |
| --- | --- | --- |
| Language | Go (latest stable, 1.22+) | Needed for method + wildcard routing in `net/http` |
| HTTP routing | `net/http` `ServeMux` | Patterns like `GET /trackers/{id}` |
| Templates | `html/template` | Auto-escaping, layouts via `{{block}}` / `{{template}}` |
| Static files and templates | `embed` | Ship a single binary |
| Logging | `log/slog` | Structured logs to stdout |
| ORM | GORM (`gorm.io/gorm`) | `AutoMigrate` for schema |
| Database | SQLite, one shared app database | A database per user was considered and rejected: it complicates migrations, backups, and especially sharing |
| SQLite driver | `github.com/glebarez/sqlite` | Pure-Go GORM driver, so no CGO and easy cross-compiling |
| Password hashing | `golang.org/x/crypto/bcrypt` | |
| Email (magic links) | `net/smtp` to a transactional email relay | In development, links are logged to the console instead of sent |
| Frontend | HTMX (vendored `htmx.min.js`) | Served from `/static`, no CDN |
| Charts | Chart.js (vendored `chart.umd.min.js`) | Only on pages that show charts; never on the fast-entry path |
| CSS | Pico.css (vendored `pico.min.css`) | Classless-first, minimal custom CSS |
| Icons | [Tabler Icons](https://tabler.io/icons), individual SVGs vendored and embedded | MIT licensed. Only the icons we use; no icon font, no JS. Inlined so they take Pico's colors |
| Install | Web app manifest + icons | No build step |
| Payments | Paddle Billing: Paddle.js checkout + webhooks, API called with `net/http` | Paddle is the merchant of record, so it handles sales tax and VAT. No Paddle SDK |
| Ads | Google AdSense with a Google-certified consent platform | Later, after the app is live |

Target: **3 direct Go dependencies** (GORM, the SQLite driver, `x/crypto`). Everything else is the standard library, vendored static files, or HTTP calls.

## Data model

### Accounts

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
```

### Households and trackers

```go
type Household struct {
    ID          uint
    Name        string  `gorm:"not null"` // "Sheldon's trackers", "Leibole house"
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
    Kind        string  `gorm:"not null;default:count"` // "count", later "number" and "duration"
    Unit        string  // number trackers only: "kg", "lb"
    ShareToken  *string `gorm:"uniqueIndex"` // nil = no share link
    Position    int
    ArchivedAt  *time.Time // archived trackers are hidden and their share link stops working
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type Entry struct {
    ID           uint
    TrackerID    uint      `gorm:"index;not null"`
    OccurredAt   time.Time `gorm:"index;not null"` // when it happened (UTC), editable by owners
    RecordedByID *uint     // nil when logged through a share link
    ViaLink      bool
    Note         string
    Number       *float64 // number trackers (phase 4)
    DurationSec  *int     // duration trackers (phase 4)
    CreatedAt    time.Time // drives the 15-minute undo window
    UpdatedAt    time.Time
}
```

Tokens (`InviteToken`, `ShareToken`, session IDs, login tokens) are 32 random bytes from `crypto/rand`, base64url-encoded.

### Access checks

Every tracker lookup goes through one of two small functions, so access rules live in one place:

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
```

Handlers then check the role for owner-only actions. A tracker the user can't see is a 404, never a 403, so IDs don't leak.

### Counting "the dog ate 3 times today"

Compute today's boundaries in the viewer's time zone in Go, then count in SQL:

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

**Counts per day** (e.g. the last 30 days) load `occurred_at` for the range and group by local date in Go. SQLite can't convert to IANA time zones itself, and grouping in Go stays correct across daylight-saving changes. The same per-day series feeds the charts.

**Share-link visitors have no time zone**, so "today" on a share page uses the time zone of the household's first owner. Everyone in a house sees the same "today".

### Carrying forward last values (phase 4)

For number and duration trackers, the form is prefilled from the tracker's most recent entry by `OccurredAt`:

```go
var last Entry
err := db.Where("tracker_id = ?", tracker.ID).Order("occurred_at DESC").First(&last).Error
// gorm.ErrRecordNotFound means there's nothing to carry forward; show an empty form.
```

- Backfilling an older entry doesn't change what carries forward.
- Only the value carries forward. The note does not, and the time defaults to now.
- **Log again** creates a new entry with the same value and `OccurredAt` = now.
- To change something (moving up to 6lb), edit the prefilled value and submit. That entry then carries forward.

### Decisions worth calling out

- **`OccurredAt` vs `CreatedAt`**: owners can backfill ("I forgot to log yesterday"), so when it happened is separate from when the row was written. The undo window uses `CreatedAt`.
- **Time zones**: store UTC, convert using `User.TimeZone` (or the household owner's, on share pages). Detect the browser's zone at signup with `Intl.DateTimeFormat().resolvedOptions().timeZone` in a hidden field; changeable in settings.
- **Archive, not delete**, for trackers. Archiving is reversible. Permanently deleting a tracker and its entries is a separate, confirmed owner action.
- **Value columns on `Entry`** (`Number`, `DurationSec`) instead of a generic field system. Two nullable columns cover the planned tracker kinds with no joins.
- **Email is stored lowercased** so `Sheldon@…` and `sheldon@…` can't become two accounts.

## Accounts and login

Two ways in, same account:

- **Email and password.** Password is optional; set it at signup or later in settings.
- **Magic link.** Enter your email, get a one-time link valid for 15 minutes. Clicking it logs you in. The same mechanism handles "forgot password".

Signing up needs only an email. Magic links double as email verification.

Magic-link emails go through `net/smtp` to a transactional email relay (which one is an open question). In development, the mailer logs the link to the console, so no email setup is needed locally.

Sessions last 30 days and renew on use.

## Charts, overlays, and delayed comparisons

The payoff for consistent tracking. Charts are always secondary to recording: the fast-entry path never loads Chart.js.

- **Per tracker**: counts per day, week, or month for count trackers; values over time for number trackers; duration over time for duration trackers.
- **Overlay**: pick two or more trackers you can see and draw them on a shared time axis.
- **Delayed comparison**: shift one tracker by 1, 2, or 3 days, so "woods outing on day 0" lines up with "meals on day 1, 2, 3".
- The server computes the per-day series in Go (reusing the time-zone-aware grouping) and embeds it in the page as JSON in a `<script type="application/json">` tag. A small script hands it to Chart.js.
- The page describes what you're seeing ("meals on the 2 days after each woods outing") and never claims a cause.

## Project layout

Start flat: one `main` package, split only when navigation or coupling becomes painful. If it grows, move toward `cmd/web` (startup), `internal/app` (server, routes, shared helpers), `internal/tracker`, `internal/store`, and `ui/templates` + `ui/static`, organized by feature rather than handler/service/repository layers.

```
track-anything/
├── PLAN.md
├── go.mod
├── main.go             # config, open DB, migrate, build mux, start server
├── db.go               # GORM setup, AutoMigrate, SQLite pragmas
├── models.go           # structs above + small helpers (day boundaries, tokens)
├── access.go           # trackerForUser, trackerForShareToken, role checks
├── auth.go             # signup, login, logout, magic links, session middleware
├── mail.go             # net/smtp sender, console sender for development
├── handlers_trackers.go
├── handlers_entries.go
├── handlers_households.go # members, invite link, join
├── handlers_share.go   # share-link pages, no login
├── handlers_charts.go
├── handlers_billing.go # upgrade page, Paddle webhook, customer portal link (phase 5)
├── render.go           # template loading + render helper (full page vs HTMX partial)
├── middleware.go       # logging, recover, auth, CSRF check
├── templates/
│   ├── layout.html     # ad and consent scripts live here, outside any HTMX swap target
│   ├── dashboard.html
│   ├── tracker_show.html
│   ├── share.html      # what a share-link visitor sees
│   ├── household.html
│   ├── charts.html
│   └── partials/       # fragments returned to HTMX requests
├── icons/              # Tabler SVGs we use (plus.svg, trash.svg, ...), inlined by the icon template func
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
├── Dockerfile          # multi-stage build, used by Dokku now and Fly.io later
└── Makefile            # run, build, test
```

## Routes

Everything except auth, share links, the Paddle webhook, and static files requires a session.

```
GET    /                          logged out: what the app is + log in / sign up
                                  logged in: dashboard of trackers grouped by household, today's count, +1
GET    /signup, POST /signup
GET    /login,  POST /login       password login
POST   /login/link                email a magic link
GET    /login/link/{token}        use a magic link
POST   /logout
GET    /settings, POST /settings  time zone, password, (phase 5) plan, delete account

GET    /households/{hid}                      members, invite link, trackers
POST   /households/{hid}/invite               turn on or regenerate the invite link (owner)
POST   /households/{hid}/invite/delete        turn invites off (owner)
POST   /households/{hid}/members/{uid}/delete remove a member (owner)
POST   /households/{hid}/members/{uid}/owner  make a member an owner (owner)
GET    /join/{token}                          accept an invite (log in or sign up first)

GET    /trackers/new              new tracker form (pick household)
POST   /trackers                  create tracker
GET    /trackers/{id}             tracker page: +1, recent entries, per-day counts, streak
POST   /trackers/{id}             rename (owner)
POST   /trackers/{id}/archive     archive or unarchive (owner)
POST   /trackers/{id}/delete      permanently delete, with confirmation (owner)
POST   /trackers/{id}/share       turn on or regenerate share link (owner)
POST   /trackers/{id}/share/delete turn share link off (owner)

POST   /trackers/{id}/quick       log "now" (the +1 button)
POST   /trackers/{id}/entries     log with a time, note, or value
POST   /trackers/{id}/repeat      log "now" with the last value (phase 4)
POST   /entries/{eid}/undo        delete an entry created in the last 15 minutes (any member)
POST   /entries/{eid}             edit time or note (owner)
POST   /entries/{eid}/delete      delete any entry (owner)

GET    /s/{token}                 share page: tracker name, today's count, +1, today's entries
POST   /s/{token}/quick           log "now" via the link
POST   /s/{token}/entries/{eid}/undo  undo a recent entry via the link

GET    /charts                    overlay and delayed-comparison view (phase 3)

GET    /billing                   upgrade page: loads Paddle.js and opens checkout (phase 5)
POST   /billing/portal            redirect to Paddle's customer portal to manage or cancel (phase 5)
POST   /billing/webhook           Paddle webhook, verified by signature (phase 5)

GET    /static/...                embedded files
```

Forms use `POST` so everything works without JavaScript. HTMX attributes enhance the same forms. The render helper checks the `HX-Request` header and returns either the full page or a partial.

## HTMX interactions

- **Quick log (+1)**: `hx-post` swaps in the updated "3 today" count and a short-lived **Undo** button. The count bumps immediately on tap (a tiny `hx-on` handler) so it feels instant, and the server response confirms it.
- **Undo**: removes the entry and updates the count. It disappears once the 15-minute window passes.
- **Log with details**: the entry form prepends the new row and re-renders prefilled with what was just logged.
- **Owner inline edits**: edit an entry's time or note in place; delete with `hx-confirm`.

## Icons

[Tabler Icons](https://tabler.io/icons) (MIT). Copy in only the SVGs a screen actually uses, into `icons/`, and embed them. No icon font, no JavaScript, no external request.

Icons are **inlined** into the HTML rather than referenced with `<img>`, so their `stroke="currentColor"` follows Pico's text color, including dark mode and button hover states. A template function does it:

```go
//go:embed icons/*.svg
var iconFS embed.FS

func icon(name string) (template.HTML, error) {
    b, err := iconFS.ReadFile("icons/" + name + ".svg")
    return template.HTML(b), err
}
```

```html
<button aria-label="Log a meal">{{icon "plus"}}</button>
```

- Icons only where they help comprehension; a text label is fine too.
- When copying an SVG in, add `aria-hidden="true"` to it. Icon-only buttons carry an `aria-label`.
- A missing icon name is a template error, so a test that renders each page catches typos.
- Starting set (about a dozen): `tallymarks` (the brand mark, also the app icon), `plus`, `arrow-back-up` (undo), `trash`, `pencil`, `chart-line`, `settings`, `share`, `users`, `archive`, `calendar`, `link`, `logout`.

## Summaries

All plain queries, rendered as tables or charts:

- **Count today**, **count per day** over the last 30 days, and **current streak** (phase 2).
- **Charts, overlays, delayed comparisons** (phase 3).
- **Values and durations over time** (phase 4).

## Security basics

- Passwords hashed with bcrypt. Magic-link and session tokens from `crypto/rand`; login tokens stored only as a sha256 hash, single use, 15-minute expiry.
- Session cookie: `HttpOnly`, `Secure`, `SameSite=Lax`, stored in the `sessions` table with an expiry. `ENV=dev` turns off `Secure` so every browser works on `http://localhost`.
- CSRF: `http.CrossOriginProtection` on all non-GET requests, including share-link posts.
- All tracker and entry access goes through `trackerForUser` or `trackerForShareToken`. A tracker you can't see is a 404, and the test checks no row changed.
- **Share links are bearer tokens.** Anyone holding the URL can log entries. Share pages send `Referrer-Policy: no-referrer` and `X-Robots-Tag: noindex` so the token doesn't leak to other sites or search engines, and link visitors can only undo entries from the last 15 minutes.
- `html/template` handles output escaping.
- Per-IP rate limits (a simple in-memory limiter) on password login, magic-link requests, and share-link posts. See Deployment for getting the real client IP behind the proxies.
- The Paddle webhook verifies the `Paddle-Signature` header (`ts=...;h1=...`, an HMAC-SHA256 of `ts:rawbody` with the notification secret, checked with `crypto/hmac`) and rejects old timestamps (replay protection). It's safe to receive the same event twice.

## Installable web app

Folded into phase 0 because it's just static files:

- `manifest.webmanifest` with `name`, `start_url: "/"`, `display: "standalone"`, and 192 and 512 icons.
- Apple touch icon and `apple-mobile-web-app-capable` meta tag, since iOS uses Share → Add to Home Screen.
- No service worker. Recent Chrome versions no longer require one for installability, and skipping it avoids stale cached CSS and JS after a deploy. Confirm on a real phone at the first deploy; add a minimal worker only if the install option is missing.

The install option only appears over HTTPS, so it's verified on trackanything.io after the phase 2 deploy.

## Business model

- **Free**: the whole app. Ad-supported once ads launch.
- **Ad-free**: **$5/year** (may change). The price lives in Paddle as a Price, and the app only knows its ID (`PADDLE_PRICE_ID`), so changing the price is a Paddle dashboard change plus a config change, not a code change. Removes ads; whether it includes anything else is undecided.

**Fees.** Paddle charges about 5% + 50¢ per transaction, so a $5 sale nets about $4.25 (roughly 15% in fees). That's higher than Stripe's ~9% at this price. The difference pays for Paddle being the merchant of record: it calculates, collects, and remits sales tax and VAT worldwide, and handles invoices, refunds, and chargebacks. For a tiny product sold to EU and US customers, that's worth it. The superseded $1/month idea would have lost over half to fees.

### Ads (phase 5, after the app is live)

- **Google AdSense.** AdSense reviews the site before approving it and expects substantive original content, clear navigation, and privacy and policy pages. The logged-out homepage and a privacy page need to be real by then.
- **Consent**: AdSense requires a Google-certified consent management platform for visitors in the EEA, UK, and Switzerland. Use one (Google's own "Privacy & messaging" is the default candidate) instead of a home-made banner. The consent platform stores the visitor's choice; the app doesn't.
- The session cookie is strictly necessary and doesn't wait on consent. Ad cookies and scripts do.
- **Ad and consent scripts live in the page layout**, outside any element HTMX swaps, so they load once per page and aren't re-run by partial updates.
- Ad-free users, and pages that shouldn't carry ads (share pages, login, settings), get no ad markup at all.

### Subscription (phase 5)

Paddle Billing with one yearly price.

- **Checkout**: the `/billing` page loads Paddle.js (from Paddle's CDN, only on that page) with the client-side token, and opens the overlay checkout for `PADDLE_PRICE_ID`. It passes the user's email and `customData: {"user_id": ...}` so the webhook knows which account paid. Paddle hosts the card form and tax calculation.
- **Webhook** (`POST /billing/webhook`): `subscription.created`, `subscription.updated`, and `subscription.canceled` set `Plan`, `PaddleCustomerID`, `PaddleSubscriptionID`, and `SubscriptionEndsAt` (from the subscription's current billing period end). The user is found through `custom_data.user_id`, falling back to `PaddleSubscriptionID`. Events can arrive out of order, so each update compares the event's `occurred_at` with the last one applied.
- **Managing the subscription**: `POST /billing/portal` asks Paddle's API for a customer portal session and redirects there. Cancelling, updating the card, and downloading invoices all happen in Paddle's portal, so the app builds none of that.
- **Grace period**: 2 days past `SubscriptionEndsAt` before treating the user as free, so a late renewal webhook doesn't flash ads at a paying user.
- **Sandbox first**: Paddle has a separate sandbox environment with its own keys and test cards. `PADDLE_ENV=sandbox` points the app at `sandbox-api.paddle.com` and tells Paddle.js to use sandbox. Paddle has no local forwarding CLI like Stripe's, so local webhook testing uses a tunnel (e.g. `cloudflared tunnel --url http://localhost:8080`) pointed at by a sandbox notification destination, or Paddle's webhook simulator against the deployed app. Automated tests don't need either; they sign payloads themselves.
- **Account approval**: Paddle reviews the business and website before enabling live payments. It expects a live site with pricing, terms of service, a privacy policy, and a refund policy. Apply early in phase 5, since review takes time.

## SQLite configuration

Set on connect:

- `PRAGMA journal_mode=WAL;` (concurrent reads while writing)
- `PRAGMA foreign_keys=ON;`
- `PRAGMA busy_timeout=5000;`
- `PRAGMA synchronous=NORMAL;`

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

What this means for the app:

- **No TLS in the Go server.** It listens on plain HTTP (`ADDR`, or Dokku's `$PORT`).
- **Build**: a multi-stage `Dockerfile` (build with `CGO_ENABLED=0`, then a minimal image with the binary). Templates and static files are embedded; embed time zone data too with `import _ "time/tzdata"`.
- **Deploy**: `git push dokku main`.
- **SQLite storage**: `dokku storage:mount` a host directory, e.g. `/var/lib/dokku/data/storage/trackanything:/data`, with `DB_PATH=/data/trackanything.db`. Exactly one instance.
- **Host header must survive the chain**, for Dokku's routing and the CSRF origin check.
- **Real client IP** (rate limiters, logs): `CF-Connecting-IP`, falling back to `X-Forwarded-For`. Only trusted because the app is unreachable except through the proxy chain.
- **`Secure` cookies still work**, because the browser sees HTTPS.
- **Backups**: [Litestream](https://litestream.io) to S3-compatible storage, or a nightly `sqlite3 .backup` cron job on box to start. Live from the first deploy.
- **Config** via `dokku config:set`: `ADDR`/`PORT`, `DB_PATH`, `BASE_URL`, `ENV`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `MAIL_FROM`. Phase 5 adds `PADDLE_ENV` (`sandbox` or `production`), `PADDLE_API_KEY`, `PADDLE_CLIENT_TOKEN`, `PADDLE_WEBHOOK_SECRET`, `PADDLE_PRICE_ID`.

### Later: hosting directly on Fly.io

If real usage justifies it, move the app itself to Fly.io. The same Docker image runs there, with SQLite on a Fly volume (still one instance) and Litestream for backups. Because the app has no TLS or host-specific setup, the move is a redeploy plus a data copy.

## Testing

Tests are part of the product. If this plan describes a behavior, `make test` covers it.

Standard library only: `testing` and `net/http/httptest`. Each test gets its own SQLite database (`:memory:` or a temp file) with `AutoMigrate`. Tests use a cookie jar so a flow can log in and keep going. The mailer is swapped for an in-memory one that records sent links.

Three layers:

1. **Pure logic**, table-driven, no HTTP: day boundaries in an IANA time zone including DST spring-forward and fall-back; streaks; per-day series and day-shifted series for delayed comparisons; the undo window; which entry carries forward (a backfilled older entry must not win).
2. **Handlers**: every route has a happy path and the failures that matter: bad input, CSRF rejection, logged-out redirect, expired session, a non-member, and a member attempting an owner-only action.
3. **Flows** through the cookie jar, matching each phase's "done when".

What the suite has to pin down:

- **Accounts.** Signup, password login, wrong password, magic link (works once, fails when expired or reused, stored only as a hash), logout, session expiry, rate limits, email case-insensitivity.
- **Households.** A new user gets a personal household as owner. Invite link joins as member; a regenerated or disabled link fails. Owners can remove members and promote owners; members can't.
- **Permissions.** For every owner-only route, a member gets refused and nothing changes. Non-members get 404 for trackers, entries, and households.
- **Share links.** Logging and undo work without a session. The share page shows only that tracker. Undo fails after 15 minutes. Regenerated, disabled, and archived-tracker links return 404. Owner-only actions are unreachable through a link.
- **Logging.** +1, backfill, notes, undo within the window, undo refused after it, owner delete, `RecordedByID` and `ViaLink` set correctly.
- **Time.** "Today" follows the viewer's time zone (or the household owner's on share pages), and DST changes don't double-count or skip a day.
- **Charts.** The embedded series matches the per-day counts, and a 2-day shift lines up the right days.
- **Values (phase 4).** Number and duration entries, prefill from the last entry, **Log again**.
- **Ads and billing (phase 5).** No ad markup for ad-free users, users in the grace period, or on share/login/settings pages. The Paddle webhook rejects bad signatures and old timestamps, applies an event once even if delivered twice, ignores an older event arriving after a newer one, finds the user through `custom_data`, and sets and clears the plan. Tests build the `Paddle-Signature` header the way Paddle does, against a test secret, and the portal handler talks to a stub API URL.
- **Install.** The manifest is served with the right content type and names `standalone` and both icon sizes.

## Phases

Each phase ends with the app running and usable locally. Phase 2 ends with the first public deploy.

### Phase 0: Project skeleton

- `go mod init`, `main.go` with `ServeMux`, config from environment variables.
- GORM + SQLite with the pragmas above; `AutoMigrate` wired up.
- `html/template` layout with Pico.css and HTMX vendored and embedded; render helper for full page vs HTMX partial.
- Web app manifest, icons, and Apple meta tags linked from the layout.
- Middleware: request logging with `slog`, panic recovery.
- `GET /healthz` and a placeholder home page.
- `Dockerfile` and `Makefile` (`run`, `build`, `test`).

**Done when:** `make run` serves the placeholder page at http://localhost:8080 and `make test` passes.

### Phase 1: User accounts

- `User`, `Session`, and `LoginToken` models.
- Signup with email (password optional), password login, magic-link login, logout. Mailer with a console sender for development and `net/smtp` for production.
- Session cookie and auth middleware that loads the current user.
- Time zone captured at signup; settings page for time zone and password.
- CSRF protection and per-IP rate limits on login and magic-link requests.
- Tests for this phase, per the Testing section.

**Done when:** you can sign up, log out, log back in with a password and with a magic link (printed to the console), and a second account works independently.

### Phase 2: The dog tracker, shared

- `Household`, `HouseholdMember`, `Tracker` (count kind), and `Entry` models. A personal household is created at signup.
- Create, rename, archive, and permanently delete trackers (owners).
- Dashboard grouped by household: "N today" and a **+1** button with optimistic count and **Undo**.
- Tracker page: recent entries, count per day for the last 30 days, current streak. Owners can backfill, edit, and delete entries; members can undo recent ones.
- Household page: invite link (create, regenerate, disable), join flow, remove members, promote to owner.
- Share links: turn on, regenerate, turn off; share page with today's count, +1, and undo; no login.
- Tests for this phase, per the Testing section.
- **First public deploy**: Dokku app on box, storage mounted, config set, Cloudflare and Fly.io ingress routed, `Host` and client IP headers confirmed, backups running, home-screen install confirmed on a phone.

**Done when:** both of you log the dog's meals from your own phones (or one of you through the share link) for a week instead of using paper, and the dashboard says "Dog ate: 3 today" correctly in your time zone.

### Phase 3: Charts, overlays, delayed comparisons

- Vendored Chart.js, loaded only on chart pages.
- Per-tracker chart of counts per day, week, or month.
- Overlay any trackers you can see on a shared time axis.
- Shift one tracker by 1–3 days for "what happened in the days after X?".
- Neutral wording: patterns, not causes.
- Tests for this phase, per the Testing section.

**Done when:** you can put "Woods outing" and "Dog ate" on one chart, shift woods by 1–3 days, and see whether low-eating days follow outings.

### Phase 4: Number and duration trackers

- `Kind` = `number` (with a unit) or `duration`; `Number` and `DurationSec` on entries.
- Entry forms for each kind; share links support them too.
- Prefill from the last entry and **Log again**.
- Charts for values and durations over time.
- CSV export per tracker.
- Tests for this phase, per the Testing section.

**Done when:** woods outings record how long they were, a movement like "Club mill" tracks weight over time, and re-logging last time's weight is one tap. Then decide, from real use, whether workouts need fields and sub-trackers.

### Phase 5: Ads and the ad-free subscription

After the app is live and has real content for AdSense review.

- Privacy policy page and a real logged-out homepage.
- Account deletion in settings: removes the user's data for real, not a soft delete. A sole owner of a household with other members must promote another owner or delete the household first. The privacy policy notes how long backups keep deleted data.
- AdSense with a Google-certified consent platform; ad scripts in the layout, never on share, login, or settings pages.
- Terms of service and refund policy pages (Paddle requires them), and apply for Paddle approval early.
- Paddle checkout on `/billing` (sandbox first), webhook sets the plan, customer portal link in settings, 2-day grace period; ad-free users see no ads.
- Tests for this phase, per the Testing section.

**Done when:** AdSense approves the site, a free user sees ads after consenting in the consent platform, and a user who pays $5 sees none for a year.

## Deferred: fields and sub-trackers

Built only if number and duration trackers prove painful for workouts, for example because one exercise needs weight **and** reps **and** sets in a single entry, or because a workout should group several exercises.

- **Fields**: a tracker gets typed fields (number, text, choice, checkbox); each entry stores one value per field (a `Field` table and a `Value` table). Carry-forward and **Log again** extend to all fields.
- **Sub-trackers and sessions**: `ParentID` on `Tracker` and `Entry`. **Start workout** creates a parent entry (the session); each exercise logged in it is a child entry. McGill Big 3 becomes a parent with three name-only sub-trackers, and "done today" means each has an entry.
- **Repeat last session**: carry-forward for a whole workout.

Example of what that would record:

```
Trackers                          Entries (Saturday)
─────────                         ──────────────────
TOI                               #10 TOI            08:00
├── Swing   (implement, weight,   ├── #11 Swing      08:05  KB, 24kg, 10 reps, 3 sets
│            reps, sets)          ├── #12 Mill       08:15  Club, 10lb, 8 reps, 3 sets
├── Mill    (same fields)         └── #13 Halo       08:25  Mace, 10lb, 10 reps, 2 sets
└── Halo    (same fields)
```

## Later ideas

- **Start without an account**: the logged-out homepage asks "What would you like to track?", and submitting it creates a temporary user and the tracker; saving the account later adds an email. Needs long-lived sessions, cleanup of abandoned temporary users, a rate limit, and a visible **Log in** link so returning users don't make duplicates. Share links already cover much of the "use it without signing up" need.
- Tracker templates ("Start from: McGill Big 3", "Start from: TOI").
- A chosen icon per tracker (a paw for the dog, a pill for medication), picked from a curated handful of Tabler icons.
- Reminders (email) for daily trackers.
- CSV import.
- Extra things the ad-free plan could include, if anything.

## Decision log

| Topic | Current decision | Notes |
| --- | --- | --- |
| Price | $5/year, may change | $1/month superseded. The price lives in Paddle; the app stores only `PADDLE_PRICE_ID` |
| Payments | Paddle Billing | Merchant of record handles sales tax and VAT; ~15% fees at $5 accepted for that |
| Implementation | Go web app | A good first real Go product; AI accelerates delivery |
| Frontend | Go templates + HTMX + Pico.css | No SPA framework |
| Charts | Chart.js, vendored | Focused client-side JS for visualization only |
| Persistence | SQLite, one shared app database | Database per user rejected: extra complexity, worst for sharing |
| Sharing | Households plus per-tracker share links | Share links work without logging in; non-owners can log and undo recent entries |
| Invites | Copyable links | The app doesn't send invite email |
| Login | Email + password, and magic links | Password optional |
| First use case | Dog meals | Replaces paper tick marks |
| Second use case | Woods outings and workout progression | Validates number and duration tracking |
| Tracker kinds | Count first, then number and duration | Fields and sub-trackers deferred until real use demands them |
| Analysis | Overlays + delayed comparisons | Woods outings vs eating on following days is the test case |
| Ads | AdSense with a certified consent platform, after launch | No home-made consent banner |
| Start without account | Later idea | Share links cover most of the need for now |
| Install | Manifest + icons in phase 0, no service worker | Verified on a phone at first deploy |
| UI icons | Tabler Icons, vendored SVGs inlined via a template func | Font Awesome passed over: icon font or JS loader, CC BY license. Tabler's large set also suits per-tracker icons later |
| First public deploy | End of phase 2 | Backups live from the start |

## Open questions

- **Share page contents**: today's count and today's entries only (current plan), or the recent history and streak too?
- **Undo window**: 15 minutes is the starting value. Right length?
- **Workouts**: is one number per tracker (e.g. weight for "Club mill") enough, or do weight and reps need to be recorded together? That's the trigger for fields. Relatedly, for TOI: one entry per exercise with a sets count, or one entry per set?
- **Units**: fixed per tracker (current plan), or chosen per entry?
- **Email relay** for magic links: Postmark, Amazon SES, Resend, or another provider with SMTP?
- **Free tier**: ad-supported, limited by tracker count or history, or simply generous so $5/year is mostly a convenience purchase?
- **Does ad-free include anything else?**
- **Consent platform**: Google's "Privacy & messaging", or a third-party certified one?
- **Lag analysis**: how far beyond visual overlays should it go?
- **Backup and export**: format and retention policy.