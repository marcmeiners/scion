#!/usr/bin/env bash
set -euo pipefail

# Fault injection helper for demo/scion-chat:
# - Removes public-ISD control-plane rows from beacon/path/trust sqlite DBs
# - Leaves private membership rows intact
# - Destructive by design (no backup/restore), intended for disposable gen/gen-cache state
#
# Usage:
#   demo/scion-chat/fault_public_down.sh on --root <db-root> [--public-isd 1]
#   demo/scion-chat/fault_public_down.sh status --root <db-root> [--public-isd 1]
#   demo/scion-chat/fault_public_down.sh hold --root <db-root> [--public-isd 1] [--interval 2]


ROOT=""
PUBLIC_ISD=1
INTERVAL=2
ACTION="${1:-}"
shift || true

usage() {
    echo "Usage: $0 <on|status|hold> --root <db-root> [--public-isd N] [--interval sec]"
    exit 2
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --root)
            ROOT="${2:-}"
            shift 2
            ;;
        --public-isd)
            PUBLIC_ISD="${2:-}"
            shift 2
            ;;
        --interval)
            INTERVAL="${2:-}"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1"
            usage
            ;;
    esac
done

if [[ -z "$ACTION" ]]; then
    usage
fi
if [[ -z "$ROOT" ]]; then
    echo "--root is required"
    usage
fi
if [[ ! -d "$ROOT" ]]; then
    echo "root directory does not exist: ${ROOT}"
    exit 1
fi
if ! [[ "$PUBLIC_ISD" =~ ^[0-9]+$ ]]; then
    echo "--public-isd must be an integer"
    exit 2
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "sqlite3 is required"
    exit 1
fi

discover_dbs() {
    find "$ROOT" -type f \( -name "*.beacon.db" -o -name "*.path.db" -o -name "*.trust.db" \) | sort
}

table_exists() {
    local db="$1"
    local table="$2"
    sqlite3 "$db" "SELECT 1 FROM sqlite_master WHERE type='table' AND name='${table}' LIMIT 1;" | grep -q 1
}

sql_exec() {
    local db="$1"
    local sql="$2"
    sqlite3 "$db" "PRAGMA busy_timeout=2000; PRAGMA foreign_keys=ON; ${sql}" >/dev/null
}

inject_once() {
    local db_count=0
    # phase 1: remove public routing/beacon state first
    while IFS= read -r db; do
        db_count=$((db_count + 1))
        if table_exists "$db" "Beacons"; then
            sql_exec "$db" "DELETE FROM Beacons WHERE LocalIsd = ${PUBLIC_ISD};"
            echo "pruned public beacons in ${db}"
        fi
        if table_exists "$db" "Segments"; then
            sql_exec "$db" "DELETE FROM Segments WHERE StartIsdID = ${PUBLIC_ISD} OR EndIsdID = ${PUBLIC_ISD};"
            if table_exists "$db" "NextQuery"; then
                sql_exec "$db" "DELETE FROM NextQuery WHERE SrcIsdID = ${PUBLIC_ISD} OR DstIsdID = ${PUBLIC_ISD};"
            fi
            echo "pruned public paths in ${db}"
        fi
    done < <(discover_dbs)

    # phase 2: remove public trust material (TRCs/chains) after path/beacon state
    while IFS= read -r db; do
        if table_exists "$db" "trcs"; then
            sql_exec "$db" "DELETE FROM trcs WHERE isd_id = ${PUBLIC_ISD};"
            echo "pruned public TRCs in ${db}"
        fi
        if table_exists "$db" "chains"; then
            sql_exec "$db" "DELETE FROM chains WHERE isd_id = ${PUBLIC_ISD};"
            echo "pruned public chains in ${db}"
        fi
    done < <(discover_dbs)
    if [[ "$db_count" -eq 0 ]]; then
        echo "no *.beacon.db, *.path.db, or *.trust.db found under ${ROOT}"
        exit 1
    fi
}

show_status() {
    local db_count=0
    while IFS= read -r db; do
        db_count=$((db_count + 1))
        echo "== ${db}"
        if table_exists "$db" "Beacons"; then
            local c
            c="$(sqlite3 "$db" "SELECT COUNT(*) FROM Beacons WHERE LocalIsd = ${PUBLIC_ISD};")"
            echo "public beacons (LocalIsd=${PUBLIC_ISD}): ${c}"
        fi
        if table_exists "$db" "Segments"; then
            local s q
            s="$(sqlite3 "$db" "SELECT COUNT(*) FROM Segments WHERE StartIsdID = ${PUBLIC_ISD} OR EndIsdID = ${PUBLIC_ISD};")"
            echo "public path segments (Start/End ISD=${PUBLIC_ISD}): ${s}"
            if table_exists "$db" "NextQuery"; then
                q="$(sqlite3 "$db" "SELECT COUNT(*) FROM NextQuery WHERE SrcIsdID = ${PUBLIC_ISD} OR DstIsdID = ${PUBLIC_ISD};")"
                echo "public next-query hints (Src/Dst ISD=${PUBLIC_ISD}): ${q}"
            fi
        fi
        if table_exists "$db" "trcs"; then
            local t
            t="$(sqlite3 "$db" "SELECT COUNT(*) FROM trcs WHERE isd_id = ${PUBLIC_ISD};")"
            echo "public TRCs (isd_id=${PUBLIC_ISD}): ${t}"
        fi
        if table_exists "$db" "chains"; then
            local ch
            ch="$(sqlite3 "$db" "SELECT COUNT(*) FROM chains WHERE isd_id = ${PUBLIC_ISD};")"
            echo "public chains (isd_id=${PUBLIC_ISD}): ${ch}"
        fi
    done < <(discover_dbs)
    if [[ "$db_count" -eq 0 ]]; then
        echo "no *.beacon.db, *.path.db, or *.trust.db found under ${ROOT}"
        exit 1
    fi
}

case "$ACTION" in
    on)
        inject_once
        echo "public outage injected for ISD ${PUBLIC_ISD}"
        ;;
    hold)
        echo "holding public outage for ISD ${PUBLIC_ISD} (interval ${INTERVAL}s), Ctrl+C to stop"
        while true; do
            inject_once
            sleep "$INTERVAL"
        done
        ;;
    status)
        show_status
        ;;
    *)
        usage
        ;;
esac
