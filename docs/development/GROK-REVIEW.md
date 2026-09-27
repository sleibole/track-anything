# Grok review

Reviewed `f3edec0` (`Add email verification that does not block signup, and show a normal page when a login link cannot be sent`) on 27 Sep 2026. `go test ./...` passed.

This is a read of the current program: handlers, auth, mail, templates, `static/app.js`, the SQLite setup, and the development docs. Phase 2 (households, count trackers, share links, recorded zeros) and the summary-display follow-on are implemented and covered by tests. Charts, number and duration trackers, billing, Turnstile, and Prometheus are specified and not built.

## What is in good shape

The design constraints are visible in the code. One `main` package, server-rendered `html/template`, HTMX as a progressive layer, SQLite pragmas on the DSN so every pooled connection gets them, tokens from `crypto/rand`, magic-link and verification tokens stored as SHA-256, and a single conditional `UPDATE` so two clicks cannot both consume a link. `GET` does not log anyone in, which is the right defense against a scanner that only opens the URL.

Access checks go through `trackerForUser`, `trackerForShareToken`, `archivedTrackerForOwner`, and `householdForUser`. A tracker the viewer cannot see is a 404. Owner-only routes check the role after that. Members cannot backfill: `handleLogEntry` reads the time field only for an owner. Archiving writes `archived_at` and clears `share_token` together, so restore cannot revive an old link. Cookies are `HttpOnly` and `SameSite=Lax`, and `Secure` is on unless `ENV=dev`. `http.CrossOriginProtection` sits on the whole mux. Share pages send `Referrer-Policy: no-referrer` and `X-Robots-Tag: noindex`.

The test suite matches that care. Day boundaries across DST, summary wording, household permissions, share-link limits, session renewal, rate limits, and SMTP retry behavior are all pinned down. The failure paths that matter (non-member, member attempting an owner action, expired or reused link) are real HTTP tests with a cookie jar, not stubs of the happy path.

## Findings

### 1. Pressing Enter on the password login form sends a magic link

`templates/login.html` is one form whose `action` is `POST /login/link`. The visible password button is a second submit control with `formaction="/login"`.

The first submit button in the form is Continue. CSS hides it while the password toggle is checked (`static/app.css`, `.auth-link-submit`), and it does not disable it. Implicit submit — Enter, or Go on a phone keyboard — activates that first button. The browser posts to `/login/link`. `handleLoginLinkRequest` reads the email and ignores the password, then shows “Check your email.”

Clicking the Log in button does use `/login`. The keyboard path, which is the normal one on a phone, does not.

Signup does not have this problem: it has a single submit button. The password field is disabled by `static/app.js` when the toggle is off, so an email-only submit does not include a leftover password. That script never disables the unused submit button on the login form.

Fix: while the password toggle is checked, disable Continue (or move the password submit button ahead of it and point the form at `/login`). A test that posts the login form the way a browser does on Enter, with both buttons present and the password toggle on, would lock this in.

### 2. A password set at signup survives until someone changes it, and the verification mail says to ignore it

Signup creates the user, the household, and a session, then sends verification as a best effort (`handleSignup`). `TestSignupSucceedsWhenVerificationEmailFails` and `TestNewUserStartsUnverified` lock in that the account is usable immediately. That matches the latest commit. It also means an address can be claimed by whoever types it first.

The concrete hole:

1. Someone signs up as `you@example.com` and sets a password. They are logged in. No mailbox check has happened.
2. The real mailbox receives “Confirm your email” from `sendVerificationEmail`. The body says: “If you didn't sign up, you can ignore this email.”
3. Ignoring it leaves the password session in place. Signup also tells the real person “An account with that email already exists,” so the magic-link form is the way in.
4. A later magic link logs the real person into that same account and marks the email verified (`handleLoginLinkUse`). The original password still works. Nothing in settings lists other sessions. The other session ends only when someone changes the password (`handleSettingsPassword`).

Magic-link login is safe on its own, because the link is delivered to the mailbox. The password set before anyone proved they own the address is the part that outlives that. The sentence in the verification mail is the wrong instruction for that case: the person who did not sign up is the one who needs to open a magic link and set a new password.

The smallest change that keeps immediate signup is to stop accepting a password until the address is verified, and to rewrite the mail so an unexpected signup says how to take the account back. Creating the session only after verification is the stronger version, and it drops the “log in before the mailbox is proven” window entirely. Either way, “ignore this email” should go.

Signup revealing that an address is already registered is a separate, smaller leak. The magic-link form is careful not to do that (`handleLoginLinkRequest`). The two forms disagree.

### 3. Request logs store bearer tokens

`logRequests` in `middleware.go` logs `r.URL.Path` on every request. These paths contain secrets:

| Path | What the log line is |
| --- | --- |
| `/login/link/{token}` | A one-time login credential, 15 minutes |
| `/verify/{token}` | A one-time confirmation link, 24 hours |
| `/s/{token}` | A share link. Anyone with it can log and can undo entries from the last 15 minutes. It stays valid until regenerated or the tracker is archived |
| `/join/{token}` | A household invite. Anyone with it can join as a member |

Production logs are JSON on stdout (`newLogger`). A log aggregator then holds long-lived share and invite URLs. Login tokens are shorter-lived and still enough to create a session if the log is read before they expire.

`consoleMailer` also logs the full message body, link included, when `SMTP_HOST` is empty. Startup warns about that in production. The access log does it even when mail is configured correctly.

Log the route pattern, or strip the token segment, for those four paths. Keep the mail body out of Info logs once a relay is configured.

### 4. The rate-limit key is whichever address the client claims

`clientIP` prefers `CF-Connecting-IP`, then the first `X-Forwarded-For` hop, then `RemoteAddr`. The comment says this is safe because the app is only reachable through Cloudflare and the Fly ingress. Nothing in the process checks that. A request that reaches Dokku, the Fly address directly, or a LAN port can send its own `CF-Connecting-IP` and pick a fresh limiter bucket.

The limiter itself grows without a hard cap. `allow` deletes expired windows only after the map passes 10,000 keys, and a key still inside its window stays. A client that can vary the header can add a new entry per request for the whole window (a minute for login, an hour for signup). That is a memory leak aimed at the process, and it also skips the login, signup, magic-link, and share limits.

Before a public deploy, terminate those headers at the proxy that actually saw the client, and ignore them inside the app unless the peer is that proxy. Cap the map.

Login limiting is per source address only. There is no per-account throttle. Ten attempts a minute per address is the whole brake on password guessing, and `ARCHITECTURE.md` already says Turnstile should sit in front of signup, login, and password change. It is not in the code or in `go.mod`. See Documentation drift.

### 5. The server can run out of write time while mail is still sending

`http.Server.WriteTimeout` is 15 seconds and covers handler time. `smtpMailer` may dial twice, each attempt bounded by 5 seconds, with a 500ms pause between them, and signup also runs bcrypt. A slow relay can spend most of the write budget inside `handleSignup` or `handleLoginLinkRequest`.

Signup creates the user before it sends mail. If the connection is cut at the timeout, the account exists and the browser saw a failure. A retry then hits “An account with that email already exists.” Login-link sending already turns a mail error into a normal page (`TestLoginLinkSendFailureIsAFriendlyPage`); a timeout that never returns a response does not.

This is acceptable for local MailHog. For the production relay, send with a budget that fits under the write timeout, or accept the signup and surface a “we could not send mail” page the way a failed login link already does. The second is already how a returned SMTP error is handled. The timeout is the case that skips that page.

### 6. Recorded-zero and log can both land on the same day

`recordZero` counts entries, then `FirstOrCreate`s the zero, outside a transaction. `logEntry` inserts the entry and deletes that day’s zero in a transaction. Interleaving them as count-zero, then log-and-delete, then create-zero leaves a day that has events and a `recorded_zeros` row. The summary line prefers the count, so the card still looks right. The today block still offers “Undo none” / “Clear none” next to real entries, because that block keys off `Today.Zero`.

The invariant in `PLAN.md` is that a day with events cannot also be none. Do the count, the insert, and the conflicting delete in one transaction, and treat a unique-index conflict as “the zero is already there.”

### 7. `newToken` ignores a failure from `crypto/rand`

```go
rand.Read(b)
return base64.RawURLEncoding.EncodeToString(b)
```

If the read fails, the returned string is an encoding of a zero or partial buffer, and it can become a session id, a share token, or a login token. Failure here is rare and means the process should stop. Check the error and fail the request, or panic at startup if the reader is unusable.

### 8. Share-link regeneration has no confirmation

Invite regeneration asks first (`hx-confirm` on the household page). “Make a new link” on the tracker edit form does not. It immediately replaces `share_token`. A kitchen tablet or a sitter holding the old URL starts getting 404s, which is what regenerate is for, and it is easy to hit beside “Turn off the share link.” Archive is the same kind of quiet control; the product text already treats archive as quiet. Regenerating a link that other people are using is closer to the invite case.

### 9. SQLite is using the default connection pool

`openDB` sets WAL, `foreign_keys`, `busy_timeout`, and `synchronous` on the DSN, and it does not call `SetMaxOpenConns`. `database/sql` then allows unlimited connections. SQLite still has one writer. Under overlapping requests the extra connections wait on `busy_timeout` (5 seconds) and then surface “database is locked” as a 500. A small cap is enough for one Dokku process. The pragma is on, and the models never declare foreign keys, so `foreign_keys=ON` is not enforcing anything until relations or constraint tags exist.

### 10. Old rows have no screen

History is `historyDays` (30) ending yesterday, plus today. Older entries stay in the database. Last occurrence still finds them. An owner cannot see, edit, or delete them until a later chart or a longer history exists. That matches the phase 2 wording (“the last 30 days”). It is worth remembering before real use passes a month: archive is not the only way data disappears from the product, and there is no export until phase 4.

Sessions, login tokens, and verification tokens are also kept after they expire. Expired sessions are deleted when that cookie shows up again. Unused rows are not. Fine for a household. A public deploy wants a periodic delete of expired rows so the file does not collect every login link ever sent.

## Intentional behavior worth keeping in mind

These match the tests and `PLAN.md`. They are easy to “fix” by accident.

- Any household member, and anyone with the share link, can undo any entry or recorded zero from the last 15 minutes, including one they did not log. `TestUndoWithinFifteenMinutes` says so. The share page prints those entry ids. A link left open on a tablet can remove a meal someone else just logged. The window is the mitigation.
- Two members of one household can disagree about “today.” Each page uses that user’s stored zone. The share page uses the household’s first owner, ordered by `household_members.created_at`. `TestTodayFollowsEachViewersStoredZone` and `TestSharePageTodayFollowsTheHouseholdCreatorsZone` both pass. A couple only shares a calendar day when their stored zones match. Signup copies the browser zone once and does not revise it on later requests.
- Promoting a member to owner cannot be undone. There is no demote and no leave. Phase 5 account deletion is the planned way out.
- Changing the password does not ask for the current one. The recovery path is a magic link, then settings. Anyone holding a live session can set a new password and log every other session out, including their own remaining one.

## Documentation drift

`ARCHITECTURE.md` says that if it describes a current behavior, `make test` covers it. Several present-tense sections describe things the binary does not do, and one behavior the binary does do is missing from that document.

| Document says | Code |
| --- | --- |
| Signup, login, and change password require Cloudflare Turnstile. Tests should reject a missing token. | No Turnstile widget, no siteverify call, no keys in `.env.example`. |
| Direct dependencies include `github.com/prometheus/client_golang`. `GET /metrics` is unauthenticated Prometheus text. | Not in `go.mod`. No `/metrics` route. |
| Layout lists `handlers_charts.go`, `handlers_billing.go`, `charts.html`, and vendored `chart.umd.min.js`. | Not in the tree. Chart.js is correctly described as phase 3 elsewhere; the file tree reads as the current layout. |
| `User` has `Plan`, Paddle ids, and `SubscriptionEndsAt`. | `models.go` has none of those. Fine as a phase 5 sketch if it is labeled as one. The struct block is not. |
| Routes and the data model stop at login tokens. | `VerificationToken`, `GET/POST /verify/{token}`, `GET /verify/done`, and `POST /settings/verify` are implemented. Signup sends that mail. Settings shows an unverified address. `PLAN.md` still says magic links are the verification. Both can be true, and the architecture doc should name the table and the routes. |
| `SUMMARY-DISPLAY.md` describes the code as it was before the feature: `todaySummary` on every card, no summary field on the form. | `summaryLine`, the radios on `tracker_form.html`, `static/app.js`, and `TestDoneTodaySummary` / `TestLastOccurrenceSummary` are in place. The plan’s “where the line is built today” section is historical. |

`PLAN.md` phase 2 still says summary display is not on the create form. The “After phase 2” section is the one that matches the code. That split is readable if phase 2 is kept as a record of what “done” meant. `SUMMARY-DISPLAY.md` should say the work landed, or it will send the next change looking for a form that is already there.

`home.html` says “dog meals, workouts, habits.” `PLAN.md` says the product is not a habit tracker. Small copy mismatch on the logged-out page.

Open questions the docs already track, still open, and still blocking a public deploy: which SMTP relay, and a backup that has been restored once. `run` warns when `ENV=prod` and `SMTP_HOST` is empty. It does not warn when `BASE_URL` is still the default `http://localhost:8080` or when `ENV` is left at its default `dev` (which turns off `Secure` on the session cookie). The Docker image sets `ENV=prod` and does not set `BASE_URL`.

## Before the first public deploy

Already required by `PLAN.md` and `ARCHITECTURE.md`, and not visible in this repo:

- A relay that delivers a magic link end to end.
- A backup that has been restored onto a copy of the database. One Dokku process, one SQLite file, Litestream or a nightly `.backup`.
- Turnstile, if the security section is still the decision. Finding 4 is the gap it was meant to close.
- `BASE_URL` and `ENV` set on the app, and the proxy in front stripping client-supplied `CF-Connecting-IP` and `X-Forwarded-For`.

Not required by the phase list, and worth doing in the same pass: findings 1, 2, and 3. They affect the two ways into an account and the links that grant access without an account.

## Test gaps

The suite is the right shape. These cases are not in it:

- Implicit submit of the login form while the password toggle is on posts to `/login` and creates a session. Today only an explicit post to `/login` is tested.
- A password set during signup stops working once the real mailbox verifies, or whatever rule replaces finding 2. The current tests assert the opposite for the session: unverified users stay logged in.
- Request logs for `/s/{token}`, `/join/{token}`, `/login/link/{token}`, and `/verify/{token}` do not contain the raw token.
- `recordZero` and `logEntry` overlapping on one day leave either entries or a zero, not both.
- `newToken` fails closed when `rand.Read` returns an error.
- Turnstile and `/metrics`, once they exist. The architecture test list already names both.
