// Package screenshot 实现字幕时间对齐和唯一秒去重逻辑。

package screenshot

import (
	"math"
	"sort"

	screenshotruntime "minfo/internal/screenshot/runtime"
	screenshotsubtitle "minfo/internal/screenshot/subtitle"
	screenshottimestamps "minfo/internal/screenshot/timestamps"
)

const subtitleSnapEpsilon = 0.50

// resolveUniqueScreenshotSecond 会在已占用秒级时间点集合中寻找一个不冲突的截图时间点。
func (r *screenshotRunner) resolveUniqueScreenshotSecond(requested, aligned float64, usedSeconds map[int]struct{}) (float64, bool, bool) {
	aligned = r.clampToDuration(aligned)
	second := screenshottimestamps.ScreenshotSecond(aligned)
	if _, exists := usedSeconds[second]; !exists {
		return aligned, false, true
	}

	if r.subtitle.Mode != "none" {
		r.ensureSubtitleIndex()
		for _, candidate := range r.uniqueAlignedCandidatesFromSubtitleIndex(requested) {
			candidate = r.clampToDuration(candidate)
			if _, exists := usedSeconds[screenshottimestamps.ScreenshotSecond(candidate)]; exists {
				continue
			}
			return candidate, true, true
		}
	}

	return 0, false, false
}

// uniqueAlignedCandidatesFromSubtitleIndex 会根据字幕索引生成可去重的候选截图时间点。
func (r *screenshotRunner) uniqueAlignedCandidatesFromSubtitleIndex(requested float64) []float64 {
	if len(r.ensureSubtitleIndex()) == 0 {
		return nil
	}

	type secondCandidate struct {
		value    float64
		distance float64
		second   int
	}

	candidates := make([]secondCandidate, 0, len(r.subtitleState.Index))
	seen := make(map[int]struct{}, len(r.subtitleState.Index))
	for _, span := range r.subtitleState.Index {
		startSecond := screenshottimestamps.ScreenshotSecond(span.Start)
		endSecond := screenshottimestamps.ScreenshotSecond(span.End)
		for second := startSecond; second <= endSecond; second++ {
			secondStart := math.Max(span.Start, float64(second))
			secondEnd := math.Min(span.End, float64(second)+0.999)
			if secondEnd < secondStart {
				continue
			}
			candidate := secondStart + (secondEnd-secondStart)/2
			secondKey := screenshottimestamps.ScreenshotSecond(candidate)
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

// alignToSubtitle 会基于全片字幕索引选择最终截图时间点。
func (r *screenshotRunner) alignToSubtitle(requested float64) float64 {
	if r.subtitle.Mode == "none" {
		return requested
	}

	index := r.ensureSubtitleIndex()
	if len(index) == 0 {
		r.logf("[提示] 全片字幕索引未找到可用字幕事件，按原时间点截图：%s", screenshottimestamps.SecToHMSMS(requested))
		return requested
	}

	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		r.logBitmapSubtitleVisibilityProgress()
		if candidate, ok := r.findNearestVisibleBitmapIndexedCandidate(requested); ok {
			return r.logAlignedSubtitleIndexCandidate(requested, candidate)
		}
		r.logf("[提示] 全片字幕索引未找到可见字幕事件，按原时间点截图：%s", screenshottimestamps.SecToHMSMS(requested))
		return requested
	}

	if candidate, ok := screenshotsubtitle.SnapFromIndex(requested, index, subtitleSnapEpsilon); ok {
		candidate = r.clampToDuration(candidate)
		return r.logAlignedSubtitleIndexCandidate(requested, candidate)
	}

	r.logf("[提示] 全片字幕索引未找到可用字幕事件，按原时间点截图：%s", screenshottimestamps.SecToHMSMS(requested))
	return requested
}

// logAlignedSubtitleIndexCandidate 会记录全片字幕索引命中的对齐结果并返回最终时间点。
func (r *screenshotRunner) logAlignedSubtitleIndexCandidate(requested, candidate float64) float64 {
	candidate = r.clampToDuration(candidate)
	if floatDiffGT(candidate, requested) {
		r.logf("[对齐] 请求 %s → 全片字幕索引 %s", screenshottimestamps.SecToHMSMS(requested), screenshottimestamps.SecToHMSMS(candidate))
	} else {
		r.logf("[提示] 已直接复用全片字幕索引对齐到原时间点附近：%s", screenshottimestamps.SecToHMSMS(candidate))
	}
	return candidate
}

// acceptBitmapSubtitleCandidate 在接受位图候选时间点前验证该时刻是否真的渲染出字幕。
func (r *screenshotRunner) acceptBitmapSubtitleCandidate(label string, candidate float64) (float64, bool) {
	candidate = r.clampToDuration(candidate)
	key := screenshotsubtitle.BitmapCandidateKey(candidate)
	if _, rejected := r.subtitleState.RejectedBitmapCandidates[key]; rejected {
		return 0, false
	}

	visible, err := r.bitmapSubtitleVisibleAt(candidate)
	if err != nil {
		r.logf("[提示] %s候选可视性验证失败，沿用该时间点：%s | 原因：%s",
			label,
			screenshottimestamps.SecToHMSMS(candidate),
			err.Error(),
		)
		return candidate, true
	}
	if !visible {
		if r != nil && r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
			shortBack := r.renderCoarseBack()
			longBack := r.settings.CoarseBackPGS
			if longBack > shortBack {
				longVisible, longErr := r.internalBitmapSubtitleVisibleAtWithCoarseBack(candidate, longBack)
				if longErr == nil && longVisible {
					r.subtitleState.BitmapRenderBackOverride = longBack
					r.logf("[提示] %s候选仅在较大回溯窗口下渲染出字幕，后续位图截图改用 %ds 回溯窗口：%s",
						label,
						longBack,
						screenshottimestamps.SecToHMSMS(candidate),
					)
					return candidate, true
				}
			}
		}
		if r.subtitleState.RejectedBitmapCandidates == nil {
			r.subtitleState.RejectedBitmapCandidates = make(map[string]struct{})
		}
		r.subtitleState.RejectedBitmapCandidates[key] = struct{}{}
		r.logf("[提示] %s候选未实际渲染出字幕，继续搜索：%s",
			label,
			screenshottimestamps.SecToHMSMS(candidate),
		)
		return 0, false
	}
	return candidate, true
}

// findNearestVisibleBitmapIndexedCandidate 会查找最近可见的全片位图字幕索引候选项。
func (r *screenshotRunner) findNearestVisibleBitmapIndexedCandidate(requested float64) (float64, bool) {
	if len(r.ensureSubtitleIndex()) == 0 {
		return 0, false
	}

	spans := append([]screenshotruntime.SubtitleSpan(nil), r.subtitleState.Index...)
	sort.Slice(spans, func(i, j int) bool {
		left := math.Abs(screenshotsubtitle.BitmapSnapPoint(spans[i], subtitleSnapEpsilon) - requested)
		right := math.Abs(screenshotsubtitle.BitmapSnapPoint(spans[j], subtitleSnapEpsilon) - requested)
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
		candidate, ok := r.acceptBitmapSubtitleCandidate("全片字幕索引", screenshotsubtitle.BitmapSnapPoint(span, subtitleSnapEpsilon))
		if ok {
			return candidate, true
		}
	}
	return 0, false
}

// clampToDuration 把时间点限制在 [0, duration] 范围内。
func (r *screenshotRunner) clampToDuration(value float64) float64 {
	if value < 0 {
		return 0
	}
	if r.media.Duration > 0 && value > r.media.Duration {
		return r.media.Duration
	}
	return value
}

func floatDiffGT(a, b float64) bool {
	return math.Abs(a-b) > 0.0005
}
