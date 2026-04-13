// Package screenshot 实现字幕时间对齐和字幕索引探测逻辑。

package screenshot

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"minfo/internal/system"
)

// resolveUniqueScreenshotSecond 会在已占用秒级时间点集合中寻找一个不冲突的截图时间点。
func (r *screenshotRunner) resolveUniqueScreenshotSecond(requested, aligned float64, usedSeconds map[int]struct{}) (float64, bool, bool) {
	aligned = r.clampToDuration(aligned)
	second := screenshotSecond(aligned)
	if _, exists := usedSeconds[second]; !exists {
		return aligned, false, true
	}

	if r.subtitle.Mode != "none" {
		if len(r.subtitleIndex) == 0 {
			r.subtitleIndex = r.buildSubtitleIndex()
		}
		for _, candidate := range r.uniqueAlignedCandidatesFromSubtitleIndex(requested) {
			candidate = r.clampToDuration(candidate)
			if _, exists := usedSeconds[screenshotSecond(candidate)]; exists {
				continue
			}
			return candidate, true, true
		}
	}

	return 0, false, false
}

// uniqueAlignedCandidatesFromSubtitleIndex 会根据字幕索引生成可去重的候选截图时间点。
func (r *screenshotRunner) uniqueAlignedCandidatesFromSubtitleIndex(requested float64) []float64 {
	if len(r.subtitleIndex) == 0 {
		return nil
	}

	type secondCandidate struct {
		value    float64
		distance float64
		second   int
	}

	candidates := make([]secondCandidate, 0, len(r.subtitleIndex))
	seen := make(map[int]struct{}, len(r.subtitleIndex))
	for _, span := range r.subtitleIndex {
		startSecond := screenshotSecond(span.Start)
		endSecond := screenshotSecond(span.End)
		for second := startSecond; second <= endSecond; second++ {
			secondStart := math.Max(span.Start, float64(second))
			secondEnd := math.Min(span.End, float64(second)+0.999)
			if secondEnd < secondStart {
				continue
			}
			candidate := secondStart + (secondEnd-secondStart)/2
			secondKey := screenshotSecond(candidate)
			if _, exists := seen[secondKey]; exists {
				continue
			}
			seen[secondKey] = struct{}{}
			candidates = append(candidates, secondCandidate{
				value:    candidate,
				distance: math.Abs(candidate - requested),
				second:   secondKey,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			if candidates[i].second == candidates[j].second {
				return candidates[i].value < candidates[j].value
			}
			return candidates[i].second < candidates[j].second
		}
		return candidates[i].distance < candidates[j].distance
	})

	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.value)
	}
	return values
}

// detectStartOffset 读取当前截图源的起始时间偏移。
func (r *screenshotRunner) detectStartOffset() float64 {
	return r.detectStartOffsetForInput(r.sourcePath)
}

// detectStartOffsetForInput 读取指定输入的起始时间偏移。
func (r *screenshotRunner) detectStartOffsetForInput(input string) float64 {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value
		}
	}

	stdout, _, err = system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-show_entries", "format=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
			return value
		}
	}
	return 0
}

// detectVideoDimensions 读取当前视频流的宽高。
func (r *screenshotRunner) detectVideoDimensions() (int, int) {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		r.sourcePath,
	)
	if err != nil {
		return 0, 0
	}

	value := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, err1 := strconv.Atoi(parts[0])
	height, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return width, height
}

// detectColorspace 读取视频流的色彩空间元数据，并整理成稳定键值。
func (r *screenshotRunner) detectColorspace() string {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_space,color_primaries,color_transfer",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return ""
	}

	lines := make([]string, 0, 3)
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "color_space=") || strings.HasPrefix(line, "color_primaries=") || strings.HasPrefix(line, "color_transfer=") {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "|") + "|"
}

// buildColorspaceChain 返回 ffmpeg 使用的色彩空间转换过滤器链。
func buildColorspaceChain(info string) string {
	switch {
	case strings.Contains(info, "bt2020") && (strings.Contains(info, "smpte2084") || strings.Contains(info, "arib-std-b67")):
		return "format=yuv420p10le,zscale=t=linear:npl=203,format=gbrpf32le,tonemap=mobius:param=0.3:desat=2.0,zscale=p=bt709:t=bt709:m=bt709,format=rgb24"
	case strings.Contains(info, "bt2020"):
		return "zscale=p=bt709:t=bt709:m=bt709,format=rgb24"
	default:
		return ""
	}
}

// buildDisplayAspectFilter 会构建显示参数显示比例过滤器，为后续流程准备好可直接使用的结果。
func buildDisplayAspectFilter() string {
	// DVD/VOB and other anamorphic sources often use non-square pixels.
	// Still-image formats do not reliably preserve SAR, so we expand to
	// square pixels before writing PNG/JPG.
	return "scale='trunc(iw*sar/2)*2:ih',setsar=1"
}

// detectDisplayAspectFilter 读取视频流的 SAR/DAR 元数据，并构建更适合静态截图的比例修正过滤器。
func (r *screenshotRunner) detectDisplayAspectFilter() string {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,sample_aspect_ratio,display_aspect_ratio",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return buildDisplayAspectFilter()
	}

	width := 0
	height := 0
	sar := ""
	dar := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "width="):
			width, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "width=")))
		case strings.HasPrefix(line, "height="):
			height, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "height=")))
		case strings.HasPrefix(line, "sample_aspect_ratio="):
			sar = strings.TrimSpace(strings.TrimPrefix(line, "sample_aspect_ratio="))
		case strings.HasPrefix(line, "display_aspect_ratio="):
			dar = strings.TrimSpace(strings.TrimPrefix(line, "display_aspect_ratio="))
		}
	}

	if looksLikeDVDSource(r.dvdProbeSource()) {
		mediainfoDAR := ""
		if r.hasDVDMediaInfoResult {
			mediainfoDAR = strings.TrimSpace(r.dvdMediaInfoResult.DisplayAspectRatio)
		}
		r.logf("[信息] DVD 比例探测：ffprobe width=%d height=%d sar=%s dar=%s | mediainfo dar=%s",
			width,
			height,
			displayProbeValue(sar),
			displayProbeValue(dar),
			displayProbeValue(mediainfoDAR),
		)
		if width2, height2, filter, ok := r.detectDVDDisplayAspectFilterFromMediaInfo(width, height); ok {
			r.logf("[信息] DVD 比例修正将直接使用 mediainfo DAR：%s", filter)
			return buildDisplayAspectFilterForMetadata(width2, height2, "", filter)
		}
		r.logf("[提示] mediainfo 未提供可用 DVD 比例，回退 ffprobe SAR/DAR。")
	}

	return buildDisplayAspectFilterForMetadata(width, height, sar, dar)
}

func (r *screenshotRunner) detectDVDDisplayAspectFilterFromMediaInfo(width, height int) (int, int, string, bool) {
	if r == nil || !r.hasDVDMediaInfoResult {
		return 0, 0, "", false
	}
	if !looksLikeDVDSource(r.dvdProbeSource()) {
		return 0, 0, "", false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, "", false
	}
	if strings.TrimSpace(r.dvdMediaInfoResult.DisplayAspectRatio) == "" {
		return 0, 0, "", false
	}
	return width, height, r.dvdMediaInfoResult.DisplayAspectRatio, true
}

// buildDisplayAspectFilterForMetadata 根据视频宽高和宽高比元数据生成静态截图所需的比例修正链。
func buildDisplayAspectFilterForMetadata(width, height int, sar, dar string) string {
	if width > 0 && height > 0 {
		normalizedDAR := normalizeMediaInfoAspectRatio(dar)
		if darNum, darDen, ok := parseAspectRatio(normalizedDAR); ok {
			rawAspect := float64(width) / float64(height)
			displayAspect := float64(darNum) / float64(darDen)
			if math.Abs(displayAspect-rawAspect) > 0.02 {
				return fmt.Sprintf("scale='trunc(ih*%d/%d/2)*2:ih',setsar=1", darNum, darDen)
			}
		} else if displayAspect, ok := parseAspectRatioValue(normalizedDAR); ok {
			rawAspect := float64(width) / float64(height)
			if math.Abs(displayAspect-rawAspect) > 0.02 {
				return fmt.Sprintf("scale='trunc(ih*%.6f/2)*2:ih',setsar=1", displayAspect)
			}
		}
	}

	if sarNum, sarDen, ok := parseAspectRatio(sar); ok {
		if sarNum == sarDen {
			return "setsar=1"
		}
	}
	return buildDisplayAspectFilter()
}

func parseAspectRatio(raw string) (int, int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "N/A" || value == "0:1" {
		return 0, 0, false
	}

	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}

	num, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	den, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || num <= 0 || den <= 0 {
		return 0, 0, false
	}
	return num, den, true
}

func parseAspectRatioValue(raw string) (float64, bool) {
	if num, den, ok := parseAspectRatio(raw); ok {
		return float64(num) / float64(den), true
	}

	value := strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if value == "" || value == "N/A" {
		return 0, false
	}

	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || ratio <= 0 {
		return 0, false
	}
	return ratio, true
}

// joinFilters 连接多个非空 ffmpeg 过滤器片段。
func joinFilters(parts ...string) string {
	filters := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filters = append(filters, part)
	}
	return strings.Join(filters, ",")
}

// pgsBitmapPacketMinSize 返回探测 PGS 位图字幕区间时使用的最小包大小阈值。
func pgsBitmapPacketMinSize() int {
	return 1500
}

// dvdBitmapPacketMinSize 返回探测 DVD 位图字幕区间时使用的最小包大小阈值。
func dvdBitmapPacketMinSize() int {
	return 1
}

// alignToSubtitle 根据当前字幕模式和可用索引选择最终截图时间点。
func (r *screenshotRunner) alignToSubtitle(requested float64) float64 {
	if r.subtitle.Mode == "none" {
		return requested
	}

	if r.subtitle.Mode == "internal" && r.isPGSSubtitle() {
		return r.alignPGSToSubtitle(requested)
	}

	if candidate, ok := r.snapWindow(requested); ok {
		candidate = r.clampToDuration(candidate)
		if r.subtitle.Mode == "internal" && r.isDVDSubtitle() {
			if confirmed, ok := r.acceptBitmapSubtitleCandidate("就近/扩窗字幕", candidate); ok {
				r.logf("[对齐] 请求 %s → 就近/扩窗字幕 %s", secToHMSMS(requested), secToHMSMS(confirmed))
				return confirmed
			}
		} else {
			r.logf("[对齐] 请求 %s → 就近/扩窗字幕 %s", secToHMSMS(requested), secToHMSMS(candidate))
			return candidate
		}
	}

	if candidate, ok := r.snapExpandedWindow(requested); ok {
		candidate = r.clampToDuration(candidate)
		if r.subtitle.Mode == "internal" && r.isDVDSubtitle() {
			if confirmed, ok := r.acceptBitmapSubtitleCandidate("扩窗字幕", candidate); ok {
				r.logf("[对齐] 请求 %s → 扩窗字幕 %s", secToHMSMS(requested), secToHMSMS(confirmed))
				return confirmed
			}
		} else {
			r.logf("[对齐] 请求 %s → 扩窗字幕 %s", secToHMSMS(requested), secToHMSMS(candidate))
			return candidate
		}
	}

	if r.subtitle.Mode == "internal" && r.isDVDSubtitle() {
		if candidate, ok := r.findNearestBitmapSubtitle(requested); ok && math.Abs(candidate-requested) <= 1200 && candidate >= 0 {
			if confirmed, ok := r.acceptBitmapSubtitleCandidate("渐进扩窗", candidate); ok {
				r.logf("[对齐] 请求 %s → 渐进扩窗 %s", secToHMSMS(requested), secToHMSMS(confirmed))
				return confirmed
			}
		}
	}

	if len(r.subtitleIndex) == 0 {
		r.subtitleIndex = r.buildSubtitleIndex()
	}
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		if candidate, ok := r.findNearestVisibleBitmapIndexedCandidate(requested); ok {
			candidate = r.clampToDuration(candidate)
			if floatDiffGT(candidate, requested) {
				r.logf("[对齐] 请求 %s → 全片位图索引 %s", secToHMSMS(requested), secToHMSMS(candidate))
			} else {
				r.logf("[提示] 周边扩窗未命中，沿用全片位图索引对齐到原时间点附近：%s", secToHMSMS(candidate))
			}
			return candidate
		}
		r.logf("[提示] 周边及全片均未找到可见字幕事件，按原时间点截图：%s", secToHMSMS(requested))
		return requested
	}
	if candidate, ok := snapFromIndex(requested, r.subtitleIndex, subtitleSnapEpsilon); ok {
		candidate = r.clampToDuration(candidate)
		if floatDiffGT(candidate, requested) {
			r.logf("[对齐] 请求 %s → 全片索引 %s", secToHMSMS(requested), secToHMSMS(candidate))
		} else {
			r.logf("[提示] 周边及全片均未找到字幕事件，按原时间点截图：%s", secToHMSMS(requested))
		}
		return candidate
	}

	r.logf("[提示] 周边及全片均未找到字幕事件，按原时间点截图：%s", secToHMSMS(requested))
	return requested
}

// alignPGSToSubtitle 处理 PGS 位图字幕的时间对齐，并结合可见性校验筛掉无效候选。
func (r *screenshotRunner) alignPGSToSubtitle(requested float64) float64 {
	if candidate, ok := r.snapWindow(requested); ok {
		if confirmed, ok := r.acceptBitmapSubtitleCandidate("就近/扩窗字幕", candidate); ok {
			r.logf("[对齐] 请求 %s → 就近/扩窗字幕 %s", secToHMSMS(requested), secToHMSMS(confirmed))
			return confirmed
		}
	}

	if candidate, ok := r.snapExpandedWindow(requested); ok {
		if confirmed, ok := r.acceptBitmapSubtitleCandidate("扩窗字幕", candidate); ok {
			r.logf("[对齐] 请求 %s → 扩窗字幕 %s", secToHMSMS(requested), secToHMSMS(confirmed))
			return confirmed
		}
	}

	if candidate, ok := r.findNearestBitmapSubtitle(requested); ok && math.Abs(candidate-requested) <= 1200 && candidate >= 0 {
		if confirmed, ok := r.acceptBitmapSubtitleCandidate("渐进扩窗", candidate); ok {
			r.logf("[对齐] 请求 %s → 渐进扩窗 %s", secToHMSMS(requested), secToHMSMS(confirmed))
			return confirmed
		}
	}

	r.logf("[提示] 周边及全片均未找到字幕事件，按原时间点截图：%s", secToHMSMS(requested))
	return requested
}

// snapWindow 在常规搜索窗口内查找离请求时间最近的字幕事件。
func (r *screenshotRunner) snapWindow(requested float64) (float64, bool) {
	var spans []subtitleSpan
	var err error

	switch {
	case r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle():
		absoluteStart := math.Max(requested+r.startOffset-r.settings.SearchBack, 0)
		spans, err = r.probeSupportedBitmapSpans(absoluteStart, r.settings.SearchBack+r.settings.SearchForward)
	case r.subtitle.Mode == "internal":
		absoluteStart := math.Max(requested+r.startOffset-r.settings.SearchBack, 0)
		spans, err = r.probeInternalTextSpans(absoluteStart, r.settings.SearchBack+r.settings.SearchForward)
	default:
		start := math.Max(requested-r.settings.SearchBack, 0)
		spans, err = r.probeExternalTextSpans(start, r.settings.SearchBack+r.settings.SearchForward)
	}
	if err != nil {
		r.logf("[提示] 字幕窗口探测失败：%s", err.Error())
		return 0, false
	}
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		return snapFromBitmapSpans(requested, spans, subtitleSnapEpsilon)
	}
	return snapFromSpans(requested, spans, subtitleSnapEpsilon)
}

// snapExpandedWindow 在更大的搜索窗口内再次查找字幕事件。
func (r *screenshotRunner) snapExpandedWindow(requested float64) (float64, bool) {
	var spans []subtitleSpan
	var err error

	switch {
	case r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle():
		absoluteStart := math.Max(requested+r.startOffset-60, 0)
		spans, err = r.probeSupportedBitmapSpans(absoluteStart, 120)
	case r.subtitle.Mode == "internal":
		absoluteStart := math.Max(requested+r.startOffset-60, 0)
		spans, err = r.probeInternalTextSpans(absoluteStart, 120)
	default:
		start := math.Max(requested-60, 0)
		spans, err = r.probeExternalTextSpans(start, 120)
	}
	if err != nil {
		r.logf("[提示] 字幕扩窗探测失败：%s", err.Error())
		return 0, false
	}
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		return snapFromBitmapSpans(requested, spans, subtitleSnapEpsilon)
	}
	return snapFromSpans(requested, spans, subtitleSnapEpsilon)
}

// findNearestBitmapSubtitle 会查找最近位图字幕，并在多个候选项中返回最合适的结果。
func (r *screenshotRunner) findNearestBitmapSubtitle(requested float64) (float64, bool) {
	for _, span := range []float64{60, 120, 240, 480, 900} {
		absoluteStart := math.Max(requested+r.startOffset-span, 0)
		spans, err := r.probeSupportedBitmapSpans(absoluteStart, span+span)
		if err != nil || len(spans) == 0 {
			continue
		}

		best := -1.0
		bestSpan := subtitleSpan{}
		bestDistance := math.MaxFloat64
		for _, item := range spans {
			mid := item.Start + (item.End-item.Start)/2
			distance := math.Abs(mid - requested)
			if distance < bestDistance {
				best = mid
				bestSpan = item
				bestDistance = distance
			}
		}
		if best >= 0 {
			return r.clampToDuration(bitmapSnapPoint(bestSpan, subtitleSnapEpsilon)), true
		}
	}
	return requested, false
}

// acceptBitmapSubtitleCandidate 在接受位图候选时间点前验证该时刻是否真的渲染出字幕。
func (r *screenshotRunner) acceptBitmapSubtitleCandidate(label string, candidate float64) (float64, bool) {
	candidate = r.clampToDuration(candidate)
	key := bitmapCandidateKey(candidate)
	if _, rejected := r.rejectedBitmapCandidates[key]; rejected {
		return 0, false
	}

	visible, err := r.bitmapSubtitleVisibleAt(candidate)
	if err != nil {
		r.logf("[提示] %s候选可视性验证失败，沿用该时间点：%s | 原因：%s",
			label,
			secToHMSMS(candidate),
			err.Error(),
		)
		return candidate, true
	}
	if !visible {
		if r.rejectedBitmapCandidates == nil {
			r.rejectedBitmapCandidates = make(map[string]struct{})
		}
		r.rejectedBitmapCandidates[key] = struct{}{}
		r.logf("[提示] %s候选未实际渲染出字幕，继续搜索：%s",
			label,
			secToHMSMS(candidate),
		)
		return 0, false
	}
	return candidate, true
}

// findNearestVisibleBitmapIndexedCandidate 会查找最近可见位图Indexed候选项，并在多个候选项中返回最合适的结果。
func (r *screenshotRunner) findNearestVisibleBitmapIndexedCandidate(requested float64) (float64, bool) {
	if len(r.subtitleIndex) == 0 {
		return 0, false
	}

	spans := append([]subtitleSpan(nil), r.subtitleIndex...)
	sort.Slice(spans, func(i, j int) bool {
		left := math.Abs(bitmapSnapPoint(spans[i], subtitleSnapEpsilon) - requested)
		right := math.Abs(bitmapSnapPoint(spans[j], subtitleSnapEpsilon) - requested)
		if left == right {
			return spans[i].Start < spans[j].Start
		}
		return left < right
	})

	limit := len(spans)
	if limit > 8 {
		limit = 8
	}
	for _, span := range spans[:limit] {
		candidate, ok := r.acceptBitmapSubtitleCandidate("全片位图索引", bitmapSnapPoint(span, subtitleSnapEpsilon))
		if ok {
			return candidate, true
		}
	}
	return 0, false
}

// buildSubtitleIndex 会为当前字幕源建立可复用的时间区间索引，供后续对齐和去重逻辑使用。
func (r *screenshotRunner) buildSubtitleIndex() []subtitleSpan {
	if r.subtitle.Mode == "none" {
		return nil
	}
	if r.subtitle.Mode == "internal" && r.isPGSSubtitle() {
		return nil
	}

	var spans []subtitleSpan
	var err error

	if r.subtitle.Mode == "internal" && r.isDVDSubtitle() {
		spans, err = r.probeSupportedBitmapSpans(-1, 0)
	} else if r.subtitle.Mode == "internal" {
		spans, err = r.probeInternalTextSpans(-1, 0)
	} else {
		spans, err = r.probeExternalTextSpans(-1, 0)
	}
	if err != nil {
		r.logf("[提示] 建立字幕索引失败：%s", err.Error())
		return nil
	}
	if len(spans) == 0 {
		return nil
	}

	if r.subtitle.Mode == "internal" && r.isDVDSubtitle() {
		spans = mergeNearbySubtitleSpans(spans, 0.75)
		r.logf("[信息] 已建立字幕索引（DVD 位图字幕，共 %d 段）。", len(spans))
		return spans
	}

	r.logf("[信息] 已建立字幕索引（文字字幕）。")
	return spans
}

// probeSupportedBitmapSpans 根据字幕类型分派到对应的位图字幕区间探测逻辑。
func (r *screenshotRunner) probeSupportedBitmapSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	switch {
	case r.isPGSSubtitle():
		return r.probePGSSubtitleSpans(startAbs, duration)
	case r.isDVDSubtitle():
		return r.probeDVDSubtitleSpans(startAbs, duration)
	default:
		return nil, nil
	}
}

// probePGSSubtitleSpans 探测 PGS 字幕的时间区间。
func (r *screenshotRunner) probePGSSubtitleSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	return r.probeInternalBitmapSpans(startAbs, duration, pgsBitmapPacketMinSize())
}

// probeDVDSubtitleSpans 探测 DVD 字幕的时间区间。
func (r *screenshotRunner) probeDVDSubtitleSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	return r.probeInternalBitmapSpans(startAbs, duration, dvdBitmapPacketMinSize())
}

// probeInternalBitmapSpans 用 ffprobe 包信息构建内挂位图字幕的时间区间。
func (r *screenshotRunner) probeInternalBitmapSpans(startAbs, duration float64, bitmapMinSize int) ([]subtitleSpan, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", readInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time,size",
		"-of", "json",
		r.sourcePath,
	)
	return r.probePacketSpans(args, true, bitmapMinSize)
}

// probeInternalTextSpans 用 ffprobe 包信息构建内挂文字字幕的时间区间。
func (r *screenshotRunner) probeInternalTextSpans(startAbs, duration float64) ([]subtitleSpan, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", readInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "json",
		r.sourcePath,
	)
	return r.probePacketSpans(args, true, -1)
}

// probeExternalTextSpans 用 ffprobe 包信息构建外挂文字字幕的时间区间。
func (r *screenshotRunner) probeExternalTextSpans(start, duration float64) ([]subtitleSpan, error) {
	args := []string{"-v", "error"}
	if start >= 0 {
		args = append(args, "-read_intervals", readInterval(start, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "json",
		r.subtitle.File,
	)
	return r.probePacketSpans(args, false, -1)
}

// probePacketSpans 把 ffprobe 返回的包数据转换成排序后的字幕区间列表。
func (r *screenshotRunner) probePacketSpans(args []string, internal bool, bitmapMinSize int) ([]subtitleSpan, error) {
	stdout, stderr, err := system.RunCommand(r.ctx, r.ffprobeBin, args...)
	if err != nil {
		return nil, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var payload ffprobePacketsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}

	spans := make([]subtitleSpan, 0, len(payload.Packets))
	for _, packet := range payload.Packets {
		pts, ok := parseFloatString(packet.PTSTime)
		if !ok {
			continue
		}
		durationValue, ok := parseFloatString(packet.DurationTime)
		if !ok {
			durationValue = defaultSubtitleDuration
		}
		if bitmapMinSize >= 0 {
			sizeValue, ok := parseIntString(packet.Size)
			if !ok || sizeValue < bitmapMinSize {
				continue
			}
		}

		start := pts
		end := pts + durationValue
		if internal {
			start -= r.startOffset
			end -= r.startOffset
		}
		if end < 0 {
			continue
		}
		if start < 0 {
			start = 0
		}
		spans = append(spans, subtitleSpan{Start: start, End: end})
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})
	if bitmapMinSize >= 0 {
		return mergeNearbySubtitleSpans(spans, 0.75), nil
	}
	return spans, nil
}

// clampToDuration 把时间点限制在 [0, duration] 范围内。
func (r *screenshotRunner) clampToDuration(value float64) float64 {
	if value < 0 {
		return 0
	}
	if r.duration > 0 && value > r.duration {
		return r.duration
	}
	return value
}
