# Track Anything — Agent instructions

Follow the development documents when changing the product:

- `docs/development/PLAN.md` — what Track Anything is and what is still planned
- `docs/development/DESIGN.md` — how it should look and behave
- `docs/development/ARCHITECTURE.md` — how it is built

`PLAN.md` points here. User help, when it exists, belongs in `docs/help/`.

## Tests

Existing tests must not be modified, deleted, skipped, disabled, or have their assertions weakened without my explicit permission. If an existing test appears incorrect or conflicts with an intended behavior change, explain the issue and request permission before changing it. Adding new tests is allowed.

## Development servers

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
