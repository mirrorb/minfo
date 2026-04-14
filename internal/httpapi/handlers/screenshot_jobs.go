// Package handlers 提供截图后台任务的创建、取消与状态查询接口。

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

const (
	screenshotJobStatusPending   = "pending"
	screenshotJobStatusRunning   = "running"
	screenshotJobStatusCanceling = "canceling"
	screenshotJobStatusSucceeded = "succeeded"
	screenshotJobStatusFailed    = "failed"
	screenshotJobStatusCanceled  = "canceled"
	screenshotJobTTL             = 30 * time.Minute
)

var screenshotJobs = struct {
	mu    sync.Mutex
	items map[string]*screenshotJob
}{
	items: make(map[string]*screenshotJob),
}

type screenshotJob struct {
	mu              sync.RWMutex
	id              string
	mode            string
	inputPath       string
	variant         string
	subtitleMode    string
	count           int
	status          string
	output          string
	downloadURL     string
	pngLossyFiles   []string
	pngLossyIndexes []int
	errMessage      string
	createdAt       time.Time
	updatedAt       time.Time
	completedAt     time.Time
	logger          *infoLogger
	cleanup         func()
	taskContext     context.Context
	cancel          context.CancelFunc

	cancelRequested bool
}

// ScreenshotJobsHandler 负责创建新的截图后台任务，并立即返回任务 ID。
func ScreenshotJobsHandler(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		writeScreenshotJobError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	inputPath, cleanup, err := transport.InputPath(r)
	if err != nil {
		writeScreenshotJobError(w, http.StatusBadRequest, err.Error())
		return
	}

	mode := screenshot.NormalizeMode(r.FormValue("mode"))
	variant := screenshot.NormalizeVariant(r.FormValue("variant"))
	subtitleMode := screenshot.NormalizeSubtitleMode(r.FormValue("subtitle_mode"))
	count := screenshot.NormalizeCount(r.FormValue("count"))

	job, err := createScreenshotJob(mode, inputPath, cleanup, variant, subtitleMode, count)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		writeScreenshotJobError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeScreenshotJobResponse(w, http.StatusAccepted, job.snapshot())
}

// ScreenshotJobHandler 返回截图后台任务当前状态，或处理对已有任务的取消请求。
func ScreenshotJobHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleScreenshotJobGet(w, r)
	case http.MethodDelete:
		handleScreenshotJobDelete(w, r)
	default:
		writeScreenshotJobError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleScreenshotJobGet 返回截图后台任务当前状态，也会在完成后带上最终结果和日志。
func handleScreenshotJobGet(w http.ResponseWriter, r *http.Request) {
	jobID := parseScreenshotJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getScreenshotJob(jobID)
	if !ok {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	writeScreenshotJobResponse(w, http.StatusOK, job.snapshot())
}

// handleScreenshotJobDelete 会请求取消指定的截图后台任务，并返回取消后的最新快照。
func handleScreenshotJobDelete(w http.ResponseWriter, r *http.Request) {
	jobID := parseScreenshotJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getScreenshotJob(jobID)
	if !ok {
		writeScreenshotJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job.requestCancel()
	writeScreenshotJobResponse(w, http.StatusOK, job.snapshot())
}

// parseScreenshotJobID 会从请求路径中提取截图后台任务 ID。
func parseScreenshotJobID(r *http.Request) string {
	jobID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/screenshot-jobs/"))
	return strings.Trim(jobID, "/")
}

// createScreenshotJob 会创建一个新的截图后台任务，并立即启动后台执行流程。
func createScreenshotJob(mode, inputPath string, cleanup func(), variant, subtitleMode string, count int) (*screenshotJob, error) {
	pruneScreenshotJobs(time.Now())

	jobID, err := buildScreenshotJobID()
	if err != nil {
		return nil, err
	}

	taskContext, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &screenshotJob{
		id:           jobID,
		mode:         mode,
		inputPath:    inputPath,
		variant:      variant,
		subtitleMode: subtitleMode,
		count:        count,
		status:       screenshotJobStatusPending,
		createdAt:    now,
		updatedAt:    now,
		logger:       newInfoLogger(),
		cleanup:      cleanup,
		taskContext:  taskContext,
		cancel:       cancel,
	}

	screenshotJobs.mu.Lock()
	screenshotJobs.items[jobID] = job
	screenshotJobs.mu.Unlock()

	go job.run()
	return job, nil
}

// getScreenshotJob 返回指定任务；如果任务不存在或已过期，则返回 false。
func getScreenshotJob(jobID string) (*screenshotJob, bool) {
	pruneScreenshotJobs(time.Now())

	screenshotJobs.mu.Lock()
	defer screenshotJobs.mu.Unlock()

	job, ok := screenshotJobs.items[jobID]
	if !ok {
		return nil, false
	}
	return job, true
}

// pruneScreenshotJobs 会删除已完成且超过保留时间的后台任务记录。
func pruneScreenshotJobs(now time.Time) {
	screenshotJobs.mu.Lock()
	defer screenshotJobs.mu.Unlock()

	for jobID, job := range screenshotJobs.items {
		if !job.expired(now) {
			continue
		}
		delete(screenshotJobs.items, jobID)
	}
}

// run 会在后台执行具体截图任务，并在结束后更新任务状态和结果。
func (j *screenshotJob) run() {
	defer func() {
		if j.cancel != nil {
			j.cancel()
		}
		if j.cleanup != nil {
			j.cleanup()
		}
		if j.logger != nil {
			j.logger.Close()
		}
	}()

	if !j.beginRun() {
		if j.isCancellationRequested() {
			j.finishCanceled()
		}
		return
	}

	ctx, cancel := context.WithTimeout(j.taskContext, config.RequestTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "minfo-screenshot-job-*")
	if err != nil {
		j.fail(err)
		return
	}
	defer os.RemoveAll(tempDir)

	switch j.mode {
	case screenshot.ModeLinks:
		result, err := screenshot.RunUploadWithLiveLogs(ctx, j.inputPath, tempDir, j.variant, j.subtitleMode, j.count, j.logger.LogLine)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(result.Output, "", result.LossyPNGFiles, result.LossyPNGIndexes)
	default:
		downloadURL, _, err := prepareScreenshotZipDownload(ctx, j.inputPath, tempDir, j.variant, j.subtitleMode, j.count, j.logger.LogLine)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed("", downloadURL, nil, nil)
	}
}

// snapshot 会生成当前任务的安全快照，供 HTTP 接口直接返回。
func (j *screenshotJob) snapshot() transport.ScreenshotJobResponse {
	j.mu.RLock()
	count := j.count
	response := transport.ScreenshotJobResponse{
		OK:              true,
		JobID:           j.id,
		Status:          j.status,
		Mode:            j.mode,
		Output:          j.output,
		DownloadURL:     j.downloadURL,
		Error:           j.errMessage,
		PNGLossyFiles:   append([]string(nil), j.pngLossyFiles...),
		PNGLossyIndexes: append([]int(nil), j.pngLossyIndexes...),
	}
	logger := j.logger
	j.mu.RUnlock()

	var entries []transport.LogEntry
	if logger != nil {
		response.Logs = logger.String()
		entries = logger.Entries()
		response.LogEntries = entries
	}
	response.Progress = buildScreenshotTaskProgress(response.Mode, response.Status, count, response.LogEntries)
	return response
}

// expired 会判断后台任务是否已经完成且超过保留时间。
func (j *screenshotJob) expired(now time.Time) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.completedAt.IsZero() {
		return false
	}
	return now.Sub(j.completedAt) > screenshotJobTTL
}

// beginRun 会把任务从 pending 切换到 running；如果任务已被取消，则返回 false。
func (j *screenshotJob) beginRun() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status != screenshotJobStatusPending {
		return false
	}
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		return false
	}

	j.status = screenshotJobStatusRunning
	j.updatedAt = time.Now()
	return true
}

// requestCancel 会请求取消当前任务，并立刻把状态推进到 canceling。
func (j *screenshotJob) requestCancel() {
	var cancel context.CancelFunc

	j.mu.Lock()
	switch j.status {
	case screenshotJobStatusSucceeded, screenshotJobStatusFailed, screenshotJobStatusCanceled:
		j.mu.Unlock()
		return
	case screenshotJobStatusCanceling:
		j.mu.Unlock()
		return
	default:
		j.cancelRequested = true
		j.status = screenshotJobStatusCanceling
		j.errMessage = "任务取消中。"
		j.updatedAt = time.Now()
		cancel = j.cancel
		j.mu.Unlock()
	}

	if cancel != nil {
		cancel()
	}
}

// succeed 会记录后台任务成功产出的最终结果。
func (j *screenshotJob) succeed(output, downloadURL string, pngLossyFiles []string, pngLossyIndexes []int) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = screenshotJobStatusCanceled
		j.output = ""
		j.downloadURL = ""
		j.pngLossyFiles = nil
		j.pngLossyIndexes = nil
		j.errMessage = "任务已取消。"
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = screenshotJobStatusSucceeded
	j.output = output
	j.downloadURL = downloadURL
	j.pngLossyFiles = append([]string(nil), pngLossyFiles...)
	j.pngLossyIndexes = append([]int(nil), pngLossyIndexes...)
	j.errMessage = ""
	j.updatedAt = now
	j.completedAt = now
}

// fail 会记录后台任务失败原因，并把状态切换为 failed。
func (j *screenshotJob) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || isScreenshotJobCanceledError(err) || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = screenshotJobStatusCanceled
		j.output = ""
		j.downloadURL = ""
		j.pngLossyFiles = nil
		j.pngLossyIndexes = nil
		j.errMessage = "任务已取消。"
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = screenshotJobStatusFailed
	j.output = ""
	j.downloadURL = ""
	j.pngLossyFiles = nil
	j.pngLossyIndexes = nil
	if err != nil {
		j.errMessage = err.Error()
	} else {
		j.errMessage = "job failed"
	}
	j.updatedAt = now
	j.completedAt = now
}

// finishCanceled 会把任务最终标记为 canceled，并记录完成时间。
func (j *screenshotJob) finishCanceled() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status == screenshotJobStatusSucceeded || j.status == screenshotJobStatusFailed || j.status == screenshotJobStatusCanceled {
		return
	}

	now := time.Now()
	j.status = screenshotJobStatusCanceled
	j.output = ""
	j.downloadURL = ""
	j.pngLossyFiles = nil
	j.pngLossyIndexes = nil
	j.errMessage = "任务已取消。"
	j.updatedAt = now
	j.completedAt = now
}

// isCancellationRequested 会判断当前任务是否已经收到了取消请求。
func (j *screenshotJob) isCancellationRequested() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled)
}

// isScreenshotJobCanceledError 会判断错误是否来自主动取消而不是普通失败。
func isScreenshotJobCanceledError(err error) bool {
	return errors.Is(err, context.Canceled)
}

// buildScreenshotJobID 生成适合 URL 使用的随机任务 ID。
func buildScreenshotJobID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// writeScreenshotJobResponse 会把截图后台任务响应编码为 JSON，并显式关闭缓存。
func writeScreenshotJobResponse(w http.ResponseWriter, status int, payload transport.ScreenshotJobResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeScreenshotJobError 会把后台任务相关错误包装成统一 JSON 响应。
func writeScreenshotJobError(w http.ResponseWriter, status int, message string) {
	writeScreenshotJobResponse(w, status, transport.ScreenshotJobResponse{
		OK:    false,
		Error: message,
	})
}
