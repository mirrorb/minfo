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

bdinfo_bin="$(find /opt/bdinfo -maxdepth 4 -type f \( -iname 'BDInfo*' -o -iname 'bdinfo*' \) -perm /111 | head -n 1)"
if [ -z "$bdinfo_bin" ]; then
  echo "bdinfo: executable not found under /opt/bdinfo" >&2
  exit 1
fi

args="${BDINFO_ARGS:--w}"

# shellcheck disable=SC2086
if ! output="$(cd "$out_dir" && "$bdinfo_bin" $args "$input" 2>"$log_file")"; then
  cat "$log_file" >&2
  exit 1
fi

if [ -n "$(printf '%s' "$output" | tr -d '\r\n')" ]; then
  printf '%s\n' "$output"
  exit 0
fi

report="$(find "$out_dir" -maxdepth 1 -type f \( -iname '*.txt' -o -iname '*.log' -o -iname '*.bdinfo' \) -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -n 1 | cut -d' ' -f2-)"
if [ -n "$report" ]; then
  cat "$report"
  exit 0
fi

cat "$log_file" >&2
echo "bdinfo: no output produced" >&2
exit 1
