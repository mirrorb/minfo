package handlers

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

var screenshotUploadStartPattern = regexp.MustCompile(`^开始处理 (\d+) 个文件\.\.\.$`)

// estimateScreenshotTaskRunningProgress 会根据截图模式和日志推导截图任务的运行进度。
func estimateScreenshotTaskRunningProgress(mode string, count int, entries []transport.LogEntry) *transport.TaskProgress {
	requestedCount := screenshot.NormalizeCount(strconv.Itoa(count))
	if requestedCount <= 0 {
		requestedCount = 1
	}

	state := parseScreenshotProgressState(entries)
	if mode == screenshot.ModeLinks {
		if progress := estimateUploadProgressFromMarkers(requestedCount, state); progress != nil {
			return progress
		}
	} else if progress := estimateZipProgressFromMarkers(requestedCount, state); progress != nil {
		return progress
	}

	initFinished := false
	captureAttempts := 0
	captureFinished := false
	uploadTotal := 0
	uploadProcessed := 0
	uploadFinished := false

	for _, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		switch {
		case strings.HasPrefix(line, "[信息] 容器起始偏移："):
			initFinished = true
		case strings.HasPrefix(line, "[信息] 截图:"):
			captureAttempts++
		case line == "===== 任务完成 =====":
			captureFinished = true
		case screenshotUploadStartPattern.MatchString(line):
			matches := screenshotUploadStartPattern.FindStringSubmatch(line)
			uploadTotal = parseInt(matches, 1)
		case strings.HasPrefix(line, "已上传并校准域名:") || strings.HasPrefix(line, "上传失败:"):
			uploadProcessed++
		case strings.HasPrefix(line, "处理完成! 成功:"):
			uploadFinished = true
		}
	}

	if mode == screenshot.ModeLinks {
		return estimateUploadRunningProgress(requestedCount, initFinished, captureAttempts, captureFinished, uploadTotal, uploadProcessed, uploadFinished)
	}
	return estimateZipRunningProgress(requestedCount, initFinished, captureAttempts, captureFinished)
}

// estimateZipProgressFromMarkers 会优先根据截图阶段标记估算压缩包模式进度。
func estimateZipProgressFromMarkers(requestedCount int, state screenshotProgressState) *transport.TaskProgress {
	hasSubtitle := state.subtitleMarker != nil
	bootstrapFloor := bootstrapProgressPercent(state.bootstrapMarker)
	if state.packageMarker != nil {
		total := maxInt(state.packageMarker.total, 1)
		effective := markerStepProgress(state.packageMarker, 0.15)
		percent := maxFloat(zipPackageBase(hasSubtitle)+scaledProgressFloat(effective, total, zipPackageWidth()), bootstrapFloor)
		return progressSnapshot(percent, "整理结果", state.packageMarker.detail, state.packageMarker.current, total, true)
	}

	if state.renderMarker != nil || state.captureStarted > 0 || state.captureCompleted > 0 {
		total := maxInt(state.captureTotal, requestedCount)
		effective := float64(state.captureCompleted)
		detail := state.captureFinishDetail
		current := clampInt(maxInt(state.captureCompleted, state.captureStarted), 0, total)
		indeterminate := false
		if state.captureStarted > state.captureCompleted {
			if state.renderMarker != nil && state.renderMarker.percentOrder >= state.captureStartOrder && state.renderMarker.percent > 0 {
				effective += float64(state.renderMarker.percent) / 100.0
				detail = state.renderMarker.detail
				indeterminate = state.renderMarker.percent < 100
			} else {
				effective += 0.1
				detail = state.captureStartDetail
				indeterminate = true
			}
		} else if state.captureCompleted >= total {
			detail = "截图已生成，正在整理结果。"
			current = total
		}
		percent := maxFloat(zipRenderBase(hasSubtitle)+scaledProgressFloat(effective, total, zipRenderWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "生成截图", detail, current, total, indeterminate)
	}

	if state.prepMarker != nil {
		total := maxInt(state.prepMarker.total, 1)
		effective := markerStageProgress(state.prepMarker, 0.1)
		percent := maxFloat(zipPrepBase(hasSubtitle)+scaledProgressFloat(effective, total, zipPrepWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "准备截图", state.prepMarker.detail, state.prepMarker.current, total, state.prepMarker.percent <= 0)
	}

	if state.subtitleMarker != nil {
		return progressSnapshot(maxFloat(subtitleProgressPercent(state.subtitleMarker), bootstrapFloor), "准备字幕", state.subtitleMarker.detail, state.subtitleMarker.current, state.subtitleMarker.total, state.subtitleMarker.percent <= 0)
	}

	if state.bootstrapMarker != nil {
		total := maxInt(state.bootstrapMarker.total, 1)
		return progressSnapshot(bootstrapFloor, "准备任务", state.bootstrapMarker.detail, state.bootstrapMarker.current, total, state.bootstrapMarker.percent <= 0)
	}

	return nil
}

// estimateUploadProgressFromMarkers 会优先根据截图阶段标记估算图床上传模式进度。
func estimateUploadProgressFromMarkers(requestedCount int, state screenshotProgressState) *transport.TaskProgress {
	hasSubtitle := state.subtitleMarker != nil
	bootstrapFloor := bootstrapProgressPercent(state.bootstrapMarker)
	if state.uploadFinished {
		processed := state.uploadProcessed
		if state.uploadTotal > 0 {
			processed = clampInt(processed, 0, state.uploadTotal)
		}
		return progressSnapshot(97, "整理图床结果", "上传已完成，正在整理图床链接。", processed, state.uploadTotal, true)
	}

	if state.uploadTotal > 0 {
		processed := clampInt(state.uploadProcessed, 0, state.uploadTotal)
		percent := maxFloat(uploadStageBase()+scaledProgress(processed, state.uploadTotal, uploadStageWidth()), bootstrapFloor)
		return progressSnapshot(percent, "上传图床", fmt.Sprintf("已处理 %d/%d 张截图上传。", processed, state.uploadTotal), processed, state.uploadTotal, false)
	}

	if state.renderMarker != nil || state.captureStarted > 0 || state.captureCompleted > 0 {
		total := maxInt(state.captureTotal, requestedCount)
		effective := float64(state.captureCompleted)
		detail := state.captureFinishDetail
		current := clampInt(maxInt(state.captureCompleted, state.captureStarted), 0, total)
		indeterminate := false
		if state.captureStarted > state.captureCompleted {
			if state.renderMarker != nil && state.renderMarker.percentOrder >= state.captureStartOrder && state.renderMarker.percent > 0 {
				effective += float64(state.renderMarker.percent) / 100.0
				detail = state.renderMarker.detail
				indeterminate = state.renderMarker.percent < 100
			} else {
				effective += 0.1
				detail = state.captureStartDetail
				indeterminate = true
			}
		} else if state.captureCompleted >= total {
			detail = "截图已生成，正在准备上传图床。"
			current = total
		}
		percent := maxFloat(uploadRenderBase(hasSubtitle)+scaledProgressFloat(effective, total, uploadRenderWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "生成截图", detail, current, total, indeterminate)
	}

	if state.prepMarker != nil {
		total := maxInt(state.prepMarker.total, 1)
		effective := markerStageProgress(state.prepMarker, 0.1)
		percent := maxFloat(uploadPrepBase(hasSubtitle)+scaledProgressFloat(effective, total, uploadPrepWidth(hasSubtitle)), bootstrapFloor)
		return progressSnapshot(percent, "准备截图", state.prepMarker.detail, state.prepMarker.current, total, state.prepMarker.percent <= 0)
	}

	if state.subtitleMarker != nil {
		return progressSnapshot(maxFloat(subtitleProgressPercent(state.subtitleMarker), bootstrapFloor), "准备字幕", state.subtitleMarker.detail, state.subtitleMarker.current, state.subtitleMarker.total, state.subtitleMarker.percent <= 0)
	}

	if state.bootstrapMarker != nil {
		total := maxInt(state.bootstrapMarker.total, 1)
		return progressSnapshot(bootstrapFloor, "准备任务", state.bootstrapMarker.detail, state.bootstrapMarker.current, total, state.bootstrapMarker.percent <= 0)
	}

	return nil
}

// estimateZipRunningProgress 会在缺少细粒度标记时估算压缩包模式的粗略进度。
func estimateZipRunningProgress(requestedCount int, initFinished bool, captureAttempts int, captureFinished bool) *transport.TaskProgress {
	if !initFinished {
		return progressSnapshot(0, "准备任务", "正在等待耗时步骤开始。", 0, 0, true)
	}
	if captureFinished {
		return progressSnapshot(90, "打包结果", "截图已生成，正在整理下载包。", requestedCount, requestedCount, true)
	}

	processed := clampInt(captureAttempts, 0, requestedCount)
	percent := scaledProgress(processed, requestedCount, 100)
	return progressSnapshot(percent, "生成截图", fmt.Sprintf("已处理 %d/%d 个截图点。", processed, requestedCount), processed, requestedCount, false)
}

// estimateUploadRunningProgress 会在缺少细粒度标记时估算上传模式的粗略进度。
func estimateUploadRunningProgress(requestedCount int, initFinished bool, captureAttempts int, captureFinished bool, uploadTotal int, uploadProcessed int, uploadFinished bool) *transport.TaskProgress {
	if !initFinished {
		return progressSnapshot(0, "准备任务", "正在等待耗时步骤开始。", 0, 0, true)
	}

	if uploadFinished {
		processed := uploadProcessed
		if uploadTotal > 0 {
			processed = clampInt(processed, 0, uploadTotal)
		}
		return progressSnapshot(97, "整理图床结果", "上传已完成，正在整理图床链接。", processed, uploadTotal, true)
	}

	if uploadTotal > 0 {
		processed := clampInt(uploadProcessed, 0, uploadTotal)
		percent := uploadStageBase() + scaledProgress(processed, uploadTotal, uploadStageWidth())
		return progressSnapshot(percent, "上传图床", fmt.Sprintf("已处理 %d/%d 张截图上传。", processed, uploadTotal), processed, uploadTotal, false)
	}

	if captureFinished {
		return progressSnapshot(uploadStageBase(), "准备上传", "截图已生成，正在准备上传图床。", requestedCount, requestedCount, true)
	}

	processed := clampInt(captureAttempts, 0, requestedCount)
	percent := scaledProgress(processed, requestedCount, uploadRenderWidth(false))
	return progressSnapshot(percent, "生成截图", fmt.Sprintf("已处理 %d/%d 个截图点。", processed, requestedCount), processed, requestedCount, false)
}

// subtitleStageWidth 会返回字幕准备阶段在总进度中的宽度占比。
func subtitleStageWidth() int {
	return 30
}

// zipRenderBase 会返回压缩包模式中渲染阶段的起始百分比。
func zipRenderBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 35
	}
	return 0
}

// zipRenderWidth 会返回压缩包模式中渲染阶段的百分比宽度。
func zipRenderWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 55
	}
	return 90
}

// zipPrepBase 会返回压缩包模式中准备阶段的起始百分比。
func zipPrepBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 30
	}
	return 0
}

// zipPrepWidth 会返回压缩包模式中准备阶段的百分比宽度。
func zipPrepWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 5
	}
	return 0
}

// zipPackageBase 会返回压缩包模式中整理阶段的起始百分比。
func zipPackageBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 90
	}
	return 90
}

// zipPackageWidth 会返回压缩包模式中整理阶段的百分比宽度。
func zipPackageWidth() int {
	return 10
}

// uploadRenderBase 会返回上传模式中渲染阶段的起始百分比。
func uploadRenderBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 35
	}
	return 0
}

// uploadRenderWidth 会返回上传模式中渲染阶段的百分比宽度。
func uploadRenderWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 35
	}
	return 70
}

// uploadPrepBase 会返回上传模式中准备阶段的起始百分比。
func uploadPrepBase(hasSubtitle bool) float64 {
	if hasSubtitle {
		return 30
	}
	return 0
}

// uploadPrepWidth 会返回上传模式中准备阶段的百分比宽度。
func uploadPrepWidth(hasSubtitle bool) int {
	if hasSubtitle {
		return 5
	}
	return 0
}

// uploadStageBase 会返回上传阶段在整体任务中的起始百分比。
func uploadStageBase() float64 {
	return 70
}

// uploadStageWidth 会返回上传阶段在整体任务中的百分比宽度。
func uploadStageWidth() int {
	return 30
}
