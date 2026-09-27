# Code review

Reviewed 2026-09-27 at commit `f3edec0` (email verification). Scope: every Go file, template, `static/app.js`, the Dockerfile and build files, checked against `ARCHITECTURE.md` and `PLAN.md`. The project is at the end of phase 2, just before the first public deploy, so findings are ranked by what matters for that deploy.

`go vet` is clean, `go test -race ./...` passes, and statement coverage is 83.7%.

## Summary

The code is small, consistent, and careful. Access control goes through a few functions in `access.go`. Tokens are single-use through conditional updates. GET never consumes a token. Templates rely on `html/template` escaping throughout. The tests exercise real HTTP flows.

Three things should be fixed before the public deploy:

1. **Account pre-hijacking.** Someone can sign up with another person's email and set a password. When the real owner later logs in by email link, the squatter keeps both the password and a live session.
2. **Open redirect.** `safeNext` lets `/<TAB>/evil.com` through, and browsers follow it to `evil.com`.
3. **Rate limits trust client-supplied headers.** If the origin can be reached without going through Cloudflare, anyone can reset every limit on each request. The limiter's cleanup also turns into a linear scan under a lock once it holds more than 10,000 live keys.

The rest are medium or low. Each finding lists where it is, what goes wrong, and a suggested fix.

---

## High

### H1. Unverified signups can pre-hijack an account

`auth.go` `handleSignup` (218–287), `handleLoginLinkUse` (454–486), `handleVerifyUse` (544–569)

Signup is not blocked by verification, and that is intended. The problem is what happens when the mailbox owner arrives later:

1. An attacker signs up as `victim@example.com` with a password. They get a session right away.
2. The victim tries to sign up, sees "An account with that email already exists. Log in instead.", and logs in with an email link.
3. `handleLoginLinkUse` marks the email verified and starts a new session. It does not touch the attacker's password or session.
4. The attacker keeps full access for as long as they like, because sessions renew. They can also log in again with the password. They see every household and tracker the victim creates or joins.

The same thing happens if the victim clicks the "Confirm your email" message that was sent when the attacker signed up.

**Fix.** Proving control of the mailbox should end any access set up before that proof. In `handleLoginLinkUse`, and in `handleVerifyUse`, when the account's `EmailVerifiedAt` was nil, run one transaction that:

- clears `password_hash` if the password was set before verification. That is always the case today, because the only other place a password is set is Settings, which needs a session;
- deletes every existing session for the user;
- sets `email_verified_at`.

Only then start the new session. For `handleVerifyUse`, clearing the password would also hit a real user who signed up with a password and is now verifying. Two options: tell them on `/verify/done` that other devices were logged out, or only delete sessions other than the current one. Losing the password is the strongest protection. If that is too strict, at least delete all other sessions. A test should cover the sequence above.

### H2. Open redirect through control characters in `next` and `back`

`auth.go` `safeNext` (81–86), used at 286, 343, 380, 485; `handlers.go` `redirectBack` (72–78)

`safeNext` checks the first two bytes only. `"/\t/evil.com"` passes. `http.Redirect` cannot parse that value, so it skips its own path cleaning and writes the header unchanged: `Location: /\t/evil.com`. This was checked with a small test. Browsers remove ASCII tab and newline characters when parsing a URL, so the browser goes to `//evil.com`.

A phishing link such as `https://trackanything.io/login?next=/%09/evil.com` sends the victim to the attacker's site right after a real login. `back` is only read from POST bodies behind CSRF protection, so it is much harder to exploit, but it uses the same function.

**Fix.** Parse the value and reject anything that has a scheme or host, or that fails to parse. `url.Parse` rejects control characters:

```go
func safeNext(next string) string {
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" ||
		!strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}
```

Add `"/\t/evil.io"`, `"/\n/evil.io"`, and `"/\r/evil.io"` to the table test in `auth_test.go` (around line 347).

### H3. Rate limits key on headers the client can set

`ratelimit.go` `clientIP` (57–72) and `allow` (30–53)

`clientIP` trusts `CF-Connecting-IP` first, then the first `X-Forwarded-For` hop. The comment and `ARCHITECTURE.md` say this is safe because the app can only be reached through Cloudflare. In the planned topology, though, the Fly.io ingress has its own public IP. If it accepts traffic that did not come from Cloudflare, any client can send a random `CF-Connecting-IP` with each request, and the login, magic-link, signup, and share limits stop applying. The first `X-Forwarded-For` hop is set by the client even when traffic does go through Cloudflare, because Cloudflare appends to that header rather than replacing it.

With a new key on every request, `hits` also grows without limit inside a window. The signup window is one hour. Once there are more than 10,000 live keys, every `allow` call walks the whole map while holding the mutex and deletes nothing. That makes a cheap CPU and memory denial of service.

**Fix.**

- Make trust explicit in config, for example `TRUSTED_IP_HEADER=CF-Connecting-IP`, empty in dev. Read only that header, and ignore `X-Forwarded-For` unless it is configured. In dev, use `RemoteAddr`.
- On the Fly ingress, accept only Cloudflare's IP ranges, or use Cloudflare Authenticated Origin Pulls. Record which one is used under Deployment in `ARCHITECTURE.md`.
- Put a hard cap on the limiter map. Past the cap, either refuse new keys, which fails closed, or drop the oldest windows. Run the expired-entry sweep on a timer or on every Nth call rather than on every call past the threshold.

---

## Medium

### M1. Session IDs are stored as plain text

`models.go` `Session` (19–24), `auth.go` `startSession` (131–138), `loadUser` (151)

Login and verification tokens are stored hashed "so a database leak can't be used to log in". Session IDs are stored as they are. Sessions last 30 days and renew, which makes them the more valuable of the two, and backups are about to copy this database off the machine. Anyone with a backup could use those rows as live sessions.

**Fix.** Store `hashToken(id)` as the primary key and look it up by hash. Keep the raw value only in the cookie. Share and invite tokens have to stay readable because the app shows those URLs, so they are fine as they are.

### M2. Nothing limits mail to a single address

`auth.go` `handleLoginLinkRequest` (353–395), `handleSignup` (230, 283); `main.go` (84–88)

Link requests are limited per IP (5 every 15 minutes), and signup is limited per IP (10 per hour). No limit applies per recipient. From several IPs, or from any number once H3 is exploited, someone can flood a person's inbox with login links. Signup also sends a verification email to any address that is typed in. Both hurt the relay's sender reputation, and a new production relay is still being chosen.

**Fix.** Add a limiter keyed on the normalized recipient address, for example 3 per 15 minutes, and check it in `sendLoginLink` and `sendVerificationEmail`. When it trips, still show the same "check your email" page so it reveals nothing.

### M3. The account-enumeration protection is only partial

`auth.go` 262–265 and 370–395

The comment at 370 says the link form "can't be used to find out who has an account". In practice:

- Signup says outright that an account exists.
- For an existing account, the link request writes a row and waits on SMTP before it responds, which can take hundreds of milliseconds. For an unknown address it responds immediately.
- When SMTP fails, only existing accounts get the 500 "We couldn't send your login link" page.

Pick a stance and make the code match it. If enumeration is acceptable for this product, which is reasonable because signup already reveals it, drop the claim from the comment. If it is not acceptable, signup must not reveal the account: send the existing user a "someone tried to sign up" email and show the same page either way. Mail also has to be sent in the background, so response time and SMTP errors don't depend on whether the account exists.

### M4. A removed member can rejoin with the same invite link

`handlers_households.go` `handleRemoveMember` (128–142), `handleJoin` (211–223)

Removing a member leaves the household's invite token as it is. If invites are on, the removed person can open the link they already have and join again right away.

**Fix.** Either rotate `invite_token` automatically when a member is removed, or warn in the `hx-confirm` text and on the household page that the invite link still works. Rotating fits "remove" better.

### M5. Server errors and network failures are silent under HTMX

`templates/layout.html` 15, `render.go` `serverError` (81–84), `static/app.js` 20–36

The `htmx-config` swaps 2xx through 4xx responses and does not swap others. A 500, or a request that never reaches the server, shows nothing. Meanwhile the optimistic update in `app.js` has already changed the summary, for example "3 times today". The card keeps the wrong count until the page reloads, and the user reasonably thinks the tap was saved. This is the main interaction in the app.

**Fix.** Listen for `htmx:responseError` and `htmx:sendError` in `app.js`. Put the summary back to its previous text and show a short inline message. Serving an HTML error page for 5xx (the `message.html` template) would also be an improvement on plain text for non-HTMX requests.

### M6. An expired session during a POST ends at a 405

`auth.go` `requireUser` (191–199)

For a POST, `requireUser` redirects to `/login?next=<POST URL>`. After logging in, the browser does a GET on a route that only accepts POST and gets 405. The most likely way to hit this is a session that expires while the tab is open, followed by a tap on Log, since that goes through HTMX. The tap is also lost.

**Fix.** For non-GET requests, set `next` from the `Referer` path, run it through `safeNext`, and fall back to `/`. It would also help to show "You were logged out, so that wasn't saved" on the login page.

### M7. Production configuration isn't validated at startup

`main.go` `loadConfig` (41–57) and 121–123; `auth.go` 115, 126; `mail.go` 103–119

- If `BASE_URL` is not set in prod, it defaults to `http://localhost:8080`, and every emailed link is broken. That only shows up when someone tries to log in.
- `ENV` accepts anything. `ENV=production` gives `Secure` cookies (because it isn't `"dev"`) but text logs and no SMTP warning (because it isn't `"prod"`).
- With no `SMTP_USER`, if the server doesn't offer STARTTLS, mail is sent unencrypted. That mail contains login links. With a user set, `smtp.PlainAuth` refuses to send without TLS, which is the right behavior, but the error counts as transient and gets retried.
- Implicit TLS on port 465, which some relays require, isn't supported. The connection just waits until the 5-second timeout.

**Fix.** In `loadConfig`, or in a `validate()` function called from `run`: allow only `ENV` values `dev` and `prod`. In prod, require `BASE_URL` to be set and to start with `https://`. In prod, require STARTTLS, or implicit TLS when the port is 465, and fail the send if neither is available.

### M8. Docs describe security and ops features that aren't built

`ARCHITECTURE.md` Security, Metrics, Deployment

The docs describe Cloudflare Turnstile on signup, login, and password change, and `GET /metrics` for Prometheus. Neither exists in the code, and `PLAN.md` doesn't put either in a later phase. Backups are listed as required before the first deploy. That is fine if these are still to do, but right now the Security section describes protections the app doesn't have. Either build them before the deploy or mark them as planned in `ARCHITECTURE.md`. H3 and M2 matter more while Turnstile is missing.

---

## Low

### L1. Races that leave data out of sync

- `handlers_entries.go` `recordZero` (31–46) counts entries and then inserts the zero outside a transaction. If a log lands between those two steps, the day ends up with entries and a recorded zero. History handles it because `countsByDay` sets `Zero` only when the count is 0. The tracker and share pages still show "Undo none" or "Clear none" next to real entries. Fix: run the count and the insert in one transaction.
- `auth.go` `handleSignup` (257–273) counts and then creates. Two signups at the same moment with the same email hit the unique index, and the second gets a 500 instead of the "already exists" message. Fix: check for a unique-constraint error on `Create`.
- `handlers_trackers.go` 430–444: `MAX(position)+1` can give two trackers the same position. That does no harm today because ordering falls back to `id`.

### L2. SQLite write concurrency

`db.go` 26–28

GORM keeps a pool with more than one connection. In WAL mode, a deferred transaction that reads first and then writes can get `SQLITE_BUSY` right away, whatever `busy_timeout` is set to. That happens when another connection wrote in the meantime. Today's transactions mostly write first, so this is unlikely at current traffic. It is worth fixing before it shows up as intermittent 500s. Options: `sqlDB.SetMaxOpenConns(1)`, which is simple and fine at this scale, or immediate transactions if the driver supports a `_txlock=immediate` DSN option. Also, `DB_PATH` gets the pragmas appended with `?`, so a path that already has a query string breaks. That is unlikely.

### L3. Expired rows are never cleaned up

`sessions`, `login_tokens`, and `verification_tokens` keep growing. An expired session is deleted only when its cookie shows up again, and used or expired tokens are never deleted. Add a small hourly goroutine in `run`, stopped by the shutdown context, that deletes expired or used rows. M1 makes old session rows less dangerous, but they should still go.

### L4. No security headers on normal pages

`main.go` 225–226

Only share pages set `Referrer-Policy` and `X-Robots-Tag`. Nothing sets `X-Content-Type-Options: nosniff`, and nothing stops the app being framed. With no framing protection, a page on another site can frame "Join household", "Make owner", or the Log button and trick someone into clicking. Add a small middleware that sets `X-Content-Type-Options: nosniff`, `Content-Security-Policy: frame-ancestors 'none'` (a full CSP later), and `Referrer-Policy: same-origin` everywhere. The household and tracker edit pages show invite and share URLs, so `same-origin` covers them too.

### L5. What the share page shows

`handlers_share.go` 72, `templates/partials/tracker.html` 35

Anyone with a share link sees the notes on today's entries, including notes household members wrote for each other, for example on a medication tracker. The share page also puts the household creator's IANA time zone in `data-time-zone`. Both may be fine, but it should be a deliberate decision. `ARCHITECTURE.md` lists "today's entries" and doesn't mention notes. If notes should stay inside the household, pass an option to `entryViews` that hides them.

### L6. Panic recovery and request logging

`middleware.go`

- `recoverPanic` wraps `logRequests`, and `logRequests` logs only after `next` returns. A request that panics never gets a request log line.
- A panic after the handler has started writing leads to a second `WriteHeader` and a "superfluous WriteHeader" warning.
- `http.ErrAbortHandler` should be re-panicked rather than logged as an error.
- `statusRecorder` has no `Unwrap`, so `http.NewResponseController` can't reach the underlying writer.

Put the logging in a `defer`, re-panic `ErrAbortHandler`, and add `func (r *statusRecorder) Unwrap() http.ResponseWriter`.

### L7. Static files

`main.go` 171

- `GET /static/` returns a directory listing.
- Embedded files have no modification time, so responses have no `Last-Modified` or `ETag`, and `htmx.min.js`, `pico.min.css`, and `app.css` get downloaded again on every page load.

Wrap the file server so it returns 404 for paths ending in `/`, and set `Cache-Control`. Either version the URLs, for example with a build-hash query string added through a template function, and use a long `max-age`, or use a short `max-age` with an ETag computed at startup from each file's hash.

### L8. HTML responses have no caching headers

`render.go` 53–59

The same URL returns a full page or only the content block depending on `HX-Request`, and no `Vary: HX-Request` is sent. Pages that depend on the logged-in user have no `Cache-Control` either. The risk is low today, because without a `Last-Modified` header browsers don't cache these responses. It is a one-line fix: send `Vary: HX-Request` and `Cache-Control: no-store` on rendered pages. That also keeps the browser from serving logged-in pages from its back-forward cache after logout.

### L9. Query count and indexes

- `handlers.go` `dashboard` (103–135) runs 3 queries per household plus 2 to 3 per tracker. Three households with ten trackers each is about 70 queries per home page load. SQLite on the same machine handles that. It is the first thing to batch if the dashboard slows down: load today's entries for all trackers in one `IN` query, and zeros the same way.
- `Entry` has separate indexes on `tracker_id` and `occurred_at` (`models.go` 102–103). Every query filters on the tracker and a time range, or sorts by time within a tracker, so a composite index `(tracker_id, occurred_at)` fits better.
- `location` (`days.go` 16–22) calls `time.LoadLocation` each time, and `LoadLocation` has no cache, so it parses the embedded tzdata on every call. The settings page does that about 150 times per render (`timezones.go` 182–210). Cache locations in a `sync.Map`.
- `loadUser` runs two queries per request, one for the session and one for the user. A join would make it one.

### L10. Foreign keys are on but none are declared

`db.go` turns on `foreign_keys(ON)`, but no model declares a relation, so SQLite has no constraints to enforce. Nothing deletes users, households, or trackers yet. Phase 5 account deletion will need either declared constraints with `ON DELETE CASCADE`, or handwritten deletes in one transaction. Better to decide before data builds up.

### L11. Session handling details

- `POST /login` and `POST /signup` while already logged in start a second session and leave the first one in the database with no cookie pointing to it. Delete the current session before starting a new one.
- Changing the password logs out other sessions but leaves outstanding login links valid. Mark them used at the same time.
- Settings has no "log out everywhere" option. The only way to do it today is to change the password. Combined with H1, that is the only way out for someone who suspects another person has access.

### L12. Mail details

`mail.go`

- No `Message-ID` header. Most relays add one, but some spam filters score its absence.
- `transientSMTPError` treats every non-`textproto` error as transient, including local configuration errors such as PlainAuth's "unencrypted connection". Those get retried pointlessly. Only network errors (`net.Error`, `io.EOF`) should be retried.
- Mail is sent while the request waits: up to 10.5 seconds when both attempts time out, against a 15-second `WriteTimeout`. That fits, but with no room to spare. A small background send queue would also help with M3.

---

## Code quality

These don't change behavior but would make the code easier to change:

- `handleLoginLinkConfirm` and `handleVerifyConfirm`, and likewise `handleLoginLinkUse` and `handleVerifyUse`, are near-copies. A generic "find unused token" and "consume token" helper parameterized by model would remove about 60 lines and keep the two flows from drifting apart. H1 has to change both.
- `a.logger.Error("server error", "method", ..., "path", ..., "err", err)` is repeated at `auth.go` 284 and 381 and `settings.go` 102. Add `a.logError(r, err)` and have `serverError` call it.
- `trackerURL(t)` exists, but `handlers_trackers.go` builds the same string with `fmt.Sprintf` at 117, 449, 510, and 550. There is a matching `householdURL` helper, but 524 and 538 don't use it.
- `fmt.Sprintf("%d", u.ID)` in `settings.go` 95 could be `strconv.FormatUint`.
- `handleLoginLinkUse` and `handleVerifyUse` do a conditional `UPDATE` and then `Take` the row by hash. `UPDATE ... RETURNING user_id`, which GORM supports through `clause.Returning` on SQLite, would do both in one statement and remove a second lookup that can fail.

## Tests

Coverage is good where it matters most: access rules, undo windows, share links, time zones and DST, and the SMTP retry path. The suite takes about 5 seconds, or about 30 with `-race`. Gaps:

- `safeNext` with control characters (H2).
- The pre-hijack sequence (H1).
- `clientIP` with spoofed headers, and the limiter's behavior past its size cap (H3).
- `loadConfig`, `run`, and `newLogger` have no coverage, so config validation (M7) would be new code with no tests.
- `recoverPanic` is only partly covered, and `handleShareUndoZero` and `zeroForUser` only cover the success path.
- A test that renders every template with representative data would catch template runtime errors such as a nil field or a bad icon name. `ARCHITECTURE.md` already asks for one for icons.

## Suggested order

1. H2 (small change, one test), then H1, then H3 together with the ingress lockdown.
2. M1 and L3 together, since both change how sessions are stored and cleaned up.
3. M7 and M8 before the deploy: config validation, and either build Turnstile and metrics or mark them as planned.
4. M2, M4, M5, and M6.
5. The low findings as they come up.
