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

PORT="${PORT:-28080}"
WEBUI_PORT="${WEBUI_PORT:-28081}"
VITE_API_TARGET="${VITE_API_TARGET:-http://127.0.0.1:${PORT}}"

exec make webui-dev
