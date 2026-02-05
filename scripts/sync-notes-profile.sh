#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/sync-notes-profile.sh <mode>

Modes:
  sync        Enable sync features, disable notes features
  sync-notes  Enable sync and notes features
  off         Disable sync and notes features
  status      Show current flag states

Environment overrides:
  TD_REPO=/path/to/td
  SIDECAR_REPO=/path/to/sidecar
  SIDECAR_CONFIG=/path/to/sidecar/config.json
  TD_BIN=/path/to/td
  JQ_BIN=/path/to/jq
EOF
}

require_bin() {
  local name="$1"
  local value="$2"
  if [[ -z "$value" ]]; then
    echo "error: required binary not found: $name" >&2
    exit 1
  fi
}

to_bool() {
  local state="$1"
  if [[ "$state" == "on" ]]; then
    echo "true"
  else
    echo "false"
  fi
}

print_status() {
  local repo="$1"
  echo "[$repo]"
  (
    cd "$repo"
    "$TD_BIN" feature get sync_cli
    "$TD_BIN" feature get sync_autosync
    "$TD_BIN" feature get sync_monitor_prompt
    "$TD_BIN" feature get sync_notes
  )
}

set_td_flags() {
  local repo="$1"
  local sync_bool="$2"
  local sync_notes_bool="$3"
  (
    cd "$repo"
    "$TD_BIN" feature set sync_cli "$sync_bool" >/dev/null
    "$TD_BIN" feature set sync_autosync "$sync_bool" >/dev/null
    "$TD_BIN" feature set sync_monitor_prompt "$sync_bool" >/dev/null
    "$TD_BIN" feature set sync_notes "$sync_notes_bool" >/dev/null
  )
}

set_sidecar_notes_plugin() {
  local notes_bool="$1"
  mkdir -p "$(dirname "$SIDECAR_CONFIG")"
  if [[ ! -f "$SIDECAR_CONFIG" ]]; then
    echo '{}' >"$SIDECAR_CONFIG"
  fi

  local tmp
  tmp="$(mktemp)"
  "$JQ_BIN" \
    --argjson notes "$notes_bool" \
    '.features |= ((. // {}) | .flags |= ((. // {}) | .notes_plugin = $notes))' \
    "$SIDECAR_CONFIG" >"$tmp"
  mv "$tmp" "$SIDECAR_CONFIG"
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SIDECAR_REPO="${SIDECAR_REPO:-$(cd "$SCRIPT_DIR/.." && pwd)}"
TD_REPO="${TD_REPO:-$(cd "$SIDECAR_REPO/../td" && pwd)}"
SIDECAR_CONFIG="${SIDECAR_CONFIG:-$HOME/.config/sidecar/config.json}"
if [[ -z "${TD_BIN:-}" ]]; then
  if [[ -x "/Users/marcusvorwaller/go/bin/td" ]]; then
    TD_BIN="/Users/marcusvorwaller/go/bin/td"
  else
    TD_BIN="$(command -v td || true)"
  fi
fi
JQ_BIN="${JQ_BIN:-$(command -v jq || true)}"

require_bin "td" "$TD_BIN"
require_bin "jq" "$JQ_BIN"

if [[ ! -d "$SIDECAR_REPO/.git" ]]; then
  echo "error: SIDECAR_REPO is not a git repo: $SIDECAR_REPO" >&2
  exit 1
fi
if [[ ! -d "$TD_REPO/.git" ]]; then
  echo "error: TD_REPO is not a git repo: $TD_REPO" >&2
  exit 1
fi

MODE="${1:-}"
if [[ -z "$MODE" ]]; then
  usage
  exit 1
fi

case "$MODE" in
  sync)
    sync_state="on"
    notes_state="off"
    ;;
  sync-notes)
    sync_state="on"
    notes_state="on"
    ;;
  off)
    sync_state="off"
    notes_state="off"
    ;;
  status)
    print_status "$SIDECAR_REPO"
    print_status "$TD_REPO"
    echo "[${SIDECAR_CONFIG}]"
    "$JQ_BIN" -r '.features.flags.notes_plugin // false | "notes_plugin=\(.)"' "$SIDECAR_CONFIG"
    exit 0
    ;;
  -h|--help|help)
    usage
    exit 0
    ;;
  *)
    echo "error: unknown mode: $MODE" >&2
    usage
    exit 1
    ;;
esac

sync_bool="$(to_bool "$sync_state")"
notes_bool="$(to_bool "$notes_state")"
if [[ "$sync_state" == "on" && "$notes_state" == "on" ]]; then
  sync_notes_bool="true"
else
  sync_notes_bool="false"
fi

set_td_flags "$SIDECAR_REPO" "$sync_bool" "$sync_notes_bool"
set_td_flags "$TD_REPO" "$sync_bool" "$sync_notes_bool"
set_sidecar_notes_plugin "$notes_bool"

echo "applied profile: $MODE"
print_status "$SIDECAR_REPO"
print_status "$TD_REPO"
echo "[${SIDECAR_CONFIG}]"
"$JQ_BIN" -r '.features.flags.notes_plugin // false | "notes_plugin=\(.)"' "$SIDECAR_CONFIG"
