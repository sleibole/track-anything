# Worktrees

Cursor can give each agent its own Git worktree. The worktree is a separate checkout of this repository. The main checkout stays untouched, and two agents do not edit the same files.

Named development servers still come from `make dev-agent`. In a worktree, that command uses the worktree's copy of `scripts/dev-server.sh`, so the server's database, Air build, and log stay under that worktree's `tmp/dev-agents/<name>/`.

## Setup

Cursor looks for `.cursor/worktrees.json` in the worktree and then in the main checkout. This repository sets `setup-worktree-unix` to `.cursor/setup-worktree-unix.sh`. On macOS and Linux that script runs in the new worktree before the agent edits files. The generic `setup-worktree` entry runs the same script with `bash`.

Cursor sets `ROOT_WORKTREE_PATH` to the main checkout while the script runs. The script does three things:

1. If this worktree already has a `.env` file, it leaves that file alone.
2. Otherwise it copies the main checkout's `.env` only when every set value is safe local development configuration. A missing `.env` is fine. When the main file is missing, is a symlink, or contains settings that must not be copied, the script copies `.env.example` instead. If that is not safe either, it prints setup instructions and does not create `.env`.
3. It runs `go mod download` when `go.mod` is present. Module download uses the shared Go module cache.

The script does not print secret values. When it refuses a file, it prints the setting names only.

These settings are not copied from the main checkout:

- `ENV` other than `dev`
- `SMTP_USER`, `SMTP_PASS`, and any name that looks like a secret, password, token, or credential
- `TURNSTILE_SITE_KEY` and `TURNSTILE_SECRET_KEY`
- `TRUSTED_IP_HEADER`
- `SMTP_HOST` other than `localhost`, `127.0.0.1`, or `::1`
- `BASE_URL` other than an `http://localhost` or `http://127.0.0.1` URL
- `DB_PATH` when it is absolute or contains `..`
- any absolute path or non-local URL

`make dev-agent` still sets `ADDR`, `PORT`, `DB_PATH`, and `BASE_URL` itself after the copy. A copied `.env` cannot point that server at port 8080 or at `data/trackanything.db`.

To see whether setup ran, open the Output panel and choose **Worktrees Setup**.

## Parallel development

Use one agent per worktree. Do all of that agent's edits in its checkout. Do not change the main checkout or another worktree.

From the worktree root:

```sh
make dev-list
make dev-agent NAME=<unique-name>
```

`make dev-list` shows servers recorded in this checkout only. Pick one name and keep using it. Names are unique per checkout; two worktrees may both use the same name. They still get different ports and different SQLite files.

The port comes from `DEV_SERVER_PORT_MIN` through `DEV_SERVER_PORT_MAX`, which default to 8100–8199. The script claims a port by creating a directory with `mkdir`. That claim is held until the server process exits, including while Air is still building, so two worktrees cannot reserve the same port. The claim directory is outside every checkout: `DEV_SERVER_PORT_CLAIM_DIR` when that is set, otherwise a directory under the system temp directory whose name comes from this repository's Git common directory. Other repositories do not use that directory.

A claim records an instance id, the worktree path, the server name, the supervisor pid, the process start time, the command line, and the process working directory. Cleanup removes a claim only when that recorded identity still belongs to the instance being stopped. A live process whose pid was reused, or whose command no longer matches, keeps its claim. `make dev-stop` in one worktree does not stop another worktree's server and does not delete that server's claim.

Do not run `make dev` in a worktree. That target is the personal server: port 8080, build output `tmp/main`, database `data/trackanything.db`. Do not start it, stop it, or point a worktree at its database.

Do not kill processes by port. Do not run `pkill`, `killall`, or any other broad match against `air`, `go`, or `main`.

## Server management

The command stays in the foreground and prints `dev-agent: ready` with the URL when `/healthz` answers.

Stop only the server this worktree started, and stop it when testing is done:

```sh
make dev-stop NAME=<unique-name>
```

`make dev-stop-all` stops named servers recorded in this checkout. It does not see the personal server or another worktree's `tmp/dev-agents/`.

Ctrl-C, SIGHUP, and `dev-stop` signal the recorded process only when its command line still matches that instance.

SQLite, the Air binary, and the log for a named server are under `tmp/dev-agents/<name>/` in the checkout that started it. Those directories are gitignored. The Go build cache and module cache stay shared. They are content-addressed and are not where the app stores data.

## Review and integration

Review the diff in the Agents Window, or in the worktree with `git diff` and `git status`. `git worktree list` shows every checkout.

When the result is wanted in the main checkout, the person working there uses `/apply-worktree` in the IDE, or commits from the worktree and opens a pull request. An agent does not merge, rebase, push, or apply the worktree unless the task asks for that.

Do not run destructive Git commands unless the task explicitly allows that command. That includes `git reset --hard`, `git clean`, force-push, and deleting branches.

## Cleanup

Finished work has to be saved before the worktree disappears. Commit it, push it, or apply it back to the main checkout. A worktree that only exists as uncommitted files can be deleted by cleanup.

Cursor 3.5 and later can delete old worktrees on a schedule. The machine settings are `cursor.worktreeCleanupIntervalHours` and `cursor.worktreeMaxCount`. The default cap is 25 worktrees per machine, across every workspace on that machine, and the newest ones are kept. Cleanup runs on that interval and also catches up after a restart when the last run is older than the interval. Creating a worktree past the cap starts a cleanup pass instead of waiting for the interval.

That pass looks up the worktree root again. Worktrees created outside Cursor's manager are included. A checkout made with `/worktree`, with `git worktree add`, or by another tool can be removed when it is among the older ones.

This repository does not change those machine settings and does not delete worktrees. Remove a worktree only when the person who owns it asks, after the work that should be kept has been committed or applied. `/delete-worktree` in the IDE and `git worktree remove` both delete the checkout.
