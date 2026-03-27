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

DEBUG_HOST_PORT="${DEBUG_HOST_PORT:-48080}"
WEBUI_PORT="${WEBUI_PORT:-48081}"
VITE_API_TARGET="${VITE_API_TARGET:-http://127.0.0.1:${DEBUG_HOST_PORT}}"

exec make webui-dev
