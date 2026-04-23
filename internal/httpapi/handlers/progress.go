// Package handlers 提供后台任务的阶段型进度推导。

package handlers

import "minfo/internal/httpapi/transport"

// buildInfoTaskProgress 会根据任务类型、状态和日志推导信息类任务当前进度。
func buildInfoTaskProgress(kind, status string, entries []transport.LogEntry) *transport.TaskProgress {
	if kind == infoKindMediaInfo {
		return nil
	}
	running := estimateInfoTaskRunningProgress(kind, entries)
	switch status {
	case infoJobStatusSucceeded:
		return progressSnapshot(100, "已完成", "任务执行完成。", 0, 0, false)
	case infoJobStatusFailed:
		return finalizeProgress(running, "已失败", "任务执行失败。", false)
	case infoJobStatusCanceled:
		return finalizeProgress(running, "已取消", "任务已取消。", false)
	case infoJobStatusCanceling:
		return progressSnapshot(maxFloat(progressPercent(running), 10), "正在停止", "任务取消中...", progressCurrent(running), progressTotal(running), true)
	case infoJobStatusRunning:
		return running
	case infoJobStatusPending:
		fallthrough
	default:
		return progressSnapshot(6, "等待开始", "任务已提交，等待执行。", 0, 0, true)
	}
}

// buildScreenshotTaskProgress 会根据截图任务模式、状态和日志推导当前进度。
func buildScreenshotTaskProgress(mode, status string, count int, entries []transport.LogEntry) *transport.TaskProgress {
	running := estimateScreenshotTaskRunningProgress(mode, count, entries)
	switch status {
	case screenshotJobStatusSucceeded:
		return progressSnapshot(100, "已完成", "任务执行完成。", 0, 0, false)
	case screenshotJobStatusFailed:
		return finalizeProgress(running, "已失败", "任务执行失败。", false)
	case screenshotJobStatusCanceled:
		return finalizeProgress(running, "已取消", "任务已取消。", false)
	case screenshotJobStatusCanceling:
		return progressSnapshot(maxFloat(progressPercent(running), 10), "正在停止", "任务取消中...", progressCurrent(running), progressTotal(running), true)
	case screenshotJobStatusRunning:
		return running
	case screenshotJobStatusPending:
		fallthrough
	default:
		return progressSnapshot(0, "等待开始", "任务已提交，等待执行。", 0, 0, true)
	}
}
