# CSRF

Every request that can change data must prove it came from a page this app rendered for this browser. Two checks run, in order. Neither one is optional, and neither one replaces the other.

1. `http.CrossOriginProtection` rejects a cross-site `Sec-Fetch-Site` and an `Origin` whose host is not this request's `Host`.
2. `csrfProtect` requires the CSRF secret for the current browser context.

`GET`, `HEAD`, and `OPTIONS` skip the secret check. Every other method needs it, including a route that is public, anonymous, or reached with a share link. There is no exemption list.

The secret is not a login. It does not authorize the action. Tracker access, household role, and share tokens still decide that.

## Token lifecycle

The secret is 32 random bytes, base64url-encoded. Comparison hashes both sides and uses `subtle.ConstantTimeCompare`. A missing, empty, malformed, or mismatched value is `403` with the body `Forbidden`. The handler does not run. The log line is `csrf rejected` plus the method and the redacted path. The secret itself is never logged.

The secret stays the same until the security context changes:

- A signed-in session, including a guest, stores `CSRFSecret` on the session row when the session is created. It is not the session cookie and it is not useful without that cookie. Renewal extends the expiry and does not rotate the secret, so another tab, the back button, and a second submit of the same form keep working.
- Signing up, logging in, opening a magic link, or confirming an email creates a new session and a new secret. The old secret stops working. Those handlers redirect; the next page is rendered with the new secret. Do not render a form in the same response that calls `startSession`.
- Logout deletes the session. An expired session is deleted on the next request. Either way the old secret is rejected.
- A signed-out browser gets an `HttpOnly`, `SameSite=Lax`, host-only cookie named `csrf` (`Secure` when `ENV` is not `dev`, path `/`, 30 days). The page and the cookie carry the same value. A safe request mints it when it is missing. An unsafe request must already have it. That is one page load, not an extra round trip.
- A session secret wins over the `csrf` cookie. Logging in does not reuse the signed-out value.

A request that omits both `Origin` and `Sec-Fetch-Site` still needs the secret. That is the case Go's origin check allows through. A valid secret does not override a cross-site `Sec-Fetch-Site` or a foreign `Origin`.

## Add a form

The layout renders the secret into the page. Handlers do not put it on their view models. Nested cards and entries receive it when the page is rendered.

```html
<form method="post" action="/example" hx-post="/example">
  {{template "csrf-field" .}}
  <button type="submit">Save</button>
</form>
```

`.` must be the page view, a tracker card, or an entry. Inside `{{with}}`, `.` is something else and `$` is still that parent. Pass `$`:

```html
{{with .Data}}
<form method="post" action="/example">
  {{template "csrf-field" $}}
  <button type="submit">Save</button>
</form>
{{end}}
```

The partial emits one hidden input, `name="csrf_token"`. The value is escaped. Do not write that input by hand, and do not invent another field name.

A form that works without JavaScript is enough. The hidden field is what a normal submit sends.

## HTMX

`templates/layout.html` puts the secret in `<meta name="csrf-token">` in `<head>`. `static/app.js` registers one `htmx:configRequest` listener. For a same-origin `POST`, `PUT`, `PATCH`, or `DELETE`, it sets `X-CSRF-Token` from that meta tag. `GET` and `HEAD` are left alone. A URL on another origin gets nothing.

`#content` is what HTMX swaps. The meta tag is not inside it, and the secret does not change on a partial render, so a swap or a history restore keeps working. The hidden field is still rendered in the fragment, for a browser with JavaScript off and for a form that is submitted as a normal POST.

Do not set `hx-headers` on a button or a form. A `403` is the response for a bad or missing secret. The body is `Forbidden`. The handler did not run.

## fetch

Reorder is the `fetch` that changes data. Home order and order within a personal group both call the same helper:

```javascript
const headers = Object.assign(
  { "Content-Type": "application/x-www-form-urlencoded" },
  window.trackCSRF.headers(url)
);
fetch(url, { method: "POST", credentials: "same-origin", headers, body });
```

`window.trackCSRF.headers(url)` returns `{ "X-CSRF-Token": token }` for a same-origin URL and `{}` otherwise. Read the token at request time from the meta tag. Do not cache it in a closure that survives a new session. Do not retry a failed POST automatically.

A new same-origin `fetch` that changes data uses that helper. A request to another origin does not.

## Anonymous browsers and guests

No email and no account are required. `POST /start`, signup, login, a magic link, email confirmation, and a share link all require the secret.

`GET /` for a signed-out visitor sets the short-lived `start` cookie and does not create a user. The CSRF cookie is set on that same response. `POST /start` presents it, then creates the guest and a new session secret. Login and confirmation do the same: the POST is checked against the signed-out cookie, and the new session gets its own secret.

Share links authorize by the token in the path. They still require the CSRF secret, so another site cannot drive a visitor's browser at a link that visitor has open. There is no exemption for them.

## What is not CSRF

The secret answers "did this browser load our page?" It does not answer "should this action happen only once?"

It is not consumed. Two tabs can submit it. The back button can submit it. A double click can submit it twice, and both requests are allowed through this check.

Once-only behavior stays on the action:

- A magic link and a verification link are one row updated with a conditional `WHERE used_at IS NULL`.
- Redeeming a reward carries its own `request` key.
- Creating a tracker from the new-tracker form carries its own `request` key.
- Uniqueness in the database covers the cases that need a single row.

Do not delete or rotate the CSRF secret to stop a double submit.

## Tests

`go test -count=1 -run 'CSRF|CrossOrigin|TurnstileDoesNotBypass' .`

`make test` runs the whole suite. The browser test client sends `X-CSRF-Token` for unsafe methods. That is the same secret the server expects. It is not a bypass. A test that must show rejection uses a raw request and does not go through that client.

## Mistakes

- A new `<form method="post">` without `csrf-field`. The template test fails when this happens.
- Writing `<input name="csrf_token">` in a template instead of the partial.
- `hx-headers` on one control. It goes stale, and the global listener already covers HTMX.
- Treating a signed-out route, a share link, or `/api` as exempt. Nothing is exempt.
- Consuming the secret after a successful POST.
- Rendering a form in the response that just called `startSession`. Redirect, and let the next request render.
- Logging the secret, the `csrf` cookie, or the `X-CSRF-Token` header.
- Sending the header to another origin.
