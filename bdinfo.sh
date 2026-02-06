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
  bdinfo_bin="$(find /opt/bdinfo -maxdepth 4 -type f -perm /111 \( -name 'BDInfo*' -o -name 'bdinfo*' \) | head -n 1)"
fi
if [ -z "$bdinfo_bin" ] || [ ! -f "$bdinfo_bin" ]; then
  echo "bdinfo: BDInfo binary not found under /opt/bdinfo" >&2
  exit 1
fi

args="${BDINFO_ARGS:-}"
report_name="bdinfo.txt"
report_path="$out_dir/$report_name"

# shellcheck disable=SC2086
if ! "$bdinfo_bin" -p "$input" -o "$report_path" $args >"$log_file" 2>&1; then
  cat "$log_file" >&2
  exit 1
fi

report="$report_path"
if [ ! -f "$report" ]; then
  report="$(find "$out_dir" -maxdepth 1 -type f -printf '%T@ %p\n' 2>/dev/null | sort -nr | head -n 1 | cut -d' ' -f2-)"
fi
if [ -n "$report" ] && [ -f "$report" ]; then
  filtered="$(mktemp)"
  if awk '
    function start_block() { block=""; block_size=0; }
    function save_block() { if (block_idx > 0) { blocks[block_idx]=block; sizes[block_idx]=block_size; } }
    BEGIN { block_idx=0; start_block(); }
    {
      if (!seen_playlist) {
        if ($0 ~ /^PLAYLIST:/) {
          seen_playlist=1;
        } else {
          prefix = prefix $0 ORS;
          next;
        }
      }
      if ($0 ~ /^PLAYLIST:/) {
        if (block_idx > 0) { save_block(); }
        block_idx++;
        start_block();
      }
      block = block $0 ORS;
      if ($0 ~ /^Size:[[:space:]]*[0-9,]+ bytes/) {
        size = $0;
        gsub(/[^0-9]/, "", size);
        if (size + 0 > block_size) { block_size = size + 0; }
      }
    }
    END {
      if (block_idx > 0) { save_block(); }
      if (block_idx == 0) {
        printf "%s", prefix;
        exit;
      }
      max = 0; maxi = 0;
      for (i = 1; i <= block_idx; i++) {
        if (sizes[i] > max) { max = sizes[i]; maxi = i; }
      }
      if (maxi == 0) { maxi = 1; }
      printf "%s%s", prefix, blocks[maxi];
    }
  ' "$report" > "$filtered" 2>/dev/null; then
    cat "$filtered"
    rm -f "$filtered"
    exit 0
  fi
  rm -f "$filtered"
  cat "$report"
  exit 0
fi

cat "$log_file" >&2
echo "bdinfo: no report file produced" >&2
exit 1
