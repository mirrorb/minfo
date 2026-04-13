// Package screenshot 提供截图流程共用的格式化、语言判断和索引辅助函数。

package screenshot

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const progressLogPrefix = "[进度]"

// logs 返回当前截图运行器已经累积的完整日志文本。
func (r *screenshotRunner) logs() string {
	return strings.TrimSpace(strings.Join(r.logLines, "\n"))
}

// logf 会把一条格式化日志写入运行器缓存，并在存在实时回调时立即推送。
func (r *screenshotRunner) logf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	r.logLines = append(r.logLines, line)
	if r.logHandler != nil {
		r.logHandler(line)
	}
}

// logProgress 会写入一条稳定格式的进度日志，供上层推导阶段型进度。
func (r *screenshotRunner) logProgress(stage string, current, total int, detail string) {
	r.logf("%s %s %d/%d: %s", progressLogPrefix, strings.TrimSpace(stage), current, total, strings.TrimSpace(detail))
}

// logProgressPercent 会写入一条带百分比的进度日志，适合外部工具实时进度。
func (r *screenshotRunner) logProgressPercent(stage string, percent float64, detail string) {
	r.logf("%s %s %s%%: %s", progressLogPrefix, strings.TrimSpace(stage), formatProgressPercent(percent), strings.TrimSpace(detail))
}

// EmitProgressLog 会通过实时日志回调输出一条统一格式的进度日志。
func EmitProgressLog(onLog LogHandler, stage string, current, total int, detail string) {
	if onLog == nil {
		return
	}
	onLog(fmt.Sprintf("%s %s %d/%d: %s", progressLogPrefix, strings.TrimSpace(stage), current, total, strings.TrimSpace(detail)))
}

// EmitProgressPercentLog 会通过实时日志回调输出一条带百分比的进度日志。
func EmitProgressPercentLog(onLog LogHandler, stage string, percent float64, detail string) {
	if onLog == nil {
		return
	}
	onLog(fmt.Sprintf("%s %s %s%%: %s", progressLogPrefix, strings.TrimSpace(stage), formatProgressPercent(percent), strings.TrimSpace(detail)))
}

func clampProgressPercent(percent float64) float64 {
	switch {
	case percent < 0:
		return 0
	case percent > 100:
		return 100
	default:
		return math.Round(percent*10) / 10
	}
}

func formatProgressPercent(percent float64) string {
	clamped := clampProgressPercent(percent)
	if math.Abs(clamped-math.Round(clamped)) < 0.05 {
		return strconv.Itoa(int(math.Round(clamped)))
	}
	return fmt.Sprintf("%.1f", clamped)
}

// subtitleCodecFromPath 根据字幕文件扩展名推断 ffmpeg 使用的 codec 名称。
func subtitleCodecFromPath(path string) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".ass":
		return "ass"
	case ".ssa":
		return "ssa"
	case ".srt":
		return "subrip"
	default:
		return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(filepath.Ext(path))), ".")
	}
}

// subtitleFormatLabel 把字幕 codec 转换成更适合日志展示的格式名称。
func subtitleFormatLabel(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "subrip", "srt":
		return "SRT/SubRip"
	case "ass":
		return "ASS"
	case "ssa":
		return "SSA"
	case "hdmv_pgs_subtitle", "pgssub":
		return "PGS"
	case "dvd_subtitle":
		return "DVD Subtitle"
	case "dvb_subtitle":
		return "DVB Subtitle"
	case "xsub":
		return "XSub"
	case "vobsub":
		return "VobSub"
	case "":
		return "未知"
	default:
		return strings.ToUpper(codec)
	}
}

// subtitleHandlingLabel 返回当前字幕 codec 在截图流程里的处理方式说明。
func subtitleHandlingLabel(codec string) string {
	switch {
	case bitmapSubtitleKindFromCodec(codec) == bitmapSubtitlePGS:
		return "PGS位图"
	case bitmapSubtitleKindFromCodec(codec) == bitmapSubtitleDVD:
		return "DVD位图"
	case isUnsupportedBitmapSubtitleCodec(codec):
		return "暂不支持位图"
	default:
		return "文字字幕"
	}
}

// isASSLikeTextSubtitleCodec 会判断文字字幕 codec 是否为 ASS/SSA 这类需要保留样式的字幕格式。
func isASSLikeTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa":
		return true
	default:
		return false
	}
}

// isSupportedTextSubtitleCodec 会判断当前文字字幕 codec 是否在服务端允许范围内。
func isSupportedTextSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass", "ssa", "subrip", "srt":
		return true
	default:
		return false
	}
}

// isKnownTextSubtitleExtension 会判断文件扩展名是否属于当前识别范围内的文本字幕格式。
func isKnownTextSubtitleExtension(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".ass", ".ssa", ".srt", ".vtt", ".webvtt", ".ttml", ".dfxp", ".smi", ".sami", ".stl", ".sbv", ".lrc":
		return true
	default:
		return false
	}
}

// isSupportedTextSubtitlePath 会判断外挂文本字幕文件是否在当前允许范围内。
func isSupportedTextSubtitlePath(path string) bool {
	if !isKnownTextSubtitleExtension(filepath.Ext(path)) {
		return false
	}
	return isSupportedTextSubtitleCodec(subtitleCodecFromPath(path))
}

// parseRequestedTimestamps 把请求里的 HH:MM:SS 时间点列表转换为秒数切片。
func parseRequestedTimestamps(values []string) ([]float64, error) {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		parsed, err := parseClockTimestamp(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

// parseClockTimestamp 把单个 HH:MM:SS 时间戳解析成秒数。
func parseClockTimestamp(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, err
	}

	return float64(hours*3600 + minutes*60 + seconds), nil
}

// readInterval 按 ffprobe -read_intervals 需要的格式拼接起始时间和持续时长。
func readInterval(start, duration float64) string {
	return fmt.Sprintf("%s%%+%s", formatFloat(start), formatFloat(duration))
}

// formatFloat 把浮点数格式化为保留三位小数的字符串。
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

// formatTimestamp 把整秒值格式化为 HH:MM:SS。
func formatTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// secToHMS 把秒数向下取整后格式化为 HH:MM:SS。
func secToHMS(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	return formatTimestamp(total)
}

// secToFilenameStamp 把秒数格式化为截图文件名使用的时间戳片段。
func secToFilenameStamp(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	remain := total % 60
	return fmt.Sprintf("%02dh%02dm%02ds", hours, minutes, remain)
}

// secToHMSMS 把秒数格式化为带毫秒的 HH:MM:SS.mmm。
func secToHMSMS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := int(seconds / 3600)
	minutes := int(math.Mod(seconds, 3600) / 60)
	remain := seconds - float64(hours*3600+minutes*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", hours, minutes, remain)
}

// snapFromSpans 根据字幕区间把时间点吸附到当前区间或最近的后续区间。
func snapFromSpans(target float64, spans []subtitleSpan, epsilon float64) (float64, bool) {
	for _, span := range spans {
		if target >= span.Start && target <= span.End {
			return clampInsideSpan(target, span, epsilon), true
		}
		if span.Start >= target {
			return clampInsideSpan(span.Start+epsilon, span, epsilon), true
		}
	}
	return 0, false
}

// snapFromBitmapSpans 根据位图字幕区间返回适合截图的代表时间点。
func snapFromBitmapSpans(target float64, spans []subtitleSpan, epsilon float64) (float64, bool) {
	for _, span := range spans {
		if target >= span.Start && target <= span.End {
			return bitmapSnapPoint(span, epsilon), true
		}
		if span.Start >= target {
			return bitmapSnapPoint(span, epsilon), true
		}
	}
	return 0, false
}

// snapFromIndex 使用预先建立的字幕索引把时间点吸附到最合适的区间。
func snapFromIndex(target float64, spans []subtitleSpan, epsilon float64) (float64, bool) {
	if len(spans) == 0 {
		return target, false
	}

	bestAfterIndex := -1
	lastBeforeIndex := -1
	for index, span := range spans {
		if target >= span.Start && target <= span.End {
			return clampInsideSpan(target, span, epsilon), true
		}
		if bestAfterIndex == -1 && span.Start >= target {
			bestAfterIndex = index
		}
		if span.Start <= target {
			lastBeforeIndex = index
		}
	}

	if bestAfterIndex >= 0 {
		return clampInsideSpan(spans[bestAfterIndex].Start+epsilon, spans[bestAfterIndex], epsilon), true
	}
	if lastBeforeIndex >= 0 {
		span := spans[lastBeforeIndex]
		return clampInsideSpan(span.End-epsilon, span, epsilon), true
	}
	return target, false
}

// clampInsideSpan 把时间点限制在字幕区间内部，并预留 epsilon 安全边距。
func clampInsideSpan(value float64, span subtitleSpan, epsilon float64) float64 {
	if span.End <= span.Start {
		return span.Start
	}

	minValue := span.Start + epsilon
	maxValue := span.End - epsilon
	if maxValue < minValue {
		mid := span.Start + (span.End-span.Start)/2
		return mid
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// bitmapSnapPoint 返回位图字幕区间更适合截图的中点时间。
func bitmapSnapPoint(span subtitleSpan, epsilon float64) float64 {
	return clampInsideSpan(span.Start+(span.End-span.Start)/2, span, epsilon)
}

// bitmapCandidateKey 把候选时间点归一化为毫秒级字符串键，便于去重和缓存。
func bitmapCandidateKey(value float64) string {
	return strconv.FormatInt(int64(math.Round(value*1000)), 10)
}

// clampJPGQScale 将 JPG qscale 限制在 ffmpeg 可接受的范围内。
func clampJPGQScale(value int) int {
	if value < 1 {
		return 1
	}
	if value > 31 {
		return 31
	}
	return value
}

// fallbackJPGQScale 为超大 JPG 重拍场景选择更保守的 qscale。
func fallbackJPGQScale(value int) int {
	value = clampJPGQScale(value)
	value += 2
	if value > 6 {
		return 6
	}
	return value
}

// mergeNearbySubtitleSpans 会合并邻近字幕区间，并保留后续流程仍然需要的有效信息。
func mergeNearbySubtitleSpans(spans []subtitleSpan, maxGap float64) []subtitleSpan {
	if len(spans) <= 1 {
		return spans
	}
	if maxGap < 0 {
		maxGap = 0
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})

	merged := make([]subtitleSpan, 0, len(spans))
	current := spans[0]
	for _, span := range spans[1:] {
		if span.Start <= current.End+maxGap {
			if span.End > current.End {
				current.End = span.End
			}
			continue
		}
		merged = append(merged, current)
		current = span
	}
	merged = append(merged, current)
	return merged
}

// tracksHaveClassifiedLang 判断原始字幕轨中是否已有可识别的目标语言标签。
func tracksHaveClassifiedLang(tracks []subtitleTrack) bool {
	for _, track := range tracks {
		if classifySubtitleLanguage(strings.TrimSpace(track.Language+" "+track.Title)) != "" {
			return true
		}
	}
	return false
}

// helperTracksHaveClassifiedLang 判断 bdsub 返回的轨道里是否已有可识别的目标语言标签。
func helperTracksHaveClassifiedLang(tracks []blurayHelperTrack) bool {
	for _, track := range tracks {
		if classifySubtitleLanguage(track.Lang) != "" {
			return true
		}
	}
	return false
}

// blurayHelperNeedsFFprobe 判断当前 bdsub 元数据是否仍然需要 ffprobe 补充。
func blurayHelperNeedsFFprobe(raw []subtitleTrack, helper []blurayHelperTrack) bool {
	if len(helper) == 0 {
		return true
	}

	helperByPID := make(map[int]blurayHelperTrack, len(helper))
	for _, track := range helper {
		helperByPID[track.PID] = track
	}

	for index, track := range raw {
		helperMeta := blurayHelperTrack{}
		helperMetaOK := false
		if pid, ok := normalizeStreamPID(track.StreamID); ok {
			if meta, exists := helperByPID[pid]; exists {
				helperMeta = meta
				helperMetaOK = true
			}
		}
		if !helperMetaOK && len(helper) == len(raw) && index < len(helper) {
			helperMeta = helper[index]
			helperMetaOK = true
		}
		if !helperMetaOK {
			return true
		}
		if subtitleNeedsBluraySupplement(helperMeta.Lang, "") {
			return true
		}
	}

	return false
}

// blurayHelperHasPayloadBytes 判断当前 bdsub 结果是否已经补充了可用于热路径排序的 payload_bytes。
func blurayHelperHasPayloadBytes(result blurayHelperResult) bool {
	return result.BitrateScanned || result.BitrateMode == "payload-bytes" || result.BitrateMode == "sampled-payload-bytes"
}

// blurayHelperNeedsPayloadScan 判断当前蓝光 PGS 是否真的需要再次调用 bdsub 补充 payload_bytes。
func blurayHelperNeedsPayloadScan(raw []subtitleTrack, helperResult blurayHelperResult, helper []blurayHelperTrack, bluray []subtitleTrack, blurayMode string) bool {
	if blurayHelperHasPayloadBytes(helperResult) || len(helper) == 0 {
		return false
	}

	helperByPID := make(map[int]blurayHelperTrack, len(helper))
	for _, track := range helper {
		helperByPID[track.PID] = track
	}

	langCounts := make(map[string]int, 4)
	for index, track := range raw {
		if bitmapSubtitleKindFromCodec(track.Codec) != bitmapSubtitlePGS {
			continue
		}

		langForPick := track.Language
		titleForPick := track.Title
		helperMetaOK := false

		if pid, ok := normalizeStreamPID(track.StreamID); ok {
			if meta, exists := helperByPID[pid]; exists {
				helperMetaOK = true
				if strings.TrimSpace(meta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
				}
			}
		}
		if !helperMetaOK && len(helper) == len(raw) && index < len(helper) {
			helperMetaOK = true
			if strings.TrimSpace(helper[index].Lang) != "" {
				langForPick = strings.ToLower(strings.TrimSpace(helper[index].Lang))
			}
		}
		if !helperMetaOK {
			continue
		}

		if (blurayMode == "ffprobe" || blurayMode == "helper+ffprobe") && index < len(bluray) {
			needsSupplement := blurayMode == "ffprobe" || subtitleNeedsBluraySupplement(langForPick, titleForPick)
			if needsSupplement {
				if bluray[index].Language != "" && bluray[index].Language != "unknown" {
					langForPick = bluray[index].Language
				}
				if bluray[index].Title != "" {
					titleForPick = bluray[index].Title
				}
			} else if strings.TrimSpace(titleForPick) == "" && bluray[index].Title != "" {
				titleForPick = bluray[index].Title
			}
		}

		langClass := classifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass == "" {
			continue
		}
		langCounts[langClass]++
		if langCounts[langClass] > 1 {
			return true
		}
	}

	return false
}

// subtitleNeedsBluraySupplement 判断当前语言信息是否仍然完全不足以参与蓝光字幕选轨。
func subtitleNeedsBluraySupplement(lang, title string) bool {
	class := classifySubtitleLanguage(strings.TrimSpace(lang + " " + title))
	return class == ""
}

// looksLikeDVDSource 通过路径特征判断输入是否看起来像 DVD VIDEO_TS 源。
func looksLikeDVDSource(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	base := strings.ToLower(filepath.Base(lower))
	parent := filepath.Base(filepath.Dir(path))
	if strings.Contains(lower, "/video_ts/") || strings.EqualFold(parent, "VIDEO_TS") {
		return true
	}
	if strings.HasSuffix(base, ".ifo") || strings.HasSuffix(base, ".vob") || strings.HasSuffix(base, ".bup") {
		return strings.EqualFold(base, "video_ts.ifo") ||
			strings.EqualFold(base, "video_ts.vob") ||
			strings.EqualFold(base, "video_ts.bup") ||
			strings.HasPrefix(base, "vts_")
	}
	return false
}

// classifySubtitleLanguage 把语言和标题文本归类为 zh-Hans、zh-Hant、zh、en 或空值。
func classifySubtitleLanguage(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return ""
	}

	if containsAnyToken(value, langZHHansTokens) {
		return "zh-Hans"
	}
	if containsAnyToken(value, langZHHantTokens) {
		return "zh-Hant"
	}
	if containsAnyToken(value, langZHTokens) {
		return "zh"
	}
	if containsAnyToken(value, langENTokens) {
		return "en"
	}
	return ""
}

// containsAnyToken 会判断任意令牌是否包含目标内容。
func containsAnyToken(haystack string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(haystack, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

// subtitleLanguageScore 返回字幕语言优先级分数。
func subtitleLanguageScore(lang string) int {
	switch lang {
	case "zh-Hans":
		return 400
	case "zh-Hant":
		return 300
	case "zh":
		return 250
	case "en":
		return 200
	default:
		return 0
	}
}

// subtitleDispositionScore 根据 forced 和 default 标记计算附加优先级。
func subtitleDispositionScore(forced, isDefault int) int {
	switch {
	case forced == 0 && isDefault == 1:
		return 40
	case forced == 0 && isDefault == 0:
		return 30
	case forced == 1 && isDefault == 1:
		return 20
	default:
		return 10
	}
}

// preferPreferredSubtitleRank 比较两个字幕候选排名，并判断 current 是否更优。
func preferPreferredSubtitleRank(current, best preferredSubtitleRank) bool {
	if current.LangScore != best.LangScore {
		return current.LangScore > best.LangScore
	}
	if current.LangClass != "" &&
		current.LangClass == best.LangClass &&
		current.BitmapKind == bitmapSubtitlePGS &&
		best.BitmapKind == bitmapSubtitlePGS &&
		(current.UsePayloadBytes || best.UsePayloadBytes) &&
		current.PayloadBytes != best.PayloadBytes {
		return current.PayloadBytes > best.PayloadBytes
	}
	if current.LangClass != "" &&
		current.LangClass == best.LangClass &&
		current.BitmapKind == bitmapSubtitlePGS &&
		best.BitmapKind == bitmapSubtitlePGS &&
		(current.UseBitrate || best.UseBitrate) &&
		current.Bitrate != best.Bitrate {
		return current.Bitrate > best.Bitrate
	}
	if current.DispositionScore != best.DispositionScore {
		return current.DispositionScore > best.DispositionScore
	}
	switch {
	case current.PIDOK && best.PIDOK:
		return current.PID < best.PID
	case current.PIDOK:
		return true
	default:
		return false
	}
}

// firstSubtitleLanguage 从标签集合中读取最可能的语言字段。
func firstSubtitleLanguage(tags map[string]interface{}) string {
	for _, key := range []string{"language", "lang"} {
		if value := lookupTag(tags, key); value != "" {
			return value
		}
	}
	for _, prefix := range []string{"language-", "language_", "lang-", "lang_"} {
		if value := lookupTagPrefix(tags, prefix); value != "" {
			return value
		}
	}
	return ""
}

// firstSubtitleTitle 从标签集合中读取最可能的标题字段。
func firstSubtitleTitle(tags map[string]interface{}) string {
	for _, key := range []string{"title", "name", "handler_name"} {
		if value := lookupTag(tags, key); value != "" {
			return value
		}
	}
	for _, prefix := range []string{"title-", "title_", "name-", "name_", "handler_name-", "handler_name_"} {
		if value := lookupTagPrefix(tags, prefix); value != "" {
			return value
		}
	}
	return ""
}

// lookupTag 按大小写不敏感的精确键名读取标签值。
func lookupTag(tags map[string]interface{}, wanted string) string {
	for key, value := range tags {
		if strings.EqualFold(strings.TrimSpace(key), wanted) {
			return strings.TrimSpace(jsonString(value))
		}
	}
	return ""
}

// lookupTagPrefix 按大小写不敏感的前缀读取标签值。
func lookupTagPrefix(tags map[string]interface{}, prefix string) string {
	for key, value := range tags {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), prefix) {
			return strings.TrimSpace(jsonString(value))
		}
	}
	return ""
}

// subtitleTagsSummary 把字幕标签映射稳定地序列化为日志字符串。
func subtitleTagsSummary(tags map[string]interface{}) string {
	if len(tags) == 0 {
		return ""
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(jsonString(tags[key]))
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

// jsonString 把 JSON 解析后的任意值转换成字符串表示。
func jsonString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(typed)
	}
}

// displayProbeValue 把空值或 unknown 类探测结果统一显示为“无”。
func displayProbeValue(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "", "unknown", "und", "undefined", "null", "n/a", "na":
		return "无"
	default:
		return strings.TrimSpace(value)
	}
}

// uniqueScreenshotName 为同一秒桶内的截图生成不冲突的文件名。
func uniqueScreenshotName(aligned float64, ext string, used map[string]int) string {
	base := secToFilenameStamp(aligned)
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base + ext
	}
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

// screenshotSecond 返回时间点对应的非负整秒桶。
func screenshotSecond(value float64) int {
	second := int(math.Floor(value))
	if second < 0 {
		return 0
	}
	return second
}

// normalizeStreamPID 会规范化流PID，并在输入为空或不受支持时返回稳定的默认值。
func normalizeStreamPID(raw string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "0x")
	if strings.HasPrefix(strings.TrimSpace(raw), "0x") || strings.HasPrefix(strings.TrimSpace(raw), "0X") {
		parsed, err := strconv.ParseInt(value, 16, 64)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	}
	if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return parsed, true
	}
	return 0, false
}

// formatStreamPID 把流 PID 格式化为十六进制字符串。
func formatStreamPID(value int) string {
	return fmt.Sprintf("0x%04X", value)
}

// findBlurayRootFromVideo 从视频路径向上回溯，定位对应的蓝光根目录。
func findBlurayRootFromVideo(videoPath string) (string, bool) {
	current := filepath.Dir(videoPath)
	for {
		if current == "/" || current == "." || current == "" {
			return "", false
		}
		if info, err := os.Stat(filepath.Join(current, "BDMV", "STREAM")); err == nil && info.IsDir() {
			return current, true
		}
		if strings.EqualFold(filepath.Base(current), "BDMV") {
			if info, err := os.Stat(filepath.Join(current, "STREAM")); err == nil && info.IsDir() {
				return filepath.Dir(current), true
			}
		}
		next := filepath.Dir(current)
		if next == current {
			return "", false
		}
		current = next
	}
}

type playlistScore struct {
	Name      string
	Contains  bool
	TotalSize int64
	ClipCount int
	FileSize  int64
}

// listBlurayPlaylistsRanked 按片段命中情况和总大小为蓝光播放列表排序。
func listBlurayPlaylistsRanked(root, clip string) []string {
	playlistDir := filepath.Join(root, "BDMV", "PLAYLIST")
	streamDir := filepath.Join(root, "BDMV", "STREAM")

	playlistEntries, err := os.ReadDir(playlistDir)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(streamDir); err != nil || !info.IsDir() {
		return nil
	}

	scores := make([]playlistScore, 0)
	for _, entry := range playlistEntries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".mpls") {
			continue
		}

		path := filepath.Join(playlistDir, entry.Name())
		clipIDs := extractMPLSClipIDs(path)
		if len(clipIDs) == 0 {
			continue
		}

		totalSize := int64(0)
		contains := false
		for _, clipID := range clipIDs {
			if clipID == clip {
				contains = true
			}
			if info, err := os.Stat(filepath.Join(streamDir, clipID+".m2ts")); err == nil {
				totalSize += info.Size()
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		scores = append(scores, playlistScore{
			Name:      strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Contains:  contains,
			TotalSize: totalSize,
			ClipCount: len(clipIDs),
			FileSize:  info.Size(),
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Contains != scores[j].Contains {
			return scores[i].Contains
		}
		if scores[i].TotalSize != scores[j].TotalSize {
			return scores[i].TotalSize > scores[j].TotalSize
		}
		if scores[i].ClipCount != scores[j].ClipCount {
			return scores[i].ClipCount > scores[j].ClipCount
		}
		if scores[i].FileSize != scores[j].FileSize {
			return scores[i].FileSize > scores[j].FileSize
		}
		return scores[i].Name < scores[j].Name
	})

	playlists := make([]string, 0, len(scores))
	for _, score := range scores {
		playlists = append(playlists, score.Name)
	}
	return playlists
}

// extractMPLSClipIDs 从 MPLS 文件内容里提取唯一的片段 ID 列表。
func extractMPLSClipIDs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	matches := clipIDPattern.FindAllString(string(data), -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		clipID := strings.TrimSuffix(match, "M2TS")
		if _, ok := seen[clipID]; ok {
			continue
		}
		seen[clipID] = struct{}{}
		ids = append(ids, clipID)
	}
	return ids
}

// firstFloatLine 返回多行输出里第一个可解析的浮点数。
func firstFloatLine(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := parseFloatString(line); ok {
			return value, true
		}
	}
	return 0, false
}

// parseFloatString 会解析浮点值字符串，并把原始输入转换成结构化结果。
func parseFloatString(value string) (float64, bool) {
	text := strings.TrimSpace(value)
	if text == "" || text == "N/A" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

// parseIntString 会解析整数字符串，并把原始输入转换成结构化结果。
func parseIntString(value string) (int, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// floatDiffGT 判断两个浮点数的差值是否超过容忍阈值。
func floatDiffGT(a, b float64) bool {
	return math.Abs(a-b) > 0.0005
}

// escapeFilterValue 转义 ffmpeg 字幕过滤器里使用的路径值。
func escapeFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	value = strings.ReplaceAll(value, `:`, `\:`)
	value = strings.ReplaceAll(value, `,`, `\,`)
	value = strings.ReplaceAll(value, `;`, `\;`)
	value = strings.ReplaceAll(value, `[`, `\[`)
	value = strings.ReplaceAll(value, `]`, `\]`)
	return value
}

// clearDir 删除目录下的所有内容，但保留目录本身。
func clearDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// allDigits 判断字符串是否非空且全部由数字组成。
func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, item := range value {
		if item < '0' || item > '9' {
			return false
		}
	}
	return true
}
