package handlers

import (
	"strings"

	"minfo/internal/httpapi/transport"
	"minfo/internal/taskprogress"
)

type screenshotProgressMarker struct {
	current      int
	total        int
	percent      float64
	detail       string
	stepOrder    int
	percentOrder int
	detailOrder  int
}

type screenshotProgressState struct {
	bootstrapMarker     *screenshotProgressMarker
	subtitleMarker      *screenshotProgressMarker
	prepMarker          *screenshotProgressMarker
	packageMarker       *screenshotProgressMarker
	renderMarker        *screenshotProgressMarker
	captureStarted      int
	captureCompleted    int
	captureTotal        int
	captureStartDetail  string
	captureFinishDetail string
	captureStartOrder   int
	captureFinishOrder  int
	uploadTotal         int
	uploadProcessed     int
	uploadFinished      bool
}

// parseScreenshotProgressState 会把截图日志解析成便于估算进度的状态快照。
func parseScreenshotProgressState(entries []transport.LogEntry) screenshotProgressState {
	state := screenshotProgressState{}

	for idx, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		if event, ok := taskprogress.ParseLogLine(line); ok {
			switch event.Kind {
			case taskprogress.KindPercent:
				switch event.Stage {
				case taskprogress.StageBootstrap:
					state.bootstrapMarker = updateScreenshotProgressMarkerPercent(state.bootstrapMarker, event.Percent, event.Detail, idx)
				case taskprogress.StageRender:
					state.renderMarker = updateScreenshotProgressMarkerPercent(state.renderMarker, event.Percent, event.Detail, idx)
				case taskprogress.StagePrepare:
					state.prepMarker = updateScreenshotProgressMarkerPercent(state.prepMarker, event.Percent, event.Detail, idx)
				case taskprogress.StagePackage:
					state.packageMarker = updateScreenshotProgressMarkerPercent(state.packageMarker, event.Percent, event.Detail, idx)
				case taskprogress.StageSubtitle:
					state.subtitleMarker = updateScreenshotProgressMarkerPercent(state.subtitleMarker, event.Percent, event.Detail, idx)
				}
			case taskprogress.KindStep:
				switch event.Stage {
				case taskprogress.StageBootstrap:
					state.bootstrapMarker = updateScreenshotProgressMarkerStep(state.bootstrapMarker, event.Current, event.Total, event.Detail, idx)
				case taskprogress.StageSubtitle:
					state.subtitleMarker = updateScreenshotProgressMarkerStep(state.subtitleMarker, event.Current, event.Total, event.Detail, idx)
				case taskprogress.StagePrepare:
					state.prepMarker = updateScreenshotProgressMarkerStep(state.prepMarker, event.Current, event.Total, event.Detail, idx)
				case taskprogress.StageCaptureStart:
					state.captureStarted = event.Current
					state.captureTotal = maxInt(state.captureTotal, event.Total)
					state.captureStartDetail = event.Detail
					state.captureStartOrder = idx
					state.renderMarker = nil
				case taskprogress.StageCaptureDone:
					state.captureCompleted = event.Current
					state.captureTotal = maxInt(state.captureTotal, event.Total)
					state.captureFinishDetail = event.Detail
					state.captureFinishOrder = idx
					state.renderMarker = nil
				case taskprogress.StagePackage:
					state.packageMarker = updateScreenshotProgressMarkerStep(state.packageMarker, event.Current, event.Total, event.Detail, idx)
				}
			}
			continue
		}

		switch {
		case screenshotUploadStartPattern.MatchString(line):
			matches := screenshotUploadStartPattern.FindStringSubmatch(line)
			state.uploadTotal = parseInt(matches, 1)
		case strings.HasPrefix(line, "已上传并校准域名:") || strings.HasPrefix(line, "上传失败:"):
			state.uploadProcessed++
		case strings.HasPrefix(line, "处理完成! 成功:"):
			state.uploadFinished = true
		}
	}

	return state
}

// updateScreenshotProgressMarkerStep 会用 step 型进度日志刷新阶段标记。
func updateScreenshotProgressMarkerStep(marker *screenshotProgressMarker, current, total int, detail string, order int) *screenshotProgressMarker {
	if marker == nil {
		marker = &screenshotProgressMarker{}
	}
	marker.current = current
	marker.total = total
	marker.percent = 0
	marker.stepOrder = order
	if order >= marker.detailOrder {
		marker.detail = detail
		marker.detailOrder = order
	}
	return marker
}

// updateScreenshotProgressMarkerPercent 会用百分比型进度日志刷新阶段标记。
func updateScreenshotProgressMarkerPercent(marker *screenshotProgressMarker, percent float64, detail string, order int) *screenshotProgressMarker {
	if marker == nil {
		marker = &screenshotProgressMarker{}
	}
	if percent > marker.percent {
		marker.percent = percent
	}
	marker.percentOrder = order
	if order >= marker.detailOrder {
		marker.detail = detail
		marker.detailOrder = order
	}
	return marker
}

// markerStepProgress 会把阶段 step 进度换算成连续的有效进度值。
func markerStepProgress(marker *screenshotProgressMarker, entryBias float64) float64 {
	if marker == nil {
		return 0
	}
	if marker.current <= 0 {
		return 0
	}
	return float64(marker.current) - entryBias
}

// markerStageProgress 会综合 step 和百分比标记换算阶段内的连续进度值。
func markerStageProgress(marker *screenshotProgressMarker, entryBias float64) float64 {
	if marker == nil {
		return 0
	}
	if marker.current > 0 {
		effective := float64(maxInt(marker.current-1, 0))
		if marker.percentOrder >= marker.stepOrder && marker.percent > 0 {
			return effective + float64(marker.percent)/100.0
		}
		return effective + entryBias
	}
	if marker.percent > 0 && marker.total > 0 {
		return float64(marker.percent) / 100.0 * float64(marker.total)
	}
	if marker.percent > 0 {
		return float64(marker.percent) / 100.0
	}
	return 0
}

// subtitleProgressPercent 会把字幕准备阶段标记换算为整体任务百分比。
func subtitleProgressPercent(marker *screenshotProgressMarker) float64 {
	if marker == nil {
		return 0
	}
	if marker.total > 0 && marker.current > 0 {
		return clampPercent(markerStageProgress(marker, 0.1) / float64(marker.total) * float64(subtitleStageWidth()))
	}
	if marker.percent <= 0 {
		return 0
	}
	return clampPercent(clampPercent(marker.percent) / 100.0 * float64(subtitleStageWidth()))
}

// bootstrapProgressPercent 会把截图启动阶段标记换算为整体任务前段的百分比。
func bootstrapProgressPercent(marker *screenshotProgressMarker) float64 {
	if marker == nil {
		return 0
	}
	if marker.total > 0 && marker.current > 0 {
		return clampPercent(markerStageProgress(marker, 0.1) / float64(marker.total) * 8)
	}
	if marker.percent <= 0 {
		return 0
	}
	return clampPercent(clampPercent(marker.percent) / 100.0 * 8)
}
