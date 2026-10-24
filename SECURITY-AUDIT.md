# Security audit

Date: 23 October 2026. Scope is the Track Anything source tree: Go handlers, templates, vendored front-end scripts, auth, headers, and the Docker build. This is a code review plus `govulncheck` against the local Go 1.27.0 toolchain. It is not a live test of the Fly or Dokku edge, and those proxy configs are not in this repository.

## CSP and CSRF

**Content-Security-Policy.** Yes, as a response header, and only as a clickjacking control. `securityHeaders` in `middleware.go` sets this on every response:

```
Content-Security-Policy: frame-ancestors 'none'
```

There is no `<meta http-equiv="Content-Security-Policy">` tag in the templates. The policy has no `default-src`, `script-src`, `style-src`, `connect-src`, or `base-uri`. It does not restrict where scripts, styles, or connections come from. `X-Frame-Options` is not set; `frame-ancestors 'none'` covers current browsers.

Pages load scripts from this origin (`/static/htmx.min.js`, `/static/app.js`, `/static/chart.js`, `/static/reorder.js`, `/static/chart.umd.min.js`) and, on signup, login, and change-password, from `https://challenges.cloudflare.com/turnstile/v0/api.js`. A tighter policy would have to allow that host. Chart data is inlined as `<script type="application/json">` built with `json.Marshal`, which escapes `<`, `>`, and `&`, so a tracker name cannot close the script element. That protection is in the JSON encoder, not in CSP.

**CSRF tokens.** No. Forms do not carry a synchronizer token, a double-submit cookie, or a custom `X-CSRF-Token` header. Nothing in the templates or handlers looks up a per-session CSRF secret.

State-changing requests are rejected another way. `routes` wraps the mux with `http.NewCrossOriginProtection()` (`main.go`). Go 1.27's check (`net/http/csrf.go`) does this:

- `GET`, `HEAD`, and `OPTIONS` are always allowed.
- `Sec-Fetch-Site: same-origin` or `none` is allowed.
- Any other `Sec-Fetch-Site` value is rejected with 403.
- If `Sec-Fetch-Site` is absent and `Origin` is present, the origin host must equal the `Host` header.
- If both headers are absent, the request is allowed. Go treats that as a non-browser client or a same-origin request from an old browser.

The session cookie is `HttpOnly`, `Path=/`, `SameSite=Lax`, and `Secure` when `ENV` is not `dev`. `SameSite=Lax` withholds the cookie on cross-site `POST`, which is a second barrier for browser CSRF against an existing session. `TestCrossOriginPostsAreRejected` covers a cross-site `Sec-Fetch-Site` and a foreign `Origin`. No routes are exempted, and no extra trusted origins are registered.

Two gaps are built into that design, and Go's own comments call out the second one:

1. A request with neither `Sec-Fetch-Site` nor `Origin` is allowed. A non-browser client that already has the cookie is outside the CSRF threat model. A browser that omits both headers is not. Current browsers send at least one of them on cross-site form posts.
2. When `Sec-Fetch-Site` is missing and the `Origin` host equals `Host`, the check allows the request even if the schemes differ (`http` vs `https`). The standard library says sites should close that with HSTS. This app does not send `Strict-Transport-Security`.

Login CSRF (tricking a browser into adopting an attacker's session) does not need the victim's cookie. It needs the victim's browser to `POST` the attacker's magic-link token. Modern browsers send `Origin` and `Sec-Fetch-Site: cross-site` on that post, and the check rejects it. The same post from a client that sends neither header would succeed.

`Host` must be the public host. `CrossOriginProtection` compares `Origin` to `Host`. The deployment notes already require the proxy chain to preserve `Host`. A client-controlled `Host` that the proxy forwards would make a foreign origin look same-origin.

## Findings

| Severity | Location | Finding |
| --- | --- | --- |
| Medium | `go.mod`, `Dockerfile` | The tree is built with Go 1.27.0. `govulncheck` reports 13 reachable standard-library vulnerabilities, fixed in Go 1.27.2 and 1.27.3. |
| Low | `middleware.go` | CSP does not constrain scripts. A future XSS bug would run with a full session cookie available to same-origin script. |
| Low | `middleware.go` | No HSTS. That is the mitigation Go documents for the scheme-blind origin check above. |
| Low | `models.go`, `handlers_trackers.go`, `handlers_households.go` | Share tokens and invite tokens are stored in plaintext. A database or backup copy is enough to log on a shared tracker or join a household. |
| Low | `handlers_export.go` | CSV export writes notes, names, units, and emails without neutralizing spreadsheet formulas. |
| Low | `auth.go` `handleSignup` | Signup says "An account with that email already exists." Magic-link requests do not reveal whether the address is registered. |
| Low | `render.go` | Authenticated HTML is sent with no `Cache-Control`. A shared cache in front of the app could store a logged-in page. |
| Low | `main.go` | Request bodies are bounded by the 15s read timeout, not by a byte limit. Parsing a form or multipart body can allocate until that timeout. |

No high-severity issue turned up in the application logic: no missing ownership check on the routes reviewed, no SQL built from raw user strings, and no reflected HTML that skips `html/template`.

### Medium: Go 1.27.0 standard library

`go.mod` says `go 1.27.0`. The local toolchain used for this scan is Go 1.27.0. `Dockerfile` uses `FROM golang:1.27`, which floats, so a fresh image pull may already be newer than the module line. Pin the image and the toolchain to at least Go 1.27.3 and rebuild.

`govulncheck ./...` on 23 October 2026 reported these as called from this program. All are in the standard library. One finding in an imported module and one in a required module were reported as not on a call path (`golang.org/x/crypto/openpgp`, and `os.Root.Mkdir` on Windows). This app does not use SSH, OpenPGP, or Windows path APIs.

| ID | Fixed in | What it is | Why it matters here |
| --- | --- | --- | --- |
| GO-2026-6809 | Go 1.27.3 | `crypto/x509` still trusts intermediate certificates that should be distrusted | Turnstile Siteverify and production SMTP verify TLS certificates |
| GO-2026-6599 | Go 1.27.2 | `html/template` can mis-track context across consecutive actions | Every page is rendered with `html/template`. User content in this app sits in HTML text and attributes, and chart JSON is inserted with `template.HTML` after `json.Marshal`. I did not find a template that obviously matches the published bug shape. Upgrade anyway; the escaper is the XSS boundary. |
| GO-2026-6600 | Go 1.27.2 | `html/template` mishandles `yield` before an action in a JavaScript context | Same boundary. Chart JSON is not parsed as JavaScript. |
| GO-2026-6613 | Go 1.27.2 | HTTP/1 connection desynchronization after a 2xx response to `CONNECT` | The process speaks plain HTTP behind nginx. Request smuggling between the proxy and this server is the relevant case. |
| GO-2026-6605 | Go 1.27.2 | HTTP/1 client desynchronization after a rejected `CONNECT` | The outbound client used for Siteverify |
| GO-2026-6608 | Go 1.27.2 | MIME header parsing can exceed its memory limit | `PostFormValue` parses request bodies, including multipart |
| GO-2026-6609 | Go 1.27.2 | No limit on parsed `Range` headers | `GET /static/` is served by `http.FileServer` |
| GO-2026-6607 | Go 1.27.2 | Malformed ECH outer extensions in `crypto/tls` | Outbound TLS for Siteverify and SMTP |
| GO-2026-6610 | Go 1.27.2 | HTTP/2 client accepts malformed framing-related headers | Outbound HTTP/2 to Cloudflare |
| GO-2026-6617 | Go 1.27.2 | HTTP/2 server crash from an HPACK encoder race | The server enables HTTP/2 only when it terminates TLS. This process does not. The client path still links the package. |
| GO-2026-6612 | Go 1.27.2 | HTTP/2 server double flow-control refund | Same as above |
| GO-2026-6603 | Go 1.27.2 | HTTP/2 server memory use from `Trailer` headers | Same as above |
| GO-2026-6611 | Go 1.27.2 | HTTP/2 CPU use from repeated initial window changes | Same as above |

### Low: CSP is only `frame-ancestors`

Clickjacking of the logged-in UI is blocked for browsers that honor `frame-ancestors`. Script injection is not. `html/template` is what stops XSS today. `template.HTML` is used for three things, all reviewed:

- Embedded chart and overlay JSON, after `encoding/json` (`handlers_trackers.go`). `json.Marshal` escapes `<`.
- Tabler SVGs from the embedded `icons/` files, and emoji passed through `template.HTMLEscapeString`. Tracker icons are an allow-list (`icons.go`). `embed.FS` rejects `..` in icon names.
- Apple startup-image `<link>` tags built from fixed splash sizes (`splash.go`), not from user input.

`static/chart.js` puts tooltip text in `textContent`, not `innerHTML`. `static/app.js` does the same for the optimistic summary.

### Low: no HSTS

The browser-facing site is HTTPS at Fly Proxy. The Go process listens on plain HTTP and never sets `Strict-Transport-Security`. If the edge does not add HSTS either, a first visit or a downgrade stays possible, and the scheme-blind branch of `CrossOriginProtection` stays open for clients that do not send `Sec-Fetch-Site`. Set HSTS on the HTTPS response the browser actually sees, at the proxy or from this app when `ENV=prod`. Include subdomains only if every host name on the parent domain is HTTPS.

### Low: share and invite tokens are recoverable from the database

Session ids, magic-link tokens, and verification tokens are stored as SHA-256. The raw value exists only in the cookie or the email. Share tokens and household invite tokens are stored as the URL secret itself, so the owner can display the link again. Both are 32 bytes from `crypto/rand`, base64url-encoded, so guessing them is not practical.

A copy of `trackanything.db`, including a Litestream or cron backup, contains live links. A share link can log, record none for today, and delete entries and recorded zeros created in the last 15 minutes on that tracker. An invite link can add any logged-in account as a member. Archiving a tracker clears its share token. Turning an invite off, replacing it, or removing a member rotates or clears the invite. Nothing expires them on a timer.

This matches `ARCHITECTURE.md`. Hashing them would mean the link can be shown only at creation. Worth doing if backups are a realistic leak, which the deployment notes already treat as something that must exist before launch.

### Low: spreadsheet formulas in CSV export

`GET /trackers/{id}/export` is owner-only and uses `encoding/csv`. A note, tracker name, unit, or email that starts with `=`, `+`, `-`, `@`, tab, or carriage return is written as a normal field. Excel and some other spreadsheet apps evaluate that as a formula when the owner opens the file. A household member can set a note. Prefix those fields with a quote, or tab-separate and quote, before writing the row.

The `Content-Disposition` filename is passed through `strconv.Quote`, so a tracker name cannot break the header.

### Low: signup reveals whether an email is registered

`handleSignup` returns "An account with that email already exists. Log in instead." Password login uses one message for a missing account, an unverified account, and a bad password, and it compares a dummy bcrypt hash when there is nothing to check. Magic-link requests render the same "if there's an account" page either way, and the per-address mail cap returns that same page when it throttles. Signup is the exception. It is also behind Turnstile and a per-IP limit of 10 per hour, which makes bulk enumeration slower. It does not make the answer uniform.

### Low: authenticated responses are cacheable by default

`render` sets `Content-Type` and a status. It does not set `Cache-Control`. Splash images set `public, max-age=86400`, which is fine. Logged-in HTML, the CSV export, and share pages rely on the browser and any proxy not caching them. `Referrer-Policy: no-referrer` and `X-Robots-Tag: noindex` on share responses do not stop a cache. Send `Cache-Control: private, no-store` on responses that depend on a session or a bearer token.

### Low: no maximum body size

`http.Server` sets `ReadHeaderTimeout` (5s), `ReadTimeout` (15s), `WriteTimeout` (15s), and `IdleTimeout` (60s). It does not set a max bytes limit, and handlers do not wrap the body with `http.MaxBytesReader`. Field lengths after parsing are capped (tracker name 60, note 500, log label 24, unit 16, password 72). The cap applies after the body is read. Combined with GO-2026-6608 until Go is upgraded, a large or many-header body is a memory DoS against the single SQLite connection.

## What is already in good shape

**Sessions.** 32 random bytes, only the SHA-256 stored, 30-day expiry, sliding renewal at halfway, cookie `HttpOnly` and host-only (no `Domain` attribute). Logout deletes that session row. Login always creates a new row rather than reusing a presented cookie. Expired rows are deleted hourly.

**Passwords.** bcrypt at the default cost. Login timing does not skip the compare when the account or the hash is missing. Password login requires `EmailVerifiedAt`. Setting a password requires a verified address and a Turnstile pass, and it deletes every other session. Length is 8 to 72 bytes, which is bcrypt's own limit.

**Mailbox proof.** The first successful magic link or verification link clears any password and every session, then marks the address verified. The verification email tells the recipient to open the link even if they did not sign up, which is the right mitigation for "someone else signed up with your address." GET does not consume either token, so a mail scanner cannot use it up. Consumption is a single conditional update, so two posts cannot both succeed.

**Open redirects.** `safeNext` allows only a local path. Scheme-relative URLs, backslashes, control characters, and values `url.Parse` treats as having a host fall back to `/`. Post-login `next` uses it. `back` on form posts uses it. Overlay ids copied from `Referer` or `HX-Current-URL` are parsed as integers and re-checked with `trackerForUser`.

**Authorization.** Tracker and household reads go through `trackerForUser`, `householdForUser`, `archivedTrackerForOwner`, or `trackerForShareToken`. A missing row and a row the caller cannot see both become 404. Owner-only actions (edit, delete, archive, share on/off, export, backfill, invite, remove, promote, reorder within a household) check `roleOwner`. Reorder posts must contain exactly the caller's current id set. Share undo requires the entry to belong to the tracker behind that token. Creating a tracker checks ownership of the target household.

**Queries.** GORM conditions use placeholders. I did not find a `Raw` query or a `Sprintf` into SQL. SQLite is opened with `foreign_keys=ON`, `busy_timeout`, and WAL, and the pool is one connection.

**Abuse controls.** Turnstile is checked on the server for signup, password login, magic-link request, and password change. A missing token, a network error, a non-200, or `success: false` all reject the action. The secret is not logged. Production refuses to start without both Turnstile keys and an `https` `BASE_URL`. Rate limits: login 10/minute/IP, magic link 5 per 15 minutes/IP, signup 10/hour/IP, share posts 30/minute/IP, auth mail 3 per 15 minutes per address. Client IP is `RemoteAddr` unless `TRUSTED_IP_HEADER` is one of an allow-list, and other forwarding headers are ignored. The limiter stops admitting new keys at 10,000. Production is documented to use `Fly-Client-IP`, which Fly nginx overwrites.

**Headers and cookies.** `X-Content-Type-Options: nosniff` on every response. `Referrer-Policy: same-origin` by default, `no-referrer` on share responses, so a share token in the path is not sent off-site as a referrer. Login, verify, share, and invite paths are redacted in request logs. Server errors log the error and return `500 Internal Server Error` with no detail. Dev mail logs the message body; production mail does not.

**Redirects and email.** Production mail refuses to send if the SMTP connection never became TLS (implicit TLS on port 465, STARTTLS otherwise). `MAIL_FROM` is parsed with `net/mail`. Signup addresses must round-trip `mail.ParseAddress` as a bare address, which rejects display names and newlines.

**Container.** Multi-stage build, `CGO_ENABLED=0`, distroless nonroot, `/data` owned by uid 65532. `.env` is gitignored. `.env.example` has no production secrets. The process refuses `ENV` values other than `dev` and `prod`.

**Front end.** No `eval`, no `document.write`, no assignment to `innerHTML` in `app.js` or `chart.js`. HTMX swaps server HTML, which is escaped by the template engine. Static files are embedded. The file server is mounted at `GET /static/`; a `../` in the URL is cleaned by `ServeMux` before routing, so it cannot reach `templates/`.

## Accepted behavior, with the residual risk

These are specified in `ARCHITECTURE.md`, `DESIGN.md`, or `PLAN.md`. They are real capabilities. They are not bugs relative to that spec.

- **Share links are bearer tokens.** Anyone with the URL can log, mark today as none, see today's entries and notes, and undo anything created in the last 15 minutes on that tracker, including an owner's entry. There is no login and no Turnstile on that surface. The per-IP limit is 30 posts per minute. History, charts, settings, and other trackers are not on the page. The token in the path will sit in browser history and in any proxy access log that records full URLs. This app's own log line redacts it.
- **Any household member can undo a recent entry.** Documented. Delete and edit of older rows are owner-only.
- **Password change does not ask for the current password.** The recovery path is a magic link, then Settings. A stolen session can set a password and delete other sessions. The victim can still request a magic link. On an already-verified account that link does not delete other sessions; changing the password again does.
- **Signup starts a session before the mailbox is confirmed.** Until the recipient opens the verification or magic link, the signup browser is signed in and no password exists. Opening the link clears that session.
- **Household members see one another's email addresses** on the household page. Export includes the email of whoever recorded a row. Both are limited to people already in the household, and export is owner-only.
- **`X-Forwarded-For` uses the first address** when that header is the configured trusted header. That is safe only if the proxy replaces the header. The code does not assume it. Production is specified to use `Fly-Client-IP` instead. Leaving `TRUSTED_IP_HEADER` empty until the process is unreachable except through that proxy is the right default. If it is set while the origin is reachable directly, clients can pick their rate-limit key.
- **Rate limits are in-process.** A restart clears them. Authenticated logging, settings, and export are not rate limited. One process and one SQLite writer means a fast client can stall everyone else until the read timeout. That is an availability property of the chosen architecture.
- **No TLS inside the process.** Confidentiality of the session cookie depends on Fly terminating HTTPS and on `Secure` being set in production, which it is. The cookie is not marked `Partitioned`, and it does not use the `__Host-` prefix. `__Host-` would also require `Secure`, which is off in `ENV=dev` so localhost works.

## Suggested order of work

1. Build and deploy with Go 1.27.3 or a later 1.27 patch. Pin `golang:1.27.3` (or newer) in `Dockerfile` so the floating `1.27` tag cannot drift back.
2. Add HSTS on the response the browser sees over HTTPS.
3. If you want defense in depth beyond clickjacking, extend the CSP with `default-src 'self'`, an explicit `script-src` that includes `https://challenges.cloudflare.com` for the Turnstile pages, and `base-uri 'self'`. Keep `frame-ancestors 'none'`. Chart JSON does not need `unsafe-inline` if it stays `type="application/json"`.
4. Send `Cache-Control: private, no-store` on HTML and CSV that depend on a cookie or a share token.
5. Cap request bodies with `http.MaxBytesReader`.
6. Neutralize formula characters in CSV fields.
7. Decide whether share and invite tokens should stay recoverable from a backup. If not, store a hash and show the raw URL only when it is created or rotated.

Synchronizer CSRF tokens are not required to match the model already implemented, as long as `Host` stays trustworthy, HSTS is present, and browsers keep sending `Sec-Fetch-Site`. Adding hidden tokens would be a third layer, not a replacement for the origin check.
