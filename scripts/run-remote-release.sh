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

REMOTE_SSH_HOST_VALUE="${REMOTE_SSH_HOST:-}"
REMOTE_SSH_USER_VALUE="${REMOTE_SSH_USER:-root}"
REMOTE_SSH_PORT_VALUE="${REMOTE_SSH_PORT:-22}"
REMOTE_DEPLOY_DIR_VALUE="${REMOTE_DEPLOY_DIR:-/opt/minfo}"
REMOTE_TARGET="${REMOTE_SSH_USER_VALUE}@${REMOTE_SSH_HOST_VALUE}"
ARCHIVE_PATH="${TMPDIR:-/tmp}/minfo-remote-release.tar.gz"
REMOTE_ARCHIVE_PATH="/tmp/minfo-remote-release.tar.gz"
CONTROL_SOCKET="${TMPDIR:-/tmp}/minfo-remote-release.sock"

if [[ -z "$REMOTE_SSH_HOST_VALUE" ]]; then
    echo "REMOTE_SSH_HOST is required in .env" >&2
    exit 1
fi

cleanup() {
    rm -f "$ARCHIVE_PATH"
    ssh -p "${REMOTE_SSH_PORT_VALUE}" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -S "$CONTROL_SOCKET" -O exit "$REMOTE_TARGET" >/dev/null 2>&1 || true
    rm -f "$CONTROL_SOCKET"
}
trap cleanup EXIT

rm -f "$ARCHIVE_PATH" "$CONTROL_SOCKET"

tar \
    --exclude=".git" \
    --exclude=".gocache" \
    --exclude="webui/node_modules" \
    --exclude="webui/dist" \
    --exclude="bin" \
    --exclude="__debug_bin*" \
    -czf "$ARCHIVE_PATH" \
    -C "$ROOT_DIR" .

ssh -M -S "$CONTROL_SOCKET" -fN \
    -p "${REMOTE_SSH_PORT_VALUE}" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    "$REMOTE_TARGET"

scp -P "${REMOTE_SSH_PORT_VALUE}" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ControlPath="$CONTROL_SOCKET" \
    "$ARCHIVE_PATH" \
    "${REMOTE_TARGET}:${REMOTE_ARCHIVE_PATH}"

exec ssh -T \
    -p "${REMOTE_SSH_PORT_VALUE}" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ControlPath="$CONTROL_SOCKET" \
    "$REMOTE_TARGET" \
    "mkdir -p '${REMOTE_DEPLOY_DIR_VALUE}' && find '${REMOTE_DEPLOY_DIR_VALUE}' -mindepth 1 -maxdepth 1 ! -name '.env' -exec rm -rf {} + && tar -xzf '${REMOTE_ARCHIVE_PATH}' -C '${REMOTE_DEPLOY_DIR_VALUE}' && rm -f '${REMOTE_ARCHIVE_PATH}' && cd '${REMOTE_DEPLOY_DIR_VALUE}' && docker compose -f docker-compose.yml up -d --build && docker compose -f docker-compose.yml ps minfo && echo && echo 'remote release url: http://${REMOTE_SSH_HOST_VALUE}:38080' && echo && docker compose -f docker-compose.yml logs --no-color --tail 20 minfo"
