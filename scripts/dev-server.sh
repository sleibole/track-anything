#!/bin/bash
# Named Air dev servers for concurrent agents.
# make dev (port 8080, tmp/main, data/trackanything.db) is not managed here.
#
# Each instance gets its own port, Air build directory, and SQLite file.
# Shutdown only signals processes whose command line still matches this
# instance. A reused pid or an unrelated listener is left alone.
#
# Port claims are mkdir directories under DEV_SERVER_PORT_CLAIM_DIR, shared
# by every worktree of this repository. A claim is held until that instance
# exits. Cleanup removes a claim only when its recorded identity matches the
# instance being stopped.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

# Tests override the state directory, port range, ready timeout, claim
# directory, and the command that is launched. DEV_SERVER_RELOAD_DIR, when
# set to an absolute path, adds an Air include_dir so reload can be checked
# without editing project files. Agents leave that unset.
STATE_DIR=${DEV_SERVER_STATE_DIR:-$ROOT/tmp/dev-agents}
PORT_MIN=${DEV_SERVER_PORT_MIN:-8100}
PORT_MAX=${DEV_SERVER_PORT_MAX:-8199}
READY_TIMEOUT=${DEV_SERVER_READY_TIMEOUT:-60}
AIR_PKG=github.com/air-verse/air@v1.67.4
LOCK_DIR=""
LOCK_HELD=0
PORT_CLAIM_DIR=""
CLAIM_LOCK_DIR=""
CLAIM_LOCK_HELD=0
CLAIM_ID=""
CLAIM_OWNED=0

NAME=""
APP_PORT=""
URL=""
INSTANCE_DIR=""
INSTANCE_FILE=""
INSTANCE_CONFIG=""
INSTANCE_BINARY=""
LOG_FILE=""
CHILD_PID=""
SERVER_PID=""
TAIL_PID=""
CLEANED=0
OWN_INSTANCE=0

usage() {
  echo "usage: dev-server.sh start NAME | stop NAME | list | stop-all" >&2
  exit 1
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "dev-server: $1 is required" >&2
    exit 1
  fi
}

require_number() {
  local label=$1 value=$2
  case "$value" in
    ''|*[!0-9]*)
      echo "dev-server: $label must be a number, got ${value:-empty}" >&2
      exit 1
      ;;
  esac
}

validate_name() {
  local name=$1
  if [ -z "$name" ]; then
    echo "dev-server: NAME is required" >&2
    echo "usage: make dev-agent NAME=<unique-name>" >&2
    exit 1
  fi
  case "$name" in
    *[!A-Za-z0-9_-]*)
      echo "dev-server: NAME must contain only letters, digits, hyphens, and underscores" >&2
      exit 1
      ;;
  esac
  case "$name" in
    [A-Za-z0-9]*) ;;
    *)
      echo "dev-server: NAME must start with a letter or digit" >&2
      exit 1
      ;;
  esac
  if [ "${#name}" -gt 32 ]; then
    echo "dev-server: NAME must be 32 characters or fewer" >&2
    exit 1
  fi
}

prepare_state() {
  require_number PORT_MIN "$PORT_MIN"
  require_number PORT_MAX "$PORT_MAX"
  require_number READY_TIMEOUT "$READY_TIMEOUT"
  if [ "$PORT_MIN" -gt "$PORT_MAX" ]; then
    echo "dev-server: PORT_MIN ($PORT_MIN) is greater than PORT_MAX ($PORT_MAX)" >&2
    exit 1
  fi
  if [ "$READY_TIMEOUT" -lt 1 ]; then
    echo "dev-server: READY_TIMEOUT must be at least 1" >&2
    exit 1
  fi
  mkdir -p "$STATE_DIR"
  STATE_DIR=$(cd "$STATE_DIR" && pwd)
  LOCK_DIR=$STATE_DIR/.lock
  prepare_claims
}

# Claims live outside any checkout so one worktree can see another's ports
# without writing into that worktree. The default directory is per repository
# because it is named from the git common directory.
prepare_claims() {
  local common key dir
  if [ -n "${DEV_SERVER_PORT_CLAIM_DIR:-}" ]; then
    dir=$DEV_SERVER_PORT_CLAIM_DIR
  else
    common=$(git -C "$ROOT" rev-parse --git-common-dir 2>/dev/null || true)
    if [ -z "$common" ]; then
      common=$ROOT
    else
      case "$common" in
        /*) ;;
        *) common=$ROOT/$common ;;
      esac
    fi
    if [ -d "$common" ]; then
      common=$(cd "$common" && pwd)
    fi
    key=$(printf '%s' "$common" | cksum | awk '{print $1}')
    dir=${TMPDIR:-/tmp}/dev-server-ports-$key
  fi
  mkdir -p "$dir"
  PORT_CLAIM_DIR=$(cd "$dir" && pwd)
  CLAIM_LOCK_DIR=$PORT_CLAIM_DIR/.lock
}

trim_ws() {
  local s=$1
  s=${s#"${s%%[![:space:]]*}"}
  s=${s%"${s##*[![:space:]]}"}
  printf '%s\n' "$s"
}

process_lstart() {
  local raw
  raw=$(ps -p "$1" -o lstart= 2>/dev/null || true)
  trim_ws "$raw"
}

process_cwd() {
  local line
  line=$(lsof -a -p "$1" -d cwd -Fn 2>/dev/null | awk '/^n/ { sub(/^n/, ""); print; exit }' || true)
  trim_ws "$line"
}

read_claim_field() {
  local dir=$1 key=$2 line
  [ -f "$dir/$key" ] || return 1
  IFS= read -r line < "$dir/$key" || return 1
  [ -n "$line" ] || return 1
  printf '%s\n' "$line"
}

new_instance_id() {
  od -An -N16 -tx1 /dev/urandom | tr -d '[:space:]'
}

# live: pid is alive and the start time, command, and cwd still match.
# stale: identity is complete and that pid is not alive.
# ambiguous: anything else, including a reused pid. Callers must not delete it.
classify_claim() {
  local dir=$1 id worktree name pid stored_lstart stored_cmd stored_cwd
  local now_lstart now_cmd now_cwd
  id=$(read_claim_field "$dir" instance_id || true)
  worktree=$(read_claim_field "$dir" worktree || true)
  name=$(read_claim_field "$dir" name || true)
  pid=$(read_claim_field "$dir" pid || true)
  stored_lstart=$(read_claim_field "$dir" lstart || true)
  stored_cmd=$(read_claim_field "$dir" command || true)
  stored_cwd=$(read_claim_field "$dir" process_cwd || true)
  if [ -z "$id" ] || [ -z "$worktree" ] || [ -z "$name" ] || [ -z "$pid" ] || \
    [ -z "$stored_lstart" ] || [ -z "$stored_cmd" ] || [ -z "$stored_cwd" ]; then
    printf 'ambiguous\n'
    return 0
  fi
  case "$pid" in
    ''|*[!0-9]*)
      printf 'ambiguous\n'
      return 0
      ;;
  esac
  if ! kill -0 "$pid" 2>/dev/null; then
    printf 'stale\n'
    return 0
  fi
  now_lstart=$(process_lstart "$pid")
  now_cmd=$(command_of "$pid")
  now_cwd=$(process_cwd "$pid")
  if [ -z "$now_lstart" ] || [ -z "$now_cmd" ] || [ -z "$now_cwd" ]; then
    printf 'ambiguous\n'
    return 0
  fi
  if [ "$now_lstart" != "$stored_lstart" ] || [ "$now_cmd" != "$stored_cmd" ] || \
    [ "$now_cwd" != "$stored_cwd" ]; then
    printf 'ambiguous\n'
    return 0
  fi
  printf 'live\n'
}

write_claim() {
  local dir=$1 id lstart cmd cwd
  id=$(new_instance_id)
  lstart=$(process_lstart $$)
  cmd=$(command_of $$)
  cwd=$(process_cwd $$)
  if [ -z "$id" ] || [ -z "$lstart" ] || [ -z "$cmd" ] || [ -z "$cwd" ]; then
    echo "dev-server: could not record port-claim identity" >&2
    return 1
  fi
  printf '%s\n' "$id" > "$dir/instance_id"
  printf '%s\n' "$ROOT" > "$dir/worktree"
  printf '%s\n' "$NAME" > "$dir/name"
  printf '%s\n' "$$" > "$dir/pid"
  printf '%s\n' "$lstart" > "$dir/lstart"
  printf '%s\n' "$cmd" > "$dir/command"
  printf '%s\n' "$cwd" > "$dir/process_cwd"
  CLAIM_ID=$id
  return 0
}

# Move the claim aside only when its instance id still matches. mkdir of the
# replacement is the caller's next step. mv is atomic on one filesystem.
move_claim_aside() {
  local dir=$1 expect=$2 moved got
  [ -d "$dir" ] || return 1
  got=$(read_claim_field "$dir" instance_id || true)
  [ "$got" = "$expect" ] || return 1
  moved=$(mktemp -d "${PORT_CLAIM_DIR}/.released.XXXXXX")
  if mv "$dir" "$moved/claim" 2>/dev/null; then
    rm -rf "$moved"
    return 0
  fi
  rmdir "$moved" 2>/dev/null || true
  return 1
}

reclaim_stale_claim() {
  local dir=$1 moved
  [ -d "$dir" ] || return 0
  moved=$(mktemp -d "${PORT_CLAIM_DIR}/.stale.XXXXXX")
  if mv "$dir" "$moved/claim" 2>/dev/null; then
    rm -rf "$moved"
    return 0
  fi
  rmdir "$moved" 2>/dev/null || true
  return 1
}

claim_is_self() {
  local dir=$1 id pid worktree name stored_lstart stored_cmd
  [ -d "$dir" ] || return 1
  id=$(read_claim_field "$dir" instance_id || true)
  pid=$(read_claim_field "$dir" pid || true)
  worktree=$(read_claim_field "$dir" worktree || true)
  name=$(read_claim_field "$dir" name || true)
  stored_lstart=$(read_claim_field "$dir" lstart || true)
  stored_cmd=$(read_claim_field "$dir" command || true)
  [ "$id" = "$CLAIM_ID" ] || return 1
  [ "$pid" = "$$" ] || return 1
  [ "$worktree" = "$ROOT" ] || return 1
  [ "$name" = "$NAME" ] || return 1
  [ -n "$stored_lstart" ] && [ "$stored_lstart" = "$(process_lstart $$)" ] || return 1
  [ -n "$stored_cmd" ] && [ "$stored_cmd" = "$(command_of $$)" ] || return 1
}

# stop runs in a different process from the supervisor. Delete the claim only
# when the recorded instance id, pid, worktree, and name all match, and the
# live process fingerprint still matches when that pid is alive.
release_stopped_claim() {
  local port=$1 expect_id=$2 expect_pid=$3 expect_name=$4 expect_worktree=$5
  local dir got_id got_pid got_name got_worktree
  local stored_lstart stored_cmd stored_cwd now_lstart now_cmd now_cwd
  dir=$PORT_CLAIM_DIR/port-$port
  [ -d "$dir" ] || return 0
  got_id=$(read_claim_field "$dir" instance_id || true)
  got_pid=$(read_claim_field "$dir" pid || true)
  got_name=$(read_claim_field "$dir" name || true)
  got_worktree=$(read_claim_field "$dir" worktree || true)
  stored_lstart=$(read_claim_field "$dir" lstart || true)
  stored_cmd=$(read_claim_field "$dir" command || true)
  stored_cwd=$(read_claim_field "$dir" process_cwd || true)
  [ "$got_id" = "$expect_id" ] || return 0
  [ "$got_pid" = "$expect_pid" ] || return 0
  [ "$got_name" = "$expect_name" ] || return 0
  [ "$got_worktree" = "$expect_worktree" ] || return 0
  [ -n "$stored_lstart" ] && [ -n "$stored_cmd" ] && [ -n "$stored_cwd" ] || return 0
  if kill -0 "$got_pid" 2>/dev/null; then
    now_lstart=$(process_lstart "$got_pid")
    now_cmd=$(command_of "$got_pid")
    now_cwd=$(process_cwd "$got_pid")
    if [ "$now_lstart" != "$stored_lstart" ] || [ "$now_cmd" != "$stored_cmd" ] || \
      [ "$now_cwd" != "$stored_cwd" ]; then
      echo "dev-server: leaving the claim for port $port; process identity does not match" >&2
      return 0
    fi
  fi
  move_claim_aside "$dir" "$expect_id" || true
}

acquire_claim_lock() {
  local i=0 holder
  while true; do
    if mkdir "$CLAIM_LOCK_DIR" 2>/dev/null; then
      echo $$ > "$CLAIM_LOCK_DIR/pid"
      CLAIM_LOCK_HELD=1
      return 0
    fi
    holder=$(cat "$CLAIM_LOCK_DIR/pid" 2>/dev/null || true)
    if [ -z "$holder" ]; then
      sleep 0.1
    elif ! kill -0 "$holder" 2>/dev/null; then
      rm -rf "$CLAIM_LOCK_DIR"
      continue
    fi
    i=$((i + 1))
    if [ "$i" -ge 100 ]; then
      echo "dev-server: timed out waiting for $CLAIM_LOCK_DIR" >&2
      exit 1
    fi
    sleep 0.1
  done
}

release_claim_lock() {
  local holder
  [ "$CLAIM_LOCK_HELD" = 1 ] || return 0
  holder=$(cat "$CLAIM_LOCK_DIR/pid" 2>/dev/null || true)
  if [ "$holder" = "$$" ]; then
    rm -rf "$CLAIM_LOCK_DIR"
  fi
  CLAIM_LOCK_HELD=0
}

release_self_claim() {
  local dir grabbed=0
  [ "$CLAIM_OWNED" = 1 ] || return 0
  [ -n "$APP_PORT" ] && [ -n "$CLAIM_ID" ] && [ -n "$PORT_CLAIM_DIR" ] || return 0
  if [ "$CLAIM_LOCK_HELD" != 1 ]; then
    acquire_claim_lock
    grabbed=1
  fi
  dir=$PORT_CLAIM_DIR/port-$APP_PORT
  if claim_is_self "$dir"; then
    move_claim_aside "$dir" "$CLAIM_ID" || true
  fi
  CLAIM_OWNED=0
  if [ "$grabbed" = 1 ]; then
    release_claim_lock
  fi
}

# Caller holds the claim lock. mkdir is the reservation and it is kept after
# the lock is released, through server startup, until this instance exits.
try_claim() {
  local port=$1 dir state
  if ! port_available "$port"; then
    return 1
  fi
  dir=$PORT_CLAIM_DIR/port-$port
  if [ -e "$dir" ]; then
    if [ ! -d "$dir" ]; then
      return 1
    fi
    state=$(classify_claim "$dir")
    case "$state" in
      stale)
        reclaim_stale_claim "$dir" || return 1
        ;;
      *)
        return 1
        ;;
    esac
  fi
  if ! mkdir "$dir" 2>/dev/null; then
    return 1
  fi
  if ! write_claim "$dir"; then
    rm -rf "$dir"
    return 1
  fi
  if ! port_available "$port"; then
    move_claim_aside "$dir" "$CLAIM_ID" || true
    return 1
  fi
  APP_PORT=$port
  CLAIM_OWNED=1
  return 0
}

claim_free_port() {
  local preferred="" port
  if [ -f "$INSTANCE_DIR/port" ]; then
    preferred=$(tr -d '[:space:]' < "$INSTANCE_DIR/port" || true)
  fi
  case "$preferred" in
    ''|*[!0-9]*) preferred="" ;;
  esac
  if [ -n "$preferred" ] && try_claim "$preferred"; then
    return 0
  fi
  port=$PORT_MIN
  while [ "$port" -le "$PORT_MAX" ]; do
    if [ "$port" != "$preferred" ] && try_claim "$port"; then
      return 0
    fi
    port=$((port + 1))
  done
  return 1
}

field() {
  local file=$1 key=$2 line
  [ -f "$file" ] || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "$key"=*)
        printf '%s\n' "${line#"$key"=}"
        return 0
        ;;
    esac
  done < "$file"
  return 1
}

write_instance() {
  local tmp
  tmp=$(mktemp "${INSTANCE_FILE}.tmp.XXXXXX")
  cat > "$tmp"
  mv "$tmp" "$INSTANCE_FILE"
}

publish_instance() {
  local status=$1
  write_instance <<EOF
name=$NAME
port=$APP_PORT
url=$URL
status=$status
supervisor_pid=$$
go_pid=${CHILD_PID:-}
server_pid=${SERVER_PID:-}
config=$INSTANCE_CONFIG
binary=$INSTANCE_BINARY
log=$LOG_FILE
claim_id=${CLAIM_ID:-}
claim_port=${APP_PORT:-}
worktree=$ROOT
EOF
}

command_of() {
  ps -ww -p "$1" -o command= 2>/dev/null || true
}

command_contains() {
  local pid=$1 needle=$2 cmd
  [ -n "$pid" ] && [ -n "$needle" ] || return 1
  cmd=$(command_of "$pid")
  case "$cmd" in
    *"$needle"*) return 0 ;;
  esac
  return 1
}

command_starts_with() {
  local pid=$1 prefix=$2 cmd
  [ -n "$pid" ] && [ -n "$prefix" ] || return 1
  cmd=$(command_of "$pid")
  case "$cmd" in
    "$prefix"|"$prefix "*) return 0 ;;
  esac
  return 1
}

owns_supervisor() {
  local pid=$1 name=$2 cmd
  cmd=$(command_of "$pid")
  case "$cmd" in
    *dev-server.sh\ start\ "$name"|\
    *dev-server.sh\ start\ "$name"\ *)
      return 0
      ;;
  esac
  return 1
}

descendant_pids() {
  local pid=$1 child
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    printf '%s\n' "$child"
    descendant_pids "$child"
  done
}

is_descendant() {
  local pid=$1 ancestor=$2 hops=0 ppid
  while [ -n "$pid" ] && [ "$hops" -lt 30 ]; do
    [ "$pid" = "$ancestor" ] && return 0
    [ "$pid" = "1" ] || [ "$pid" = "0" ] && return 1
    ppid=$(ps -p "$pid" -o ppid= 2>/dev/null | tr -d '[:space:]' || true)
    [ -n "$ppid" ] && [ "$ppid" != "$pid" ] || return 1
    pid=$ppid
    hops=$((hops + 1))
  done
  return 1
}

listening_pids() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN -t 2>/dev/null || true
}

port_listening() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

# A live supervisor that still looks like this script is holding the port.
port_reserved() {
  local port=$1 dir inst other sup name
  for dir in "$STATE_DIR"/*; do
    [ -d "$dir" ] || continue
    inst=$dir/instance.env
    [ -f "$inst" ] || continue
    other=$(field "$inst" port || true)
    [ "$other" = "$port" ] || continue
    name=$(basename "$dir")
    [ "$name" = "$NAME" ] && continue
    sup=$(field "$inst" supervisor_pid || true)
    if [ -n "$sup" ] && kill -0 "$sup" 2>/dev/null && owns_supervisor "$sup" "$name"; then
      return 0
    fi
  done
  return 1
}

port_available() {
  local port=$1
  if [ "$port" -lt "$PORT_MIN" ] || [ "$port" -gt "$PORT_MAX" ]; then
    return 1
  fi
  if port_reserved "$port"; then
    return 1
  fi
  if port_listening "$port"; then
    return 1
  fi
  return 0
}

acquire_lock() {
  local i=0 holder
  while true; do
    if mkdir "$LOCK_DIR" 2>/dev/null; then
      echo $$ > "$LOCK_DIR/pid"
      LOCK_HELD=1
      return 0
    fi
    holder=$(cat "$LOCK_DIR/pid" 2>/dev/null || true)
    if [ -z "$holder" ]; then
      sleep 0.1
    elif ! kill -0 "$holder" 2>/dev/null; then
      rm -rf "$LOCK_DIR"
      continue
    fi
    i=$((i + 1))
    if [ "$i" -ge 100 ]; then
      echo "dev-server: timed out waiting for $LOCK_DIR" >&2
      exit 1
    fi
    sleep 0.1
  done
}

release_lock() {
  local holder
  [ "$LOCK_HELD" = 1 ] || return 0
  holder=$(cat "$LOCK_DIR/pid" 2>/dev/null || true)
  if [ "$holder" = "$$" ]; then
    rm -rf "$LOCK_DIR"
  fi
  LOCK_HELD=0
}

# Signal only the recorded tree. Before SIGKILL, require the command line
# to be unchanged so a pid reused during the wait is not killed.
stop_recorded() {
  local go_pid=$1 server_pid=$2 config=$3 binary=$4
  local pid cmd snap pid_list="" i alive saved
  [ -n "$config$binary" ] || return 0

  if [ -n "$go_pid" ] && command_contains "$go_pid" "$config"; then
    pid_list="$go_pid $(descendant_pids "$go_pid" | tr '\n' ' ')"
  fi
  if [ -n "$server_pid" ] && command_starts_with "$server_pid" "$binary"; then
    case " $pid_list " in
      *" $server_pid "*) ;;
      *) pid_list="$pid_list $server_pid $(descendant_pids "$server_pid" | tr '\n' ' ')" ;;
    esac
  fi

  if [ -z "$(printf '%s' "$pid_list" | tr -d '[:space:]')" ]; then
    return 0
  fi

  snap=$(mktemp -d "${TMPDIR:-/tmp}/dev-server-snap.XXXXXX")
  for pid in $pid_list; do
    [ -n "$pid" ] || continue
    cmd=$(command_of "$pid")
    [ -n "$cmd" ] || continue
    printf '%s' "$cmd" > "$snap/$pid"
    kill -TERM "$pid" 2>/dev/null || true
  done

  i=0
  while [ "$i" -lt 5 ]; do
    alive=0
    for pid in $pid_list; do
      [ -f "$snap/$pid" ] || continue
      if kill -0 "$pid" 2>/dev/null; then
        alive=1
      fi
    done
    [ "$alive" = 0 ] && break
    sleep 1
    i=$((i + 1))
  done

  for pid in $pid_list; do
    [ -f "$snap/$pid" ] || continue
    if ! kill -0 "$pid" 2>/dev/null; then
      continue
    fi
    cmd=$(command_of "$pid")
    saved=$(cat "$snap/$pid")
    if [ "$cmd" = "$saved" ]; then
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$snap"
  return 0
}

kill_if_child() {
  local pid=$1 ppid
  [ -n "$pid" ] || return 0
  ppid=$(ps -p "$pid" -o ppid= 2>/dev/null | tr -d '[:space:]' || true)
  if [ "$ppid" = "$$" ]; then
    kill "$pid" 2>/dev/null || true
  fi
}

show_log() {
  echo "dev-agent: log ($LOG_FILE):" >&2
  if [ -f "$LOG_FILE" ] && [ -s "$LOG_FILE" ]; then
    tail -n 50 "$LOG_FILE" >&2 || true
  else
    echo "(no output)" >&2
  fi
}

cleanup() {
  [ "$CLEANED" = 1 ] && return 0
  CLEANED=1
  # Drop our claim before the locks, including when startup fails before
  # OWN_INSTANCE is set. A second start of a name that is already running
  # has CLAIM_OWNED=0 and must not delete the live instance below.
  release_self_claim
  release_claim_lock
  release_lock
  kill_if_child "${TAIL_PID:-}"
  [ "$OWN_INSTANCE" = 1 ] || return 0
  stop_recorded "${CHILD_PID:-}" "${SERVER_PID:-}" "${INSTANCE_CONFIG:-}" "${INSTANCE_BINARY:-}" || true
  if [ -n "${INSTANCE_FILE:-}" ]; then
    rm -f "$INSTANCE_FILE" || true
  fi
  if [ -n "${INSTANCE_DIR:-}" ]; then
    rm -rf "$INSTANCE_DIR/air" || true
  fi
}

render_air_config() {
  local dest=$1 binary=$2 tmp_dir=$3
  local line found_tmp=0 found_cmd=0 found_ep=0 reload=""
  if [ ! -f "$ROOT/.air.toml" ]; then
    echo "dev-server: missing $ROOT/.air.toml" >&2
    return 1
  fi
  if [ -n "${DEV_SERVER_RELOAD_DIR:-}" ]; then
    case "$DEV_SERVER_RELOAD_DIR" in
      /*) ;;
      *)
        echo "dev-server: DEV_SERVER_RELOAD_DIR must be an absolute path" >&2
        return 1
        ;;
    esac
    case "$DEV_SERVER_RELOAD_DIR" in
      *\"*|*\\*)
        echo "dev-server: DEV_SERVER_RELOAD_DIR must not contain quotes or backslashes" >&2
        return 1
        ;;
    esac
    reload=$DEV_SERVER_RELOAD_DIR
    mkdir -p "$reload"
  fi
  : > "$dest"
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      'tmp_dir = "'*)
        printf 'tmp_dir = "%s"\n' "$tmp_dir" >> "$dest"
        found_tmp=1
        ;;
      'cmd = "'*)
        printf '%s\n' "$(printf '%s\n' "$line" | sed "s|-o [^ ]*|-o '$binary'|")" >> "$dest"
        found_cmd=1
        ;;
      'entrypoint = ['*)
        printf 'entrypoint = ["%s"]\n' "$binary" >> "$dest"
        found_ep=1
        ;;
      '[build]')
        printf '%s\n' "$line" >> "$dest"
        if [ -n "$reload" ]; then
          printf 'include_dir = ["%s"]\n' "$reload" >> "$dest"
        fi
        ;;
      *)
        printf '%s\n' "$line" >> "$dest"
        ;;
    esac
  done < "$ROOT/.air.toml"
  if [ "$found_tmp" != 1 ] || [ "$found_cmd" != 1 ] || [ "$found_ep" != 1 ]; then
    echo "dev-server: .air.toml is missing tmp_dir, cmd, or entrypoint" >&2
    return 1
  fi
}

foreign_listener() {
  local port=$1 ancestor=$2 pid
  for pid in $(listening_pids "$port"); do
    [ -n "$pid" ] || continue
    if ! is_descendant "$pid" "$ancestor"; then
      printf '%s\n' "$pid"
      return 0
    fi
  done
  return 1
}

health_ok() {
  curl -fsS --max-time 1 "http://127.0.0.1:$1/healthz" >/dev/null 2>&1
}

first_owned_listener() {
  local port=$1 ancestor=$2 pid
  for pid in $(listening_pids "$port"); do
    [ -n "$pid" ] || continue
    if is_descendant "$pid" "$ancestor"; then
      printf '%s\n' "$pid"
      return 0
    fi
  done
  return 1
}

recover_stale() {
  local inst=$1 go_pid server_pid config binary sup
  sup=$(field "$inst" supervisor_pid || true)
  if [ -n "$sup" ] && kill -0 "$sup" 2>/dev/null && owns_supervisor "$sup" "$NAME"; then
    URL=$(field "$inst" url)
    APP_PORT=$(field "$inst" port)
    echo "dev-agent: $NAME is already running at $URL"
    return 1
  fi
  go_pid=$(field "$inst" go_pid || true)
  server_pid=$(field "$inst" server_pid || true)
  config=$(field "$inst" config || true)
  binary=$(field "$inst" binary || true)
  stop_recorded "$go_pid" "$server_pid" "$config" "$binary"
  rm -f "$inst"
  return 0
}

wait_until_ready() {
  local deadline foreign listener code
  deadline=$((SECONDS + READY_TIMEOUT))
  while [ "$SECONDS" -lt "$deadline" ]; do
    if ! kill -0 "$CHILD_PID" 2>/dev/null; then
      set +e
      wait "$CHILD_PID"
      code=$?
      set -e
      echo "dev-agent: $NAME exited before it was ready (status $code)" >&2
      show_log
      exit 1
    fi
    foreign=$(foreign_listener "$APP_PORT" "$CHILD_PID" || true)
    if [ -n "$foreign" ]; then
      echo "dev-agent: port $APP_PORT is in use by pid $foreign; left that process running" >&2
      exit 1
    fi
    if health_ok "$APP_PORT"; then
      listener=$(first_owned_listener "$APP_PORT" "$CHILD_PID" || true)
      if [ -n "$listener" ]; then
        SERVER_PID=$listener
        publish_instance running
        return 0
      fi
    fi
    sleep 0.2
  done
  echo "dev-agent: $NAME did not become ready within ${READY_TIMEOUT}s" >&2
  echo "dev-agent: the process is still running; stopping it" >&2
  show_log
  exit 1
}

cmd_start() {
  NAME=$1
  validate_name "$NAME"
  require_cmd lsof
  require_cmd curl
  require_cmd pgrep
  prepare_state

  INSTANCE_DIR=$STATE_DIR/$NAME
  INSTANCE_FILE=$INSTANCE_DIR/instance.env
  mkdir -p "$INSTANCE_DIR"

  trap cleanup EXIT
  trap 'exit 0' INT TERM HUP

  acquire_lock
  if [ -f "$INSTANCE_FILE" ]; then
    if ! recover_stale "$INSTANCE_FILE"; then
      release_lock
      exit 0
    fi
  fi

  acquire_claim_lock
  if ! claim_free_port; then
    echo "dev-server: no free port in $PORT_MIN-$PORT_MAX" >&2
    exit 1
  fi
  URL="http://localhost:$APP_PORT"
  INSTANCE_BINARY=$INSTANCE_DIR/air/main
  INSTANCE_CONFIG=$INSTANCE_DIR/air.toml
  LOG_FILE=$INSTANCE_DIR/air.log
  mkdir -p "$INSTANCE_DIR/air" "$INSTANCE_DIR/data"
  if ! render_air_config "$INSTANCE_CONFIG" "$INSTANCE_BINARY" "$INSTANCE_DIR/air"; then
    exit 1
  fi
  printf '%s\n' "$APP_PORT" > "$INSTANCE_DIR/port"
  OWN_INSTANCE=1
  publish_instance starting
  # The claim directory stays after the lock is released, until cleanup.
  release_claim_lock
  release_lock

  echo "dev-agent: starting name=$NAME port=$APP_PORT url=$URL"
  : > "$LOG_FILE"
  tail -n +1 -f "$LOG_FILE" &
  TAIL_PID=$!

  export ADDR=":$APP_PORT"
  export PORT="$APP_PORT"
  export DB_PATH="$INSTANCE_DIR/data/trackanything.db"
  export BASE_URL="$URL"

  # The config path is on the command line so stop can recognize the process.
  if [ -n "${DEV_SERVER_COMMAND:-}" ]; then
    "$DEV_SERVER_COMMAND" "$INSTANCE_CONFIG" >>"$LOG_FILE" 2>&1 &
  else
    go run "$AIR_PKG" -c "$INSTANCE_CONFIG" >>"$LOG_FILE" 2>&1 &
  fi
  CHILD_PID=$!
  publish_instance starting

  wait_until_ready
  echo "dev-agent: ready name=$NAME port=$APP_PORT url=$URL"

  set +e
  wait "$CHILD_PID"
  status=$?
  set -e
  exit "$status"
}

stop_one() {
  local name=$1 dir inst sup go_pid server_pid config binary i
  local claim_id="" claim_port="" claim_worktree=""
  validate_name "$name"
  prepare_state
  dir=$STATE_DIR/$name
  inst=$dir/instance.env
  if [ ! -f "$inst" ]; then
    echo "dev-server: $name is not running" >&2
    return 1
  fi
  sup=$(field "$inst" supervisor_pid || true)
  go_pid=$(field "$inst" go_pid || true)
  server_pid=$(field "$inst" server_pid || true)
  config=$(field "$inst" config || true)
  binary=$(field "$inst" binary || true)
  claim_id=$(field "$inst" claim_id || true)
  claim_port=$(field "$inst" claim_port || true)
  claim_worktree=$(field "$inst" worktree || true)

  if [ -n "$sup" ] && kill -0 "$sup" 2>/dev/null; then
    if owns_supervisor "$sup" "$name"; then
      kill -TERM "$sup" 2>/dev/null || true
      i=0
      while [ "$i" -lt 24 ]; do
        if ! kill -0 "$sup" 2>/dev/null; then
          break
        fi
        if ! owns_supervisor "$sup" "$name"; then
          break
        fi
        sleep 0.25
        i=$((i + 1))
      done
      if kill -0 "$sup" 2>/dev/null && owns_supervisor "$sup" "$name"; then
        kill -KILL "$sup" 2>/dev/null || true
      fi
    else
      echo "dev-server: $name pid $sup is not its dev server; not killing it" >&2
    fi
  fi

  stop_recorded "$go_pid" "$server_pid" "$config" "$binary"
  rm -f "$inst"
  rm -rf "$dir/air"
  if [ -n "$claim_id" ] && [ -n "$claim_port" ] && [ -n "$claim_worktree" ] && [ -n "$sup" ]; then
    acquire_claim_lock
    release_stopped_claim "$claim_port" "$claim_id" "$sup" "$name" "$claim_worktree"
    release_claim_lock
  fi
  return 0
}

cmd_stop() {
  stop_one "$1"
}

cmd_list() {
  local dir inst name port url sup status found=0
  prepare_state
  for dir in "$STATE_DIR"/*; do
    [ -d "$dir" ] || continue
    inst=$dir/instance.env
    [ -f "$inst" ] || continue
    found=1
    name=$(field "$inst" name || basename "$dir")
    port=$(field "$inst" port || true)
    url=$(field "$inst" url || true)
    sup=$(field "$inst" supervisor_pid || true)
    status=$(field "$inst" status || true)
    if [ -n "$sup" ] && kill -0 "$sup" 2>/dev/null && owns_supervisor "$sup" "$name"; then
      [ "$status" = "running" ] || status=starting
    else
      status=stale
    fi
    printf '%s %s %s %s\n' "$name" "$port" "$url" "$status"
  done
  if [ "$found" = 0 ]; then
    echo "no agent dev servers"
  fi
}

cmd_stop_all() {
  local dir name found=0
  prepare_state
  for dir in "$STATE_DIR"/*; do
    [ -d "$dir" ] || continue
    [ -f "$dir/instance.env" ] || continue
    found=1
    name=$(basename "$dir")
    stop_one "$name" || true
  done
  if [ "$found" = 0 ]; then
    echo "no agent dev servers"
  fi
}

main() {
  local cmd=${1:-}
  case "$cmd" in
    start)
      [ $# -eq 2 ] || usage
      cmd_start "$2"
      ;;
    stop)
      [ $# -eq 2 ] || usage
      cmd_stop "$2"
      ;;
    list)
      [ $# -eq 1 ] || usage
      cmd_list
      ;;
    stop-all)
      [ $# -eq 1 ] || usage
      cmd_stop_all
      ;;
    *)
      usage
      ;;
  esac
}

main "$@"
