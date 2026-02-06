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

bdinfo_bin="/opt/bdinfo/BDInfo"
if [ ! -f "$bdinfo_bin" ]; then
  bdinfo_bin="/opt/bdinfo/BDInfo.exe"
fi
if [ ! -f "$bdinfo_bin" ]; then
  bdinfo_bin="$(find /opt/bdinfo -maxdepth 4 -type f \( -name 'BDInfo' -o -name 'BDInfo.exe' \) | head -n 1)"
fi
if [ -z "$bdinfo_bin" ] || [ ! -f "$bdinfo_bin" ]; then
  echo "bdinfo: BDInfo binary not found under /opt/bdinfo" >&2
  exit 1
fi

args="${BDINFO_ARGS:-}"
report_name="bdinfo.txt"

if [ "${bdinfo_bin##*.}" = "exe" ]; then
  bdinfo_dir="$(dirname "$bdinfo_bin")"
  mono_path=""
  if command -v find >/dev/null 2>&1; then
    mono_path="$(find /opt/bdinfo -type f -name '*.dll' -printf '%h\n' 2>/dev/null | sort -u | tr '\n' ':' | sed 's/:$//')"
  fi
  if [ -n "$mono_path" ]; then
    export MONO_PATH="$mono_path${MONO_PATH:+:$MONO_PATH}"
  fi
  cd "$bdinfo_dir"
  # shellcheck disable=SC2086
  if ! mono "$bdinfo_bin" -p "$input" -r "$out_dir" -o "$report_name" $args >"$log_file" 2>&1; then
    cat "$log_file" >&2
    exit 1
  fi
else
  # shellcheck disable=SC2086
  if ! "$bdinfo_bin" -p "$input" -r "$out_dir" -o "$report_name" $args >"$log_file" 2>&1; then
    cat "$log_file" >&2
    exit 1
  fi
fi

report="$(find "$out_dir" -maxdepth 1 -type f -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -n 1 | cut -d' ' -f2-)"
if [ -n "$report" ]; then
  cat "$report"
  exit 0
fi

cat "$log_file" >&2
echo "bdinfo: no report file produced" >&2
exit 1
