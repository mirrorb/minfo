#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -f .env ]]; then
    set -a
    # shellcheck disable=SC1091
    . ./.env
    set +a
fi

MODE="${1:-dlv}"
LOCK_DIR="${TMPDIR:-/tmp}/minfo-docker-debug.lock"
DEBUG_HOST_PORT_VALUE="${DEBUG_HOST_PORT:-48080}"
DLV_HOST_PORT_VALUE="${DLV_HOST_PORT:-2345}"
DEBUG_SERVICE="${DEBUG_SERVICE:-minfo-debug}"

wait_for_port() {
    local port="$1"
    for _ in $(seq 1 120); do
        if (echo >"/dev/tcp/127.0.0.1/${port}") >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

while ! mkdir "$LOCK_DIR" 2>/dev/null; do
    sleep 0.2
done

cleanup_lock() {
    rmdir "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup_lock EXIT

case "$MODE" in
    up)
        docker compose -f docker-compose.debug.yml up -d --build --remove-orphans
        cleanup_lock
        trap - EXIT
        if wait_for_port "$DLV_HOST_PORT_VALUE"; then
            exit 0
        fi
        ;;
    ready)
        cleanup_lock
        trap - EXIT
        if wait_for_port "$DEBUG_HOST_PORT_VALUE"; then
            exit 0
        fi
        ;;
    *)
        echo "unknown mode: $MODE" >&2
        exit 2
        ;;
esac

echo "timed out waiting for docker debug target in mode: $MODE" >&2
docker compose -f docker-compose.debug.yml logs --no-color --tail 100 "$DEBUG_SERVICE" >&2 || true
exit 1
