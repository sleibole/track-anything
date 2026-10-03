# Track Anything

A small web app for recording how often something happens: dog meals, a medication, a woods outing, a daily exercise. One tap logs the current time. History stays on the tracker so the count is easy to review later.

Trackers belong to a household. Anyone in the household can log. A share link lets someone log one tracker without an account, which covers a kitchen tablet or a sitter.

## What you can do

- Sign up with an email. A magic link logs you in. A password is optional and is set in Settings after the address is confirmed.
- Create a count tracker in your household. Give it a name, an icon, an optional accent, and a short log label such as **+ Ate**.
- Choose the summary line: how many times today, whether it happened today, or when it last happened.
- Log with one tap. Undo a recent entry, add an optional note, or mark today as none when nothing happened.
- Invite household members with a link, or turn on a share link for a single tracker.
- Archive a tracker and restore it later from the household page.

The app is one Go process and one SQLite database. Pages are server-rendered HTML. Forms work without JavaScript; HTMX updates the log control in place when it is available.

Charts, numeric and duration entries, and billing are not in this version. Product and implementation notes are in `docs/development/`.

## Requirements

- [Go 1.27](https://go.dev/dl/) or newer

## Run locally

```sh
make dev
```

That starts [Air](https://github.com/air-verse/air), which rebuilds the server when you save a Go file, an HTML template, or CSS or JavaScript under `static/`. Open http://localhost:8080 and refresh after a restart. Air does not reload the browser.

`make run` starts the server once, without reload.

With `SMTP_HOST` unset, login and verification links are written to the server log. To send mail locally, copy `.env.example` to `.env` and run [MailHog](https://github.com/mailhog/MailHog) (SMTP on port 1025, inbox at http://localhost:8025). A missing `.env` is fine. Variables already set in the environment are left alone.

The SQLite file is created at `data/trackanything.db`.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `ADDR` | `:8080` | Listen address. If `ADDR` is unset, `PORT` is used as `:$PORT`. |
| `DB_PATH` | `data/trackanything.db` | SQLite file. |
| `ENV` | `dev` | `dev` or `prod`. `dev` turns off the `Secure` cookie flag so `http://localhost` works. |
| `BASE_URL` | `http://localhost:8080` | Public origin used in emailed links. Required in production, and it must be `https`. |
| `SMTP_HOST` | empty | Empty logs links to the console. Set it to send mail. |
| `SMTP_PORT` | `587` | Implicit TLS on `465`; STARTTLS otherwise. Production refuses to send without TLS. |
| `SMTP_USER` | empty | SMTP username. |
| `SMTP_PASS` | empty | SMTP password. |
| `MAIL_FROM` | `Track Anything <hello@trackanything.io>` | From address. |
| `TRUSTED_IP_HEADER` | empty | Client IP header set by a trusted proxy: `Fly-Client-IP`, `CF-Connecting-IP`, or `X-Forwarded-For`. Empty uses the connection address. Production sets `Fly-Client-IP`. |
| `TURNSTILE_SITE_KEY` | test key in development | Public Turnstile widget key. Required in production. |
| `TURNSTILE_SECRET_KEY` | test key in development | Turnstile secret used for server-side verification. Required in production. Never log it. |

## Tests

```sh
make test
```

## Docker

```sh
make docker-run
```

Builds the image and runs it on port 8080 with a volume at `/data`. The image is a distroless static binary. Templates, static files, and time zone data are embedded.

## Documentation

`docs/development/` holds the product, design, and architecture decisions used while building the app:

- [`PLAN.md`](docs/development/PLAN.md) — what Track Anything is and what is still planned
- [`DESIGN.md`](docs/development/DESIGN.md) — how it should look and behave
- [`ARCHITECTURE.md`](docs/development/ARCHITECTURE.md) — how it is built, including deployment
