#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEBUI_DIR="${SCRIPT_DIR%/scripts}/webui"

cd "$WEBUI_DIR"
npm install --include=optional --no-audit --no-fund

# Work around npm optional dependency resolution occasionally skipping
# Rollup's Linux native package on remote hosts.
if [[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]] && [[ ! -d node_modules/@rollup/rollup-linux-x64-gnu ]]; then
    npm install --no-save @rollup/rollup-linux-x64-gnu@4.60.0 --no-audit --no-fund
fi
