#!/bin/sh
set -eu

if [ "$#" -lt 1 ]; then
  echo "usage: bdinfo <path>" >&2
  exit 1
fi

input="$1"
case "$input" in
  /*) ;;
  *) input="$(pwd)/$input" ;;
esac

out_dir="$(mktemp -d)"
log_file="$(mktemp)"

cleanup() {
  rm -rf "$out_dir" "$log_file"
}
trap cleanup EXIT

bdinfo_exe="/opt/bdinfo/BDInfo.exe"
if [ ! -f "$bdinfo_exe" ]; then
  bdinfo_exe="$(find /opt/bdinfo -maxdepth 4 -type f -name 'BDInfo.exe' | head -n 1)"
fi
if [ -z "$bdinfo_exe" ] || [ ! -f "$bdinfo_exe" ]; then
  echo "bdinfo: BDInfo CLI not found under /opt/bdinfo" >&2
  exit 1
fi

args="${BDINFO_ARGS:-}"

# Non-interactive scan; write report to temp directory
# shellcheck disable=SC2086
if ! mono "$bdinfo_exe" -w $args "$input" "$out_dir" >"$log_file" 2>&1; then
  cat "$log_file" >&2
  exit 1
fi

report="$(find "$out_dir" -maxdepth 1 -type f -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -n 1 | cut -d' ' -f2-)"
if [ -n "$report" ]; then
  cat "$report"
  exit 0
fi

cat "$log_file" >&2
echo "bdinfo: no report file produced" >&2
exit 1
