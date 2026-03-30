#!/bin/bash
# 高速JPG截图脚本（基于原PNG版本优化）
# - 保留：字幕功能、原始分辨率、目录智能识别
# - 优化：JPG格式输出、提升截图速度、简化色彩处理
# - 输入：视频文件 / ISO / 目录
# - 时间点：提供则就近对齐；未提供则自动取 20%/40%/60%/80% 并对齐
# - 字幕优先：中文 → 英文 → 其他/无
# 用法：
#   ./screenshots_jpg.sh <视频/ISO/目录> <输出目录> [HH:MM:SS|MM:SS] [...]

set -u
log(){ echo -e "$*" >&2; }

require_commands(){
  local missing=()
  local cmd
  for cmd in bc ffmpeg ffprobe jq; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      missing+=("$cmd")
    fi
  done
  if [ ${#missing[@]} -gt 0 ]; then
    log "[错误] 缺少依赖: ${missing[*]}"
    log "[提示] 请通过 Dockerfile 提供上述命令后再运行截图脚本。"
    exit 1
  fi
}

SUBTITLE_ENABLED=1
FILTERED_ARGS=()

for arg in "$@"; do
  if [ "$arg" = "-nosub" ]; then
    SUBTITLE_ENABLED=0
  else
    FILTERED_ARGS+=("$arg")
  fi
done

set -- "${FILTERED_ARGS[@]}"

success_count=0
fail_count=0
failed_files=()
failed_reasons=()

time_regex='^([0-9]{1,2}:)?[0-9]{1,2}:[0-9]{2}$'

# —— 探测与对齐参数（优化版）
PROBESIZE="100M"                # 减少探测数据量提升速度
ANALYZE="100M"                  # 减少分析时间
COARSE_BACK_TEXT=2              # 减少文本字幕预滚动
COARSE_BACK_PGS=8               # 减少PGS预滚动
SEARCH_BACK=4                   # 减少搜索范围
SEARCH_FWD=8
SUB_SNAP_EPS=0.50
DEFAULT_SUB_DUR=4.00
PGS_MIN_PKT=1500
AUTO_PERC=("0.20" "0.40" "0.60" "0.80")

# JPG质量参数
JPG_QUALITY=85                  # JPG质量（1-100，85是高质量与文件大小的平衡点）

MOUNTED=0
MOUNT_DIR=""
ISO_PATH=""
M2TS_INPUT=""
START_OFFSET="0.0"
DURATION="0.0"

SUB_MODE=""
SUB_FILE=""
SUB_SI=""
SUB_REL=""
SUB_LANG=""
SUB_CODEC=""
SUB_TITLE=""
SUB_IDX=""
SUB_TEMP_FILE=""

# —— 语言集合
LANGS_ZH=("zh" "zho" "chi" "zh-cn" "zh_cn" "chs" "cht" "cn" "chinese" "mandarin" "cantonese" "yue" "han")
LANGS_EN=("en" "eng" "english")

lower(){ echo "$1" | tr '[:upper:]' '[:lower:]'; }
has_lang_token(){
  local lang="$(lower "$1")"; shift
  for t in "$@"; do
    t="$(lower "$t")"
    [[ "$lang" == *"$t"* ]] && return 0
  done
  return 1
}
escape_squote(){ echo "${1//\'/\\\'}"; }
is_iso(){
  case "${1##*.}" in
    [iI][sS][oO]) return 0 ;;
    *)            return 1 ;;
  esac
}
hms_to_seconds(){ local t="$1" h=0 m=0 s=0; IFS=':' read -r a b c <<<"$t"; if [ -z "${c:-}" ]; then m=$a; s=$b; else h=$a; m=$b; s=$c; fi; echo $((10#$h*3600 + 10#$m*60 + 10#$s)); }
sec_to_hms(){ local x=${1%.*}; printf "%02d:%02d:%02d" $((x/3600)) $(((x%3600)/60)) $((x%60)); }
sec_to_hms_ms(){ awk -v t="$1" 'BEGIN{ if (t < 0) t = 0; h = int(t/3600); m = int((t%3600)/60); s = t - h*3600 - m*60; printf "%02d:%02d:%06.3f", h, m, s }'; }
fmax(){ awk -v a="$1" -v b="$2" 'BEGIN{printf "%.3f",(a>b?a:b)}'; }
fmin(){ awk -v a="$1" -v b="$2" 'BEGIN{printf "%.3f",(a<b?a:b)}'; }
fadd(){ awk -v a="$1" -v b="$2" 'BEGIN{printf "%.3f",a+b}'; }
fsub(){ awk -v a="$1" -v b="$2" 'BEGIN{printf "%.3f",a-b}'; }
clamp_0_dur(){ awk -v t="$1" -v mx="$DURATION" 'BEGIN{if(t<0)t=0; if(t>mx)t=mx; printf "%.3f",t}'; }
float_diff_gt(){ awk -v a="$1" -v b="$2" -v eps="${3:-0.0005}" 'BEGIN{d=a-b; if(d<0)d=-d; exit !(d>eps)}'; }

cleanup(){
  if [ "$MOUNTED" -eq 1 ] && [ -n "$MOUNT_DIR" ]; then
    log "[清理] 正在卸载 ISO：$MOUNT_DIR"
    if sudo umount "$MOUNT_DIR" 2>/tmp/umount_err.log; then
      log "[清理] 卸载成功，删除临时目录"
      rmdir "$MOUNT_DIR" 2>/dev/null && log "[清理] 已删除临时目录 $MOUNT_DIR"
    else
      log "[警告] 卸载失败："; cat /tmp/umount_err.log >&2
    fi
    rm -f /tmp/umount_err.log
  fi
  [ -n "${SUB_IDX:-}" ] && [ -f "$SUB_IDX" ] && rm -f "$SUB_IDX"
  [ -n "${SUB_TEMP_FILE:-}" ] && [ -f "$SUB_TEMP_FILE" ] && rm -f "$SUB_TEMP_FILE"
}
trap 'cleanup' EXIT INT TERM

validate_arguments(){
  if [ "$#" -lt 2 ]; then
    echo "[错误] 参数缺失：必须提供视频文件/ISO/目录和截图输出目录。"
    echo "正确用法: $0 <视频/ISO/目录> <输出目录> [时间点...]"
    exit 1
  fi

  local p="$1" outdir="$2"
  
  if [ ! -f "$p" ] && [ ! -d "$p" ]; then
    echo "[错误] 视频文件或目录不存在：$p"
    echo "正确用法: $0 <视频/ISO/目录> <输出目录> [时间点...]"
    exit 1
  fi

  if [ -z "$outdir" ]; then
    echo "[错误] 参数缺失：必须提供截图输出目录。"
    echo "正确用法: $0 <视频/ISO/目录> <输出目录> [时间点...]"
    exit 1
  fi

  if [ ! -d "$outdir" ]; then
    log "[提示] 输出目录不存在，正在创建：$outdir"
    mkdir -p "$outdir" || { log "[错误] 创建输出目录失败：$outdir"; exit 1; }
  fi

  shift 2
  for t in "$@"; do
    if [[ ! "$t" =~ ^([0-9]{1,2}:)?[0-9]{1,2}:[0-9]{2}$ ]]; then
      echo "[错误] 时间点格式不正确：$t"
      echo "正确格式: 00:30:00 或 30:00"
      exit 1
    fi
  done
}

# —— ISO & m2ts（保持原样）
mount_iso(){
  ISO_PATH="$1"
  local iso_dir iso_base ts
  iso_dir="$(cd "$(dirname "$ISO_PATH")" && pwd)"
  iso_base="$(basename "$ISO_PATH" .iso)"
  ts="$(date +%s)"
  MOUNT_DIR="${iso_dir}/.${iso_base}_mnt_${ts}_$$"
  mkdir -p "$MOUNT_DIR" || { log "[错误] 创建挂载目录失败：$MOUNT_DIR"; exit 1; }

  log "[提示] 识别到 ISO 文件：$ISO_PATH"
  log "[信息] 挂载 ISO -> $MOUNT_DIR"
  if ! sudo mount -o loop "$ISO_PATH" "$MOUNT_DIR" 2>/tmp/mount_err.log; then
    log "[错误] 挂载 ISO 失败："; cat /tmp/mount_err.log >&2
    rm -f /tmp/mount_err.log; rmdir "$MOUNT_DIR" 2>/dev/null; exit 1
  fi
  rm -f /tmp/mount_err.log
  MOUNTED=1
}

find_largest_m2ts_in_dir(){
  local root="$1" search_root="$1"
  [ -d "$root/BDMV/STREAM" ] && search_root="$root/BDMV/STREAM"
  log "[信息] 在目录中搜索最大 .m2ts：$search_root"
  local biggest
  biggest=$(find "$search_root" -type f -iname '*.m2ts' -printf '%s %p\n' 2>/dev/null | sort -nr | head -1 | cut -d' ' -f2-)
  [ -z "$biggest" ] && { log "[错误] 目录中未找到 .m2ts 文件"; return 1; }
  log "[信息] 选定最大 m2ts：$biggest"
  M2TS_INPUT="$biggest"; return 0
}

find_largest_m2ts(){
  local base_dir="$1" search_root
  if [ -d "$base_dir/BDMV/STREAM" ]; then search_root="$base_dir/BDMV/STREAM"
  else search_root="$base_dir"; log "[提示] 未找到 BDMV/STREAM，回退全盘搜索"; fi
  log "[信息] 正在搜索最大 m2ts 文件于：$search_root"
  local max_size=0 max_file="" sz
  while IFS= read -r -d '' f; do
    sz=$(stat -c%s "$f" 2>/dev/null || echo 0)
    [[ "$sz" =~ ^[0-9]+$ ]] || continue
    if [ "$sz" -gt "$max_size" ]; then max_size="$sz"; max_file="$f"; fi
  done < <(find "$search_root" -type f -iname '*.m2ts' -print0)
  [ -z "$max_file" ] && { log "[错误] 未找到 .m2ts"; return 1; }
  log "[信息] 选定最大 m2ts：$max_file （大小：$((max_size/1024/1024)) MB）"
  M2TS_INPUT="$max_file"; return 0
}

# —— 目录选择逻辑（保持原样）
select_input_from_arg(){
  local input="$1"

  if [ -f "$input" ]; then
    if is_iso "$input"; then
      mount_iso "$input"
      find_largest_m2ts "$MOUNT_DIR" || { echo "[错误] ISO 内无 .m2ts"; exit 1; }
      video="$M2TS_INPUT"
      log "[信息] 将使用 m2ts 文件：$video"
      return 0
    fi
    video="$input"
    log "[信息] 识别到视频文件，直接截图：$video"
    return 0
  fi

  if [ -d "$input" ]; then
    log "[提示] 输入为目录，开始智能识别..."
    local iso
    iso=$(find "$input" -maxdepth 1 -type f -iname '*.iso' -printf '%s %p\n' 2>/dev/null | sort -nr | head -1 | cut -d' ' -f2-)
    if [ -n "$iso" ]; then
      mount_iso "$iso"
      find_largest_m2ts "$MOUNT_DIR" || { echo "[错误] ISO 内无 .m2ts"; exit 1; }
      video="$M2TS_INPUT"
      log "[信息] 将使用 m2ts 文件：$video"
      return 0
    fi
    if [ -d "$input/BDMV/STREAM" ]; then
      find_largest_m2ts_in_dir "$input" || { echo "[错误] 目录内无 .m2ts"; exit 1; }
      video="$M2TS_INPUT"
      log "[信息] 将使用 m2ts 文件：$video"
      return 0
    fi
    local -a vids=()
    while IFS= read -r -d '' f; do vids+=("$f"); done < <(
      find "$input" -maxdepth 1 -type f \
        \( -iregex '.*\.\(mkv\|mp4\|mov\|m4v\|avi\|ts\|m2ts\|webm\|wmv\|flv\|rmvb\|mpeg\|mpg\)' \) -print0 2>/dev/null
    )
    if [ ${#vids[@]} -eq 0 ]; then
      echo "[错误] 目录内未发现可用视频文件（无 ISO/BDMV/常见视频）。"; exit 1
    fi
    if [ ${#vids[@]} -eq 1 ]; then
      video="${vids[0]}"; log "[信息] 选定视频：$video"; return 0
    fi
    local -a e01=()
    for f in "${vids[@]}"; do
      local filename="${f##*/}"
      if echo "$filename" | grep -qiE '(e0?1([^0-9]|$)|第一集|第1集|episode.?1|ep.?1|pilot)'; then 
        e01+=("$f")
      fi
    done
    if [ ${#e01[@]} -gt 0 ]; then
      local largest="" max=0 sz
      for f in "${e01[@]}"; do
        sz=$(stat -c%s "$f" 2>/dev/null || echo 0)
        if [ "$sz" -gt "$max" ]; then max=$sz; largest="$f"; fi
      done
      video="$largest"
      log "[信息] 多个视频，优先选择包含第一集的：$video"
      return 0
    fi
    local biggest
    biggest=$(printf '%s\0' "${vids[@]}" | xargs -0 -I{} stat -c '%s %n' "{}" | sort -nr | head -1 | cut -d' ' -f2-)
    video="$biggest"
    log "[提示] 多个视频但未发现第一集，改选体积最大：$video"
    return 0
  fi

  echo "[错误] 无法识别输入路径：$input"; exit 1
}

# —— 字幕选择（保持原样）
find_external_sub(){
  local vpath="$1" dir base
  dir="$(cd "$(dirname "$vpath")" && pwd)"
  base="$(basename "$vpath")"
  base="${base%.*}"

  local cands=()
  for ext in ass srt; do
    for z in "${LANGS_ZH[@]}"; do cands+=("$dir/${base}.$z.$ext" "$dir/${base}-${z}.$ext" "$dir/${base}_$z.$ext"); done
    for e in "${LANGS_EN[@]}"; do cands+=("$dir/${base}.$e.$ext" "$dir/${base}-${e}.$ext" "$dir/${base}_$e.$ext"); done
    cands+=("$dir/${base}.$ext")
  done
  while IFS= read -r -d '' f; do cands+=("$f"); done < <(find "$dir" -maxdepth 1 -type f \( -iname "*.ass" -o -iname "*.srt" \) -iname "*${base}*" -print0)

  declare -A seen
  local best="" score best_score=-1
  for f in "${cands[@]}"; do
    [ -f "$f" ] || continue
    [ -n "${seen[$f]:-}" ] && continue
    seen[$f]=1
    score=0
    local fn="${f##*/}"
    has_lang_token "$fn" "${LANGS_ZH[@]}" && score=100
    has_lang_token "$fn" "${LANGS_EN[@]}" && score=$((score>0?score:50))
    score=$((score+1))
    [ $score -gt $best_score ] && { best_score=$score; best="$f"; }
  done

  if [ -n "$best" ]; then
    SUB_MODE="external"; SUB_FILE="$best"
    if has_lang_token "$best" "${LANGS_ZH[@]}"; then
      SUB_LANG="zh"
    elif has_lang_token "$best" "${LANGS_EN[@]}"; then
      SUB_LANG="en"
    else
      SUB_LANG="unknown"
    fi
    SUB_CODEC="text"
    log "[信息] 选择外挂字幕：$SUB_FILE （语言：$SUB_LANG）"
    return 0
  fi
  return 1
}

pick_internal_sub(){
  local vpath="$1" j parsed
  j=$(ffprobe -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" -v error \
      -select_streams s \
      -show_entries stream=index,codec_name:stream_tags=language,title:stream_disposition=default,forced \
      -of json "$vpath" 2>/dev/null) || true
  [ -z "$j" ] && return 1

  parsed=$(echo "$j" | jq -r '.streams[] | [
      .index,
      (.codec_name // "" | ascii_downcase),
      (.tags.language // "unknown" | ascii_downcase),
      (.tags.title // ""),
      (.disposition.forced // 0),
      (.disposition.default // 0)
    ] | @tsv' 2>/dev/null) || true
  [ -z "$parsed" ] && return 1

  local best_idx="" best_codec="" best_lang_raw="" best_title="" best_forced="" best_default=""
  local last_idx="" last_codec="" last_lang_raw="" last_title="" last_forced="" last_default=""

  pick_by_langset(){
    local want_forced="$1" want_default="$2" want_lang="$3" idx codec lang title forced is_default lang_hint
    while IFS=$'\t' read -r idx codec lang title forced is_default; do
      [ -z "${idx:-}" ] && continue
      lang_hint="$(printf '%s %s' "$lang" "$title")"
      if [[ "$codec" == "hdmv_pgs_subtitle" ]] && has_lang_token "$lang_hint" "chinese" "${LANGS_ZH[@]}"; then
        best_idx="$idx"; best_codec="$codec"; best_lang_raw="$lang"; best_title="$title"; best_forced="$forced"; best_default="$is_default"; return 0
      fi
      if [ "$want_lang" = "zh" ]; then
        has_lang_token "$lang_hint" "${LANGS_ZH[@]}" && [ "$forced" = "$want_forced" ] && [ "$is_default" = "$want_default" ] && {
          best_idx="$idx"; best_codec="$codec"; best_lang_raw="$lang"; best_title="$title"; best_forced="$forced"; best_default="$is_default"; return 0; }
      else
        has_lang_token "$lang_hint" "${LANGS_EN[@]}" && [ "$forced" = "$want_forced" ] && [ "$is_default" = "$want_default" ] && {
          best_idx="$idx"; best_codec="$codec"; best_lang_raw="$lang"; best_title="$title"; best_forced="$forced"; best_default="$is_default"; return 0; }
      fi
    done <<< "$parsed"
    return 1
  }

  pick_by_langset "0" "1" "zh" || pick_by_langset "0" "0" "zh" || \
  pick_by_langset "1" "1" "zh" || pick_by_langset "1" "0" "zh" || {
    pick_by_langset "0" "1" "en" || pick_by_langset "0" "0" "en" || \
    pick_by_langset "1" "1" "en" || pick_by_langset "1" "0" "en" || true
  }

  if [ -z "$best_idx" ]; then
    while IFS=$'\t' read -r idx codec lang title forced is_default; do
      last_idx="$idx"
      last_codec="$codec"
      last_lang_raw="$lang"
      last_title="$title"
      last_forced="$forced"
      last_default="$is_default"
    done <<< "$parsed"
    best_idx="$last_idx"
    best_codec="$last_codec"
    best_lang_raw="$last_lang_raw"
    best_title="$last_title"
    best_forced="$last_forced"
    best_default="$last_default"
  fi

  [ -n "$best_idx" ] || return 1

  if has_lang_token "$best_lang_raw $best_title" "${LANGS_ZH[@]}"; then SUB_LANG="zh"
  elif has_lang_token "$best_lang_raw $best_title" "${LANGS_EN[@]}"; then SUB_LANG="en"
  else SUB_LANG="unknown"; fi

  SUB_MODE="internal"; SUB_SI="$best_idx"; SUB_CODEC="$best_codec"; SUB_TITLE="$best_title"

  local rel=0 gi
  while IFS= read -r gi; do
    [ -z "$gi" ] && continue
    if [ "$gi" = "$SUB_SI" ]; then SUB_REL="$rel"; break; fi
    rel=$((rel+1))
  done < <(ffprobe -v error -select_streams s -show_entries stream=index -of csv=p=0 "$vpath" 2>/dev/null)
  [ -z "$SUB_REL" ] && SUB_REL="0"
  log "[信息] 选择内挂字幕：流索引 $SUB_SI / 字幕序号 $SUB_REL （语言：$SUB_LANG，title：${SUB_TITLE:-无}，default=$best_default，forced=$best_forced，codec：$SUB_CODEC）"
  return 0
}

choose_subtitle(){
  local v="$1"
  SUB_MODE="none"; SUB_FILE=""; SUB_SI=""; SUB_REL=""; SUB_LANG=""; SUB_CODEC=""; SUB_TITLE=""
  if [ "$SUBTITLE_ENABLED" -ne 1 ]; then
    log "[信息] 已禁用字幕挂载与字幕对齐，将直接按时间点截图。"
    return 0
  fi
  if find_external_sub "$v"; then
    [ "$SUB_LANG" = "en" ] && log "[提示] 未找到中文字幕，改用英文外挂字幕。"
    return 0
  fi
  if pick_internal_sub "$v"; then
    [ "$SUB_LANG" = "en" ] && log "[提示] 未找到中文字幕，改用英文内挂字幕。"
    return 0
  fi
  log "[提示] 未找到可用字幕，将仅截图视频画面。"
  return 1
}

extract_internal_text_subtitle(){
  [ "$SUB_MODE" = "internal" ] || return 0
  is_bitmap_sub && return 0

  local tmp_sub
  tmp_sub="$(mktemp -t minfo-sub.XXXXXX.srt)" || return 1
  if ffmpeg -v error -i "$video" -map 0:s:"$SUB_REL" -c:s srt -f srt -y "$tmp_sub" >/dev/null 2>&1; then
    SUB_TEMP_FILE="$tmp_sub"
    SUB_MODE="external"
    SUB_FILE="$tmp_sub"
    SUB_CODEC="text"
    log "[信息] 已提取内挂文本字幕供截图使用：$SUB_FILE"
    return 0
  fi

  rm -f "$tmp_sub"
  log "[警告] 提取内挂文本字幕失败，将继续直接使用内挂字幕流。"
  return 1
}

is_bitmap_sub(){
  case "$(lower "${SUB_CODEC:-}")" in
    hdmv_pgs_subtitle|pgssub|dvd_subtitle|dvb_subtitle|xsub|vobsub) return 0 ;;
    *) return 1 ;;
  esac
}

build_text_sub_filter(){
  if [ "$SUB_MODE" = "external" ]; then
    echo "subtitles='$(escape_squote "$SUB_FILE")'"
  elif [ "$SUB_MODE" = "internal" ]; then
    echo "subtitles='$(escape_squote "$video")':si=${SUB_REL}"
  else
    echo ""
  fi
}

build_text_sub_vf_chain(){
  local pts_shift="$1" subf
  subf="$(build_text_sub_filter)"
  [ -n "$subf" ] || { echo ""; return 0; }
  if [ "$SUB_MODE" = "external" ]; then
    echo "setpts=PTS+${pts_shift}/TB,${subf}"
  else
    echo "$subf"
  fi
}

detect_start_offset(){
  local off
  off=$(ffprobe -v error -select_streams v:0 -show_entries stream=start_time -of default=noprint_wrappers=1:nokey=1 "$video" 2>/dev/null | head -n1)
  [[ "$off" =~ ^[0-9] ]] || off=$(ffprobe -v error -show_entries format=start_time -of default=noprint_wrappers=1:nokey=1 "$video" 2>/dev/null | head -n1)
  START_OFFSET="${off:-0.0}"
}

detect_duration(){
  local d
  d=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$video" 2>/dev/null | head -n1)
  [[ "$d" =~ ^[0-9] ]] || d=$(ffprobe -v error -select_streams v:0 -show_entries stream=duration -of default=noprint_wrappers=1:nokey=1 "$video" 2>/dev/null | head -n1)
  DURATION="${d:-0.0}"
}

# —— PGS 事件（完整版，保持原功能）
pgs_probe_events_internal_window_packets(){
  local start_abs="$1" dur="$2"
  ffprobe -v error -select_streams s:"$SUB_REL" -read_intervals "${start_abs}%+${dur}" \
    -show_packets -show_entries packet=pts_time,duration_time,size \
    -of default=noprint_wrappers=1 "$video" 2>/dev/null
}
packets_to_index_rel(){
  local mode="$1"
  awk -v defdur="$DEFAULT_SUB_DUR" -v off="$START_OFFSET" -v mode="$mode" -v minsz="$PGS_MIN_PKT" '
    /^pts_time=/ {pts=substr($0,index($0,"=")+1)}
    /^duration_time=/ {dur=substr($0,index($0,"=")+1)}
    /^size=/ {sz=substr($0,index($0,"=")+1)
      if (pts!="") {
        if (dur=="" || dur=="N/A") dur=defdur
        if (sz+0 >= minsz) {
          s=pts; e=pts+dur
          if(mode=="internal"){ s-=off; e-=off }
          if(e<0){ pts=""; dur=""; next }
          if(s<0) s=0
          printf "%.6f %.6f\n", s, e
        }
        pts=""; dur=""
      }
    }'
}

# —— 文本字幕事件（完整版）
probe_sub_events_internal_window(){
  local start_abs="$1" dur="$2"
  ffprobe -v error -select_streams s:"$SUB_REL" -read_intervals "${start_abs}%+${dur}" \
    -show_frames -show_entries frame=pkt_pts_time,pkt_duration_time -of default=noprint_wrappers=1 "$video" 2>/dev/null
}
probe_sub_events_external_window(){
  local start="$1" dur="$2"
  ffprobe -v error -read_intervals "${start}%+${dur}" \
    -show_frames -show_entries frame=pkt_pts_time,pkt_duration_time -of default=noprint_wrappers=1 "$SUB_FILE" 2>/dev/null
}
dump_all_sub_events_internal(){
  ffprobe -v error -select_streams s:"$SUB_REL" \
    -show_frames -show_entries frame=pkt_pts_time,pkt_duration_time -of default=noprint_wrappers=1 "$video" 2>/dev/null
}
dump_all_sub_events_external(){
  ffprobe -v error -show_frames -show_entries frame=pkt_pts_time,pkt_duration_time -of default=noprint_wrappers=1 "$SUB_FILE" 2>/dev/null
}
frames_to_index_rel(){
  local mode="$1"
  awk -v defdur="$DEFAULT_SUB_DUR" -v off="$START_OFFSET" -v mode="$mode" '
    /^pkt_pts_time=/ {pts=substr($0,index($0,"=")+1)}
    /^pkt_duration_time=/ {
      dur=substr($0,index($0,"=")+1)
      if (dur=="" || dur=="N/A") dur=defdur
      if (pts!="") {
        s=pts; e=pts+dur
        if(mode=="internal"){ s-=off; e-=off }
        if(e<0) next
        if(s<0) s=0
        printf "%.6f %.6f\n", s, e
        pts=""; dur=""
      }
    }
    END {
      if (pts!="") {
        s=pts; e=pts+defdur
        if(mode=="internal"){ s-=off; e-=off }
        if(e>=0){
          if(s<0) s=0
          printf "%.6f %.6f\n", s, e
        }
      }
    }'
}

# —— 就近对齐（完整版，保持原功能）
pgs_nearest_expand(){
  local T="$1"
  local spans=(60 120 240 480 900)
  local best_t="" best_dist=1e18
  local Tabs win_s win_d out mid dist s e
  for sp in "${spans[@]}"; do
    Tabs=$(fadd "$T" "$START_OFFSET")
    win_s=$(fsub "$Tabs" "$sp"); win_s=$(awk -v v="$win_s" 'BEGIN{print (v<0?0:v)}')
    win_d=$(fadd "$sp" "$sp")
    out=$(pgs_probe_events_internal_window_packets "$win_s" "$win_d" | packets_to_index_rel internal)
    [ -z "$out" ] && continue
    while read -r s e; do
      [ -z "$s" ] && continue
      mid=$(awk -v s="$s" -v e="$e" 'BEGIN{printf "%.6f", s+(e-s)/2}')
      dist=$(awk -v a="$mid" -v b="$T" 'BEGIN{d=a-b; if(d<0)d=-d; printf "%.6f", d}')
      awk -v d="$dist" -v bd="$best_dist" 'BEGIN{exit !(d<bd)}' && { best_dist="$dist"; best_t="$mid"; }
    done <<< "$out"
    [ -n "$best_t" ] && break
  done
  if [ -n "$best_t" ]; then echo "$(clamp_0_dur "$best_t")"; else echo "$T"; fi
}

snap_window(){
  local T="$1" eps="$SUB_SNAP_EPS" win_s win_d
  [ "$SUB_MODE" = "none" ] && { echo "$T"; return 0; }

  if [ "$SUB_MODE" = "internal" ]; then
    if is_bitmap_sub; then
      local Tabs; Tabs=$(fadd "$T" "$START_OFFSET")
      win_s=$(fsub "$Tabs" "$SEARCH_BACK"); win_s=$(awk -v v="$win_s" 'BEGIN{print (v<0?0:v)}')
      win_d=$(fadd "$SEARCH_BACK" "$SEARCH_FWD")
      pgs_probe_events_internal_window_packets "$win_s" "$win_d" | packets_to_index_rel internal | \
      awk -v T="$T" -v eps="$eps" '
        { s=$1; e=$2;
          if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; exit }
          if (!p && s>=T){ printf "%.6f\n", s+eps; p=1; exit }
        }'
    else
      local Tabs; Tabs=$(fadd "$T" "$START_OFFSET")
      win_s=$(fsub "$Tabs" "$SEARCH_BACK"); win_s=$(awk -v v="$win_s" 'BEGIN{print (v<0?0:v)}')
      win_d=$(fadd "$SEARCH_BACK" "$SEARCH_FWD")
      probe_sub_events_internal_window "$win_s" "$win_d" | frames_to_index_rel internal | \
      awk -v T="$T" -v eps="$eps" '
        { s=$1; e=$2;
          if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; exit }
          if (!p && s>=T){ printf "%.6f\n", s+eps; p=1; exit }
        }'
    fi
  else
    win_s=$(fsub "$T" "$SEARCH_BACK"); win_s=$(awk -v v="$win_s" 'BEGIN{print (v<0?0:v)}')
    win_d=$(fadd "$SEARCH_BACK" "$SEARCH_FWD")
    probe_sub_events_external_window "$win_s" "$win_d" | frames_to_index_rel external | \
    awk -v T="$T" -v eps="$eps" '
      { s=$1; e=$2;
        if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; exit }
        if (!p && s>=T){ printf "%.6f\n", s+eps; p=1; exit }
      }'
  fi
}
snap_from_index(){
  local T="$1" eps="$SUB_SNAP_EPS"
  [ -n "$SUB_IDX" ] && [ -s "$SUB_IDX" ] || { echo "$T"; return 1; }
  awk -v T="$T" -v eps="$eps" '
    BEGIN{bestAfterS=-1; lastS=-1; lastE=-1;}
    { s=$1; e=$2;
      if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; found=1; exit }
      if (bestAfterS<0 && s>=T){ bestAfterS=s; bestAfterE=e }
      if (s<=T){ lastS=s; lastE=e }
    }
    END{
      if (!found){
        if (bestAfterS>=0) printf "%.6f\n", bestAfterS+eps;
        else if (lastS>=0) printf "%.6f\n", (lastE-eps>lastS? lastE-eps : lastS+eps);
        else printf "%.6f\n", T;
      }
    }' "$SUB_IDX"
}
align_to_subtitle(){
  local T="$1" cand=""
  [ "$SUB_MODE" = "none" ] && { echo "$T"; return 0; }

  cand="$(snap_window "$T")"
  if [ -n "${cand:-}" ]; then
    cand="$(clamp_0_dur "$cand")"
    log "[对齐] 请求 $(sec_to_hms_ms "$T") → 就近/扩窗字幕 $(sec_to_hms_ms "$cand")"
    echo "$cand"; return 0
  fi

  if [ "$SUB_MODE" = "internal" ]; then
    local Tabs win_s win_d
    Tabs=$(fadd "$T" "$START_OFFSET")
    win_s=$(fsub "$Tabs" 60); win_s=$(awk -v v="$win_s" 'BEGIN{print (v<0?0:v)}')
    win_d=120
    if is_bitmap_sub; then
      cand=$(pgs_probe_events_internal_window_packets "$win_s" "$win_d" | packets_to_index_rel internal | \
        awk -v T="$T" -v eps="$SUB_SNAP_EPS" '
          { s=$1; e=$2;
            if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; exit }
            if (!p && s>=T){ printf "%.6f\n", s+eps; p=1; exit }
          }')
    else
      cand=$(probe_sub_events_internal_window "$win_s" "$win_d" | frames_to_index_rel internal | \
        awk -v T="$T" -v eps="$SUB_SNAP_EPS" '
          { s=$1; e=$2;
            if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; exit }
            if (!p && s>=T){ printf "%.6f\n", s+eps; p=1; exit }
          }')
    fi
  else
    local win_s win_d; win_s=$(fsub "$T" 60); win_s=$(awk -v v="$win_s" 'BEGIN{print (v<0?0:v)}'); win_d=120
    cand=$(probe_sub_events_external_window "$win_s" "$win_d" | frames_to_index_rel external | \
      awk -v T="$T" -v eps="$SUB_SNAP_EPS" '
        { s=$1; e=$2;
          if (T>=s && T<=e){ c=T; if(c<s+eps)c=s+eps; if(c>e-eps)c=e-eps; printf "%.6f\n", c; exit }
          if (!p && s>=T){ printf "%.6f\n", s+eps; p=1; exit }
        }')
  fi
  if [ -n "${cand:-}" ]; then
    cand="$(clamp_0_dur "$cand")"
    log "[对齐] 请求 $(sec_to_hms_ms "$T") → 扩窗字幕 $(sec_to_hms_ms "$cand")"
    echo "$cand"; return 0
  fi

  if [ "$SUB_MODE" = "internal" ] && is_bitmap_sub; then
    cand="$(pgs_nearest_expand "$T")"
    if [ -n "${cand:-}" ] && awk -v a="$cand" -v b="$T" 'BEGIN{d=a-b; if(d<0)d=-d; exit !(d<=1200)}'; then
      cand="$(clamp_0_dur "$cand")"
      log "[对齐] 请求 $(sec_to_hms_ms "$T") → 渐进扩窗 $(sec_to_hms_ms "$cand")"
      echo "$cand"; return 0
    fi
  fi

  [ -z "${SUB_IDX:-}" ] && build_sub_index >/dev/null 2>&1 || true
  cand="$(snap_from_index "$T")"
  cand="$(clamp_0_dur "$cand")"
  if float_diff_gt "$cand" "$T"; then
    log "[对齐] 请求 $(sec_to_hms_ms "$T") → 全片索引 $(sec_to_hms_ms "$cand")"
  else
    log "[提示] 周边及全片均未找到字幕事件，按原时间点截图：$(sec_to_hms_ms "$T")"
  fi
  echo "$cand"
}

build_sub_index(){
  if [ "$SUB_MODE" = "none" ] || is_bitmap_sub; then
    SUB_IDX=""; return 1
  fi
  SUB_IDX="$(mktemp -t subidx.XXXXXX)"
  if [ "$SUB_MODE" = "internal" ]; then
    dump_all_sub_events_internal | frames_to_index_rel internal | sort -n -k1,1 > "$SUB_IDX"
  else
    dump_all_sub_events_external | frames_to_index_rel external | sort -n -k1,1 > "$SUB_IDX"
  fi
  [ -s "$SUB_IDX" ] && { log "[信息] 已建立字幕索引（文字字幕）。"; return 0; }
  rm -f "$SUB_IDX"; SUB_IDX=""; return 1
}

# —— JPG截图函数（保持原功能，针对JPG优化）
do_screenshot(){
  local t_aligned="$1" path="$2" err
  local tnum="${t_aligned%.*}"; [[ "$tnum" =~ ^[0-9]+$ ]] || tnum=0
  local coarse_sec
  if [ "$SUB_MODE" = "internal" ] && is_bitmap_sub; then
    coarse_sec=$(( tnum > COARSE_BACK_PGS ? tnum - COARSE_BACK_PGS : 0 ))
  else
    coarse_sec=$(( tnum > COARSE_BACK_TEXT ? tnum - COARSE_BACK_TEXT : 0 ))
  fi
  local fine_sec="$(fsub "$t_aligned" "$coarse_sec")"
  local coarse_hms="$(sec_to_hms "$coarse_sec")"

  if [ "$SUB_MODE" = "internal" ] && is_bitmap_sub; then
    # 位图字幕：双输入 overlay，输出JPG
    err=$(ffmpeg -v error -fflags +genpts -ss "$coarse_hms" -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" \
      -i "$video" -ss "$fine_sec" \
      -filter_complex "[0:v:0][0:s:${SUB_REL}]overlay=(W-w)/2:(H-h-10)" \
      -frames:v 1 -c:v mjpeg -q:v "$JPG_QUALITY" -y "$path" 2>&1)
  else
    local subvf; subvf="$(build_text_sub_vf_chain "$t_aligned")"
    if [ -n "$subvf" ]; then
      # 文本字幕：只用 subtitles，输出JPG
      err=$(ffmpeg -v error -fflags +genpts -ss "$coarse_hms" -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" \
        -i "$video" -ss "$fine_sec" -map 0:v:0 -y -frames:v 1 -vf "$subvf" \
        -c:v mjpeg -q:v "$JPG_QUALITY" "$path" 2>&1)
    else
      # 无字幕：不加任何滤镜，输出JPG
      err=$(ffmpeg -v error -fflags +genpts -ss "$coarse_hms" -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" \
        -i "$video" -ss "$fine_sec" -map 0:v:0 -y -frames:v 1 \
        -c:v mjpeg -q:v "$JPG_QUALITY" "$path" 2>&1)
    fi
  fi
  local ret=$?
  if [ $ret -ne 0 ]; then failed_files+=("$(basename "$path")"); failed_reasons+=("$err"); fi
  return $ret
}

do_screenshot_reencode(){
  local t_aligned="$1" path="$2" err
  local tnum="${t_aligned%.*}"; [[ "$tnum" =~ ^[0-9]+$ ]] || tnum=0
  local coarse_sec
  if [ "$SUB_MODE" = "internal" ] && is_bitmap_sub; then
    coarse_sec=$(( tnum > COARSE_BACK_PGS ? tnum - COARSE_BACK_PGS : 0 ))
  else
    coarse_sec=$(( tnum > COARSE_BACK_TEXT ? tnum - COARSE_BACK_TEXT : 0 ))
  fi
  local fine_sec="$(fsub "$t_aligned" "$coarse_sec")"
  local coarse_hms="$(sec_to_hms "$coarse_sec")"

  # JPG重拍时降低质量确保小于10MB
  local low_quality=$((JPG_QUALITY - 15))  # 降低15个质量等级
  [ $low_quality -lt 50 ] && low_quality=50  # 最低质量不低于50

  if [ "$SUB_MODE" = "internal" ] && is_bitmap_sub; then
    # 位图字幕重拍：降低质量
    err=$(ffmpeg -v error -fflags +genpts -ss "$coarse_hms" -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" \
      -i "$video" -ss "$fine_sec" \
      -filter_complex "[0:v:0][0:s:${SUB_REL}]overlay=(W-w)/2:(H-h-10)" \
      -frames:v 1 -c:v mjpeg -q:v "$low_quality" -y "$path" 2>&1)
  else
    local subvf; subvf="$(build_text_sub_vf_chain "$t_aligned")"
    if [ -n "$subvf" ]; then
      # 文本字幕重拍：降低质量
      err=$(ffmpeg -v error -fflags +genpts -ss "$coarse_hms" -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" \
        -i "$video" -ss "$fine_sec" -map 0:v:0 -frames:v 1 -y -vf "$subvf" \
        -c:v mjpeg -q:v "$low_quality" "$path" 2>&1)
    else
      # 无字幕重拍：降低质量
      err=$(ffmpeg -v error -fflags +genpts -ss "$coarse_hms" -probesize "$PROBESIZE" -analyzeduration "$ANALYZE" \
        -i "$video" -ss "$fine_sec" -map 0:v:0 -frames:v 1 -y \
        -c:v mjpeg -q:v "$low_quality" "$path" 2>&1)
    fi
  fi
  local ret=$?
  if [ $ret -ne 0 ]; then failed_files+=("$(basename "$path")"); failed_reasons+=("$err"); fi
  return $ret
}

# ---------------- 主流程 ----------------
require_commands

validate_arguments "$@"

input_path="$1"; outdir="$2"; shift 2

# 根据输入选择实际视频文件
video=""
select_input_from_arg "$input_path"

# 字幕选择与时长检测
choose_subtitle "$video" || true
extract_internal_text_subtitle || true
detect_start_offset
detect_duration
DURATION_HMS="$(sec_to_hms ${DURATION%.*})"

log "[信息] 高速JPG截图模式 | 质量参数：$JPG_QUALITY"
log "[信息] 容器起始偏移：${START_OFFSET}s | 影片总时长：${DURATION_HMS}"

log "[信息] 清空截图目录: $outdir"
rm -rf "${outdir:?}"/*

declare -a TARGET_SECONDS=()
if [ "$#" -gt 0 ]; then
  if [ "$SUB_MODE" = "none" ]; then
    log "[信息] 已提供时间点：将直接按时间点截图"
  else
    log "[信息] 已提供时间点：将对齐到附近有字幕后再截图"
  fi
  for tp in "$@"; do TARGET_SECONDS+=( "$(hms_to_seconds "$tp")" ); done
else
  if [ "$SUB_MODE" = "none" ]; then
    log "[信息] 未提供时间点：自动按 20% / 40% / 60% / 80% 选取截图"
  else
    log "[信息] 未提供时间点：自动按 20% / 40% / 60% / 80% 选取并确保有字幕"
  fi
  if awk -v d="$DURATION" 'BEGIN{exit !(d>0)}'; then
    for p in "${AUTO_PERC[@]}"; do
      t=$(awk -v d="$DURATION" -v p="$p" 'BEGIN{t=d*p; if(t<5)t=5; if(t>d-5)t=d-5; printf "%.3f",t}')
      TARGET_SECONDS+=( "$t" )
    done
  else
    log "[警告] 时长未知，退化为固定时间点：300/900/1800/2700 秒"
    TARGET_SECONDS=(300 900 1800 2700)
  fi
  if [ "$SUB_MODE" != "none" ]; then
    # 为自动模式建立字幕索引提升对齐精度
    build_sub_index >/dev/null 2>&1 || true
  fi
fi

start_time=$(date +%s)
idx=0
for T_req in "${TARGET_SECONDS[@]}"; do
  idx=$((idx+1))
  if [ "$SUB_MODE" = "none" ]; then
    T_align="$T_req"
  else
    T_align="$(align_to_subtitle "$T_req" | tail -n1)"
  fi
  [ -z "$T_align" ] && T_align="$T_req"
  [ "${T_align:0:1}" = "." ] && T_align="0${T_align}"

  if [ "$#" -gt 0 ]; then
    minpart=$(( ${T_req%.*} / 60 ))
    filename="${minpart}min.jpg"
  else
    filename="auto${idx}.jpg"
  fi
  filepath="$outdir/$filename"

  log "[信息] 截图: $(sec_to_hms_ms "$T_req") → 实际 $(sec_to_hms_ms "$T_align") -> $filename"

  # 执行截图
  do_screenshot "$T_align" "$filepath"
  if [ $? -ne 0 ]; then ((fail_count++)); continue; fi

  # 检查文件大小，如超过10MB则重拍
  size_bytes=$(stat -c%s "$filepath")
  size_mb=$(echo "scale=2; $size_bytes/1024/1024" | bc)
  if (( $(echo "$size_mb > 10" | bc -l) )); then
    log "[提示] $filename 大小 ${size_mb}MB，重拍降低质量..."
    do_screenshot_reencode "$T_align" "$filepath"
    [ $? -eq 0 ] && ((success_count++)) || ((fail_count++))
  else
    ((success_count++))
  fi
done

end_time=$(date +%s); elapsed=$((end_time-start_time)); minutes=$((elapsed/60)); seconds=$((elapsed%60))

echo
echo "===== 任务完成 ====="
echo "成功: ${success_count} 张 | 失败: ${fail_count} 张"
echo "总耗时: ${minutes}分${seconds}秒"
echo "输出格式: JPG (质量: ${JPG_QUALITY})"

if [ $fail_count -gt 0 ]; then
  echo; echo "===== 失败详情 ====="
  for i in "${!failed_files[@]}"; do
    echo "[失败] 文件: ${failed_files[$i]}"; echo "原因: ${failed_reasons[$i]}"; echo
  done
fi
