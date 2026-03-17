#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONTAINER_ID="$(docker compose -f "$ROOT_DIR/compose.yml" ps -q truckapi)"
TMP_DIR=""
TMP_DB=""

usage() {
  cat <<'EOF'
Usage:
  scripts/chrob_audit.sh recent [limit]
  scripts/chrob_audit.sh zip <origin_zip> [limit]
  scripts/chrob_audit.sh load <load_number>
  scripts/chrob_audit.sh window <start_utc> <end_utc>
  scripts/chrob_audit.sh dupes [hours]

Examples:
  scripts/chrob_audit.sh recent 25
  scripts/chrob_audit.sh zip 77375 50
  scripts/chrob_audit.sh load 546891694
  scripts/chrob_audit.sh window '2026-03-12 20:20:00' '2026-03-12 20:25:00'
  scripts/chrob_audit.sh dupes 24
EOF
}

require_bin() {
  local bin="$1"
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "missing required binary: $bin" >&2
    exit 1
  fi
}

snapshot_db() {
  if [[ -z "$CONTAINER_ID" ]]; then
    echo "truckapi container is not running" >&2
    exit 1
  fi
  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/chrob_audit.XXXXXX")"
  TMP_DB="$TMP_DIR/truckapi.db"
  docker cp "$CONTAINER_ID:/var/lib/truckapi/truckapi.db" "$TMP_DB"
  docker cp "$CONTAINER_ID:/var/lib/truckapi/truckapi.db-wal" "$TMP_DIR/truckapi.db-wal" >/dev/null 2>&1 || true
  docker cp "$CONTAINER_ID:/var/lib/truckapi/truckapi.db-shm" "$TMP_DIR/truckapi.db-shm" >/dev/null 2>&1 || true
}

cleanup() {
  if [[ -n "$TMP_DIR" && -d "$TMP_DIR" ]]; then
    rm -rf "$TMP_DIR"
  fi
}

run_query() {
  local sql="$1"
  sqlite3 -header -column "$TMP_DB" "$sql"
}

require_bin docker
require_bin sqlite3
trap cleanup EXIT

if [[ $# -lt 1 ]]; then
  usage
  exit 1
fi

snapshot_db

case "$1" in
  recent)
    limit="${2:-20}"
    run_query "
      select occurred_at, action, order_number, load_number, origin_city, origin_state,
             origin_zip, destination_city, destination_state, destination_zip, reason
      from chrob_loader_audits
      order by occurred_at desc
      limit $limit;
    "
    ;;
  zip)
    if [[ $# -lt 2 ]]; then
      usage
      exit 1
    fi
    zip="$2"
    limit="${3:-50}"
    run_query "
      select occurred_at, action, order_number, load_number, origin_city, origin_state,
             origin_zip, destination_city, destination_state, destination_zip, dedupe_key, reason
      from chrob_loader_audits
      where origin_zip = '$zip'
      order by occurred_at desc
      limit $limit;
    "
    ;;
  load)
    if [[ $# -lt 2 ]]; then
      usage
      exit 1
    fi
    load_number="$2"
    run_query "
      select occurred_at, action, order_number, load_number, origin_city, origin_state,
             origin_zip, destination_city, destination_state, destination_zip, dedupe_key, reason
      from chrob_loader_audits
      where load_number = $load_number
      order by occurred_at desc;
    "
    ;;
  window)
    if [[ $# -lt 3 ]]; then
      usage
      exit 1
    fi
    start_utc="$2"
    end_utc="$3"
    run_query "
      select occurred_at, action, order_number, load_number, origin_city, origin_state,
             origin_zip, destination_city, destination_state, destination_zip, dedupe_key, reason
      from chrob_loader_audits
      where occurred_at between '$start_utc' and '$end_utc'
      order by occurred_at;
    "
    ;;
  dupes)
    hours="${2:-24}"
    run_query "
      select load_number,
             order_number,
             count(*) as posted_count,
             min(occurred_at) as first_seen_utc,
             max(occurred_at) as last_seen_utc
      from chrob_loader_audits
      where action = 'posted_success'
        and occurred_at >= datetime('now', '-$hours hours')
      group by load_number, order_number
      having count(*) > 1
      order by last_seen_utc desc;
    "
    ;;
  *)
    usage
    exit 1
    ;;
esac
