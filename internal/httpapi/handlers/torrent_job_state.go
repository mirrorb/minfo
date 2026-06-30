package handlers

import (
	"context"
	"errors"
	"time"

	"minfo/internal/httpapi/transport"
)

func (j *torrentJob) snapshot() transport.TorrentJobResponse {
	j.mu.RLock()
	response := transport.TorrentJobResponse{
		OK:          true,
		JobID:       j.id,
		Status:      j.status,
		Output:      j.output,
		DownloadURL: j.downloadURL,
		Error:       j.errMessage,
		Progress:    cloneTaskProgress(j.progress),
	}
	logger := j.logger
	j.mu.RUnlock()

	if logger != nil {
		response.Logs = logger.String()
		response.LogEntries = logger.Entries()
	}
	if response.Progress == nil {
		response.Progress = buildTorrentFallbackProgress(response.Status)
	}
	return response
}

func (j *torrentJob) expired(now time.Time) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.completedAt.IsZero() {
		return false
	}
	return now.Sub(j.completedAt) > torrentJobTTL
}

func (j *torrentJob) beginRun() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status != torrentJobStatusPending {
		return false
	}
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		return false
	}

	j.status = torrentJobStatusRunning
	j.progress = progressSnapshot(1, "准备", "正在准备制作种子。", 0, 0, true)
	j.updatedAt = time.Now()
	return true
}

func (j *torrentJob) requestCancel() {
	var cancel context.CancelFunc

	j.mu.Lock()
	switch j.status {
	case torrentJobStatusSucceeded, torrentJobStatusFailed, torrentJobStatusCanceled:
		j.mu.Unlock()
		return
	case torrentJobStatusCanceling:
		j.mu.Unlock()
		return
	default:
		j.cancelRequested = true
		j.status = torrentJobStatusCanceling
		j.errMessage = "任务取消中。"
		j.progress = progressSnapshot(progressPercent(j.progress), "取消中", "正在停止制种任务。", 0, 0, true)
		j.updatedAt = time.Now()
		cancel = j.cancel
		j.mu.Unlock()
	}

	if cancel != nil {
		cancel()
	}
}

func (j *torrentJob) updateProgress(progress *transport.TaskProgress) {
	if progress == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status != torrentJobStatusRunning {
		return
	}
	j.progress = progress
	j.updatedAt = time.Now()
}

func (j *torrentJob) succeed(output, downloadURL, outputPath, filename string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = torrentJobStatusCanceled
		j.output = ""
		j.downloadURL = ""
		j.outputPath = ""
		j.filename = ""
		j.errMessage = "任务已取消。"
		j.progress = progressSnapshot(progressPercent(j.progress), "已取消", "制种任务已取消。", 0, 0, true)
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = torrentJobStatusSucceeded
	j.output = output
	j.downloadURL = downloadURL
	j.outputPath = outputPath
	j.filename = filename
	j.errMessage = ""
	j.progress = progressSnapshot(100, "完成", "种子已生成，正在准备下载。", 0, 0, false)
	j.updatedAt = now
	j.completedAt = now
}

func (j *torrentJob) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || errors.Is(err, context.Canceled) || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = torrentJobStatusCanceled
		j.output = ""
		j.downloadURL = ""
		j.outputPath = ""
		j.filename = ""
		j.errMessage = "任务已取消。"
		j.progress = progressSnapshot(progressPercent(j.progress), "已取消", "制种任务已取消。", 0, 0, true)
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = torrentJobStatusFailed
	j.output = ""
	j.downloadURL = ""
	j.outputPath = ""
	j.filename = ""
	if err != nil {
		j.errMessage = err.Error()
	} else {
		j.errMessage = "job failed"
	}
	j.progress = progressSnapshot(progressPercent(j.progress), "失败", "制作种子失败。", 0, 0, false)
	j.updatedAt = now
	j.completedAt = now
}

func (j *torrentJob) finishCanceled() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status == torrentJobStatusSucceeded || j.status == torrentJobStatusFailed || j.status == torrentJobStatusCanceled {
		return
	}

	now := time.Now()
	j.status = torrentJobStatusCanceled
	j.output = ""
	j.downloadURL = ""
	j.outputPath = ""
	j.filename = ""
	j.errMessage = "任务已取消。"
	j.progress = progressSnapshot(progressPercent(j.progress), "已取消", "制种任务已取消。", 0, 0, true)
	j.updatedAt = now
	j.completedAt = now
}

func (j *torrentJob) isCancellationRequested() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled)
}

func cloneTaskProgress(progress *transport.TaskProgress) *transport.TaskProgress {
	if progress == nil {
		return nil
	}
	copy := *progress
	return &copy
}

func buildTorrentFallbackProgress(status string) *transport.TaskProgress {
	switch status {
	case torrentJobStatusPending:
		return progressSnapshot(1, "等待中", "制种任务等待开始。", 0, 0, true)
	case torrentJobStatusRunning:
		return progressSnapshot(5, "制作中", "正在制作种子。", 0, 0, true)
	case torrentJobStatusCanceling:
		return progressSnapshot(5, "取消中", "正在停止制种任务。", 0, 0, true)
	case torrentJobStatusSucceeded:
		return progressSnapshot(100, "完成", "种子已生成。", 0, 0, false)
	case torrentJobStatusCanceled:
		return progressSnapshot(0, "已取消", "制种任务已取消。", 0, 0, true)
	case torrentJobStatusFailed:
		return progressSnapshot(0, "失败", "制作种子失败。", 0, 0, false)
	default:
		return nil
	}
}
