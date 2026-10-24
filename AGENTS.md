# Track Anything — Agent instructions

Track Anything is one Go process, server-rendered HTML, and one SQLite database. Forms use `POST` and work without JavaScript. HTMX updates the page in place when it is available.

**Policy** is mandatory. **Practice** is the default. Depart from a practice only when the task or the surrounding code gives a concrete reason.

## Product documents (policy)

Read the documents that cover the change before editing:

- `docs/development/PLAN.md` — what Track Anything is and what is still planned
- `docs/development/DESIGN.md` — how it should look and behave
- `docs/development/ARCHITECTURE.md` — how it is built

`PLAN.md` points here. When navigation, partials, or redirects change, also follow `docs/development/HTMX.md`. `docs/development/SUMMARY-DISPLAY.md` records a finished change. The behavior lives in the three documents above.

User help, when it exists, belongs in `docs/help/`. Keep developer notes in `docs/development/`.

Work marked not built, and later ideas in `PLAN.md`, is not current work. Leave it alone unless the task asks for it.

## Repository conventions (policy)

- The application is package `main` at the repo root. `scripts/` is a separate package. Keep that layout. Do not add `cmd/`, `internal/`, or a handler/service/repository split unless the task requires it.
- One process and one SQLite database. Do not add a job runner, a public API, a frontend framework, or a second charting library.
- Direct module dependencies are GORM, `github.com/glebarez/sqlite`, and `golang.org/x/crypto`. The Go version and toolchain are in `go.mod`. The Docker image matches that toolchain. Change them only when the task asks.
- Keep the three redirect behaviors in `HTMX.md` separate. A normal submit, an in-app HTMX navigation, and a response that must replace the document are different responses.
- Tracker access goes through the helpers in `access.go`. A tracker the caller cannot see is a 404.
- `openDB` uses one SQLite connection (`SetMaxOpenConns(1)`). Leave that in place.

## Working approach (practice)

- Inspect relevant code and documentation before editing.
- Make the smallest complete change that satisfies the request.
- Follow established patterns and keep unrelated cleanup out of scope.
- Prefer simple, readable Go and the standard library when appropriate.
- Avoid speculative abstractions, unnecessary dependencies, and architectural changes without a concrete need.
- Preserve unrelated user changes. Never discard or overwrite work you did not create.
- Ask for clarification when ambiguity materially affects behavior, data integrity, or architecture. Resolve routine implementation choices independently.

## Go practices (practice)

- Follow idiomatic Go naming, formatting, package organization, and error handling.
- Use gofmt on changed Go files.
- Keep packages cohesive and interfaces small. Introduce an interface where a consumer needs one, rather than for every implementation.
- Handle errors explicitly and wrap them with useful context when appropriate. Preserve error identity when callers need `errors.Is` or `errors.As`.
- Use `context.Context` for cancellation and deadlines where relevant. Pass it explicitly through request and I/O boundaries.
- Manage resource lifetimes carefully, including response bodies, files, database rows, transactions, timers, and goroutines.
- Give goroutines a clear owner and shutdown path. Avoid unnecessary concurrency and shared mutable state.
- Preserve existing API contracts and compatibility unless the task explicitly changes them. In this repository that includes routes, form fields, and the responses tests already cover.
- Follow the repository’s existing logging approach and never log secrets, credentials, session tokens, or sensitive user data.
- Validate untrusted input and use parameterized database queries.
- Keep dependency changes intentional and directly related to the task.

In this repository:

- Log with the `*slog.Logger` on `app`. Production writes JSON to stdout. Development writes text. `logPath` replaces bearer tokens in request paths with the route placeholder. Keep that. Do not log passwords, session cookies, magic-link or verification tokens, share or invite tokens, SMTP credentials, or the Turnstile secret.
- Handlers take `http.ResponseWriter` and `*http.Request`. Pass `r.Context()` into outbound HTTP and other cancelable I/O. Shutdown already uses `context.Context`. Do not add a context parameter to every helper.
- Wrap with `%w` when a caller uses `errors.Is` or `errors.As`, including `gorm.ErrRecordNotFound` and the existing sentinel errors.
- Query through GORM with placeholders. Do not build SQL from request data.
- The cleanup goroutine started from `main` stops on shutdown. A new goroutine needs an owner and the same kind of stop path.

## Existing tests (policy)

This is a strict project policy:

- Do not modify, delete, rename, skip, disable, weaken, or replace any existing test without my explicit permission.
- This includes assertions, expected values, snapshots, golden files, fixtures, test helpers, test configuration, and CI settings when changes would alter or bypass existing test coverage.
- A feature request or application behavior change does not implicitly authorize changing existing tests.
- Do not treat an existing test failure as permission to update the test.
- New tests may be added when useful, but must not replace or circumvent existing coverage.

When an existing test fails:

1. Report the failing test, command, and relevant failure output.
2. Determine whether it failed before your changes when practical, using a safe baseline comparison that preserves unrelated work.
3. If your implementation introduced an unintended regression, fix the implementation and rerun the relevant tests without changing the existing test.
4. If the requested behavior conflicts with an existing test, or the test itself appears incorrect, explain the conflict and request explicit permission before changing it. Describe the exact proposed test change.
5. If the failure is unrelated, pre-existing, flaky, or caused by the environment, report that finding and the supporting evidence. Do not silently repair or bypass it.
6. If the cause cannot be established, say so. Do not label a failure pre-existing without evidence.

Continue independent work when possible, but do not claim completion or passing validation while relevant failures remain unresolved.

New tests use the standard library (`testing`, `net/http/httptest`) and the helpers in `main_test.go`: `newTestApp` and `memMailer`. Each test gets its own database. Tests do not load `.env`. Do not point a test at `data/trackanything.db` or a development-server database.

## Development servers (policy)

`make dev` is the personal server on port 8080. Do not start it, stop it, or kill whatever is listening there.

Run `make dev-list` before starting a server. If your name is already listed as running, use that URL. Do not start another server.

If you have no running server, start one from the repo root:

```sh
make dev-agent NAME=<unique-name>
```

Pick one name and keep using it. If that command says the name is already running, use the URL it prints.

The command stays in the foreground. It prints `dev-agent: ready` with the URL when the server is accepting requests. Use that URL for browser checks.

Stop only the server you started, and stop it when testing is done:

```sh
make dev-stop NAME=<unique-name>
```

Do not kill processes by port. Do not run `pkill`, `killall`, or any other broad match against `air`, `go`, or `main`. `make dev-stop-all` stops every named agent server and does not stop the personal server; use it only for names you started.

These servers share the working tree. Each one has its own port (from 8100 upward), Air build directory, and SQLite file.

## Validation (practice)

- The documented check is `make test`, which runs `go test ./...`. This repository has no CI workflow. Do not add one unless the task asks.
- Run the tests that cover the change during development. Run `make test` before treating the work as done.
- Run gofmt on changed Go files, and `go vet ./...`, before completion.
- Run `go test -race ./...` when the change adds or alters goroutines, shared mutable state, or shutdown.
- Add a test for new behavior and for a bug fix when it pins down a real case. Exercise the behavior a caller can observe. Skip tests that only mirror an implementation detail.
- Do not run destructive checks. Do not use production services or production data. Leave `data/trackanything.db` and the personal server on port 8080 alone.
- For a change someone can see in the browser, exercise that flow on the agent server from Development servers. `make test` does not do that. A single screenshot is not enough.
- If a check cannot run, report the command and the reason. Do not imply it passed.
- Do not alter dependencies, toolchain versions, or validation settings just to make checks pass.

## Completion report (practice)

When the task is finished, briefly summarize:

- What changed and why.
- Which checks ran and their results.
- Any existing test failures, unresolved issues, or decisions requiring my input.

Do not claim the work is complete while a relevant failure is still unresolved.
