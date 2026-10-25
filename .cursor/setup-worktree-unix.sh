#!/bin/bash
# Cursor runs this in a new Git worktree and sets ROOT_WORKTREE_PATH to the
# main checkout. The script copies safe local settings into this worktree and
# downloads Go modules. It does not start a server or write outside this
# checkout. SETUP_WORKTREE_SKIP_MOD=1 skips the module download; tests use that.

set -euo pipefail

cd "$(cd "$(dirname "$0")/.." && pwd)"

if [ -z "${ROOT_WORKTREE_PATH:-}" ]; then
  echo "setup-worktree: ROOT_WORKTREE_PATH is not set" >&2
  exit 1
fi

trim_ws() {
  local s=$1
  s=${s#"${s%%[![:space:]]*}"}
  s=${s%"${s##*[![:space:]]}"}
  printf '%s\n' "$s"
}

unquote_env() {
  local val=$1
  case "$val" in
    \"*\")
      val=${val#\"}
      val=${val%\"}
      printf '%s\n' "$val"
      return 0
      ;;
    \'*\')
      val=${val#\'}
      val=${val%\'}
      printf '%s\n' "$val"
      return 0
      ;;
    *" #"*)
      val=${val%% #*}
      val=$(trim_ws "$val")
      ;;
  esac
  printf '%s\n' "$val"
}

valid_key() {
  case "$1" in
    ''|*[!A-Za-z0-9_]*) return 1 ;;
  esac
  case "$1" in
    [0-9]*) return 1 ;;
  esac
  return 0
}

local_http() {
  case "$1" in
    http://localhost|http://localhost/*|http://localhost:*|\
    http://127.0.0.1|http://127.0.0.1/*|http://127.0.0.1:*)
      return 0
      ;;
  esac
  return 1
}

# A non-empty value is safe when it is local development configuration.
# Production credentials, production URLs, and absolute data paths are not.
value_is_safe() {
  local key=$1 val=$2 upper port
  upper=$(printf '%s' "$key" | tr '[:lower:]' '[:upper:]')
  case "$upper" in
    *SECRET*|*PASSWORD*|*PASSWD*|*TOKEN*|*CREDENTIAL*|*PRIVATE*|*API_KEY*|*_PASS|TURNSTILE_*)
      return 1
      ;;
  esac
  case "$upper" in
    SMTP_USER|TRUSTED_IP_HEADER)
      return 1
      ;;
    ENV)
      [ "$val" = "dev" ]
      return
      ;;
    SMTP_HOST)
      case "$val" in
        localhost|127.0.0.1|::1) return 0 ;;
      esac
      return 1
      ;;
    SMTP_PORT|PORT)
      case "$val" in
        *[!0-9]*) return 1 ;;
      esac
      return 0
      ;;
    BASE_URL)
      local_http "$val"
      return
      ;;
    ADDR)
      case "$val" in
        :*[!0-9]*) return 1 ;;
        :*) return 0 ;;
        localhost:*|127.0.0.1:*)
          port=${val##*:}
          case "$port" in
            ''|*[!0-9]*) return 1 ;;
          esac
          return 0
          ;;
      esac
      return 1
      ;;
    DB_PATH)
      case "$val" in
        /*|*..*) return 1 ;;
      esac
      return 0
      ;;
  esac
  case "$val" in
    /*) return 1 ;;
    *://*)
      local_http "$val"
      return
      ;;
  esac
  return 0
}

# Prints unsafe key names on stdout, one per line. Prints nothing and returns
# 2 when a line is not KEY=VALUE. Values are never printed.
unsafe_env_keys() {
  local file=$1 line key val
  while IFS= read -r line || [ -n "$line" ]; do
    line=$(trim_ws "$line")
    case "$line" in
      ''|'#'*) continue ;;
    esac
    case "$line" in
      export\ *) line=$(trim_ws "${line#export }") ;;
    esac
    case "$line" in
      *=*) ;;
      *)
        echo "setup-worktree: .env has a line that is not KEY=VALUE; not copying it" >&2
        return 2
        ;;
    esac
    key=$(trim_ws "${line%%=*}")
    val=$(trim_ws "${line#*=}")
    if ! valid_key "$key"; then
      echo "setup-worktree: .env has a line that is not KEY=VALUE; not copying it" >&2
      return 2
    fi
    val=$(unquote_env "$val")
    if [ -z "$val" ]; then
      continue
    fi
    if ! value_is_safe "$key" "$val"; then
      printf '%s\n' "$key"
    fi
  done < "$file"
}

copy_example() {
  local example=$1 keys status
  if [ ! -f "$example" ] || [ -L "$example" ]; then
    return 1
  fi
  set +e
  keys=$(unsafe_env_keys "$example")
  status=$?
  set -e
  if [ "$status" -ne 0 ] || [ -n "$keys" ]; then
    echo "setup-worktree: not copying .env.example because it is not safe local configuration"
    echo "setup-worktree: values were not printed"
    return 1
  fi
  cp "$example" .env
  echo "setup-worktree: copied .env.example to .env"
  return 0
}

copy_dev_env() {
  local src="$ROOT_WORKTREE_PATH/.env"
  local example="$ROOT_WORKTREE_PATH/.env.example"
  local keys="" status=0 copied=0
  if [ -e .env ] || [ -L .env ]; then
    echo "setup-worktree: leaving existing .env in place"
    return 0
  fi
  if [ -L "$src" ]; then
    echo "setup-worktree: not copying .env from the main checkout because it is a symlink"
  elif [ -f "$src" ]; then
    set +e
    keys=$(unsafe_env_keys "$src")
    status=$?
    set -e
    if [ "$status" -eq 0 ] && [ -z "$keys" ]; then
      cp "$src" .env
      echo "setup-worktree: copied .env from the main checkout"
      copied=1
    elif [ "$status" -eq 0 ]; then
      echo "setup-worktree: not copying .env from the main checkout; these settings are not safe to reuse:"
      printf '%s\n' "$keys"
      echo "setup-worktree: values were not printed"
    elif [ "$status" -ne 2 ]; then
      return "$status"
    fi
  elif [ -e "$src" ]; then
    echo "setup-worktree: not copying .env from the main checkout because it is not a regular file"
  else
    echo "setup-worktree: main checkout has no .env"
  fi
  if [ "$copied" -eq 1 ]; then
    return 0
  fi
  if [ ! -f "$example" ] && [ -f .env.example ]; then
    example=".env.example"
  fi
  if copy_example "$example"; then
    return 0
  fi
  echo "setup-worktree: no .env created. Copy .env.example to .env in this worktree for local MailHog settings (SMTP_HOST=127.0.0.1 and SMTP_PORT=1025). A missing .env is fine."
}

copy_dev_env

if [ "${SETUP_WORKTREE_SKIP_MOD:-}" = 1 ]; then
  exit 0
fi
if [ -f go.mod ]; then
  go mod download
else
  echo "setup-worktree: no go.mod in this worktree; skipped module download"
fi
