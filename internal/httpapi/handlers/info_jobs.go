// Package handlers 提供 MediaInfo 和 BDInfo 后台任务的创建、取消与状态查询接口。

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/system"
)

const (
	infoKindMediaInfo = "mediainfo"
	infoKindBDInfo    = "bdinfo"

	infoJobStatusPending   = "pending"
	infoJobStatusRunning   = "running"
	infoJobStatusCanceling = "canceling"
	infoJobStatusSucceeded = "succeeded"
	infoJobStatusFailed    = "failed"
	infoJobStatusCanceled  = "canceled"
	infoJobTTL             = 30 * time.Minute
)

var infoJobs = struct {
	mu    sync.Mutex
	items map[string]*infoJob
}{
	items: make(map[string]*infoJob),
}

type infoJob struct {
	mu          sync.RWMutex
	id          string
	kind        string
	inputPath   string
	bdinfoMode  string
	status      string
	output      string
	errMessage  string
	createdAt   time.Time
	updatedAt   time.Time
	completedAt time.Time
	logger      *infoLogger
	cleanup     func()
	taskContext context.Context
	cancel      context.CancelFunc

	cancelRequested bool
}

// InfoJobsHandler 负责创建新的 MediaInfo 或 BDInfo 后台任务，并立即返回任务 ID。
func InfoJobsHandler(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		writeInfoJobError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	kind := normalizeInfoJobKind(r.FormValue("kind"))
	if kind == "" {
		writeInfoJobError(w, http.StatusBadRequest, "invalid info job kind")
		return
	}

	inputPath, cleanup, err := transport.InputPath(r)
	if err != nil {
		writeInfoJobError(w, http.StatusBadRequest, err.Error())
		return
	}

	job, err := createInfoJob(kind, inputPath, cleanup, r.FormValue("bdinfo_mode"))
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		writeInfoJobError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInfoJobResponse(w, http.StatusAccepted, job.snapshot())
}

// InfoJobHandler 返回信息类后台任务当前状态，或处理对已有任务的取消请求。
func InfoJobHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleInfoJobGet(w, r)
	case http.MethodDelete:
		handleInfoJobDelete(w, r)
	default:
		writeInfoJobError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleInfoJobGet 返回信息类后台任务当前状态，也会在完成后带上最终输出和日志。
func handleInfoJobGet(w http.ResponseWriter, r *http.Request) {
	jobID := parseInfoJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getInfoJob(jobID)
	if !ok {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	writeInfoJobResponse(w, http.StatusOK, job.snapshot())
}

// handleInfoJobDelete 会请求取消指定的信息类后台任务，并返回取消后的最新快照。
func handleInfoJobDelete(w http.ResponseWriter, r *http.Request) {
	jobID := parseInfoJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getInfoJob(jobID)
	if !ok {
		writeInfoJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job.requestCancel()
	writeInfoJobResponse(w, http.StatusOK, job.snapshot())
}

// parseInfoJobID 会从请求路径中提取信息类后台任务 ID。
func parseInfoJobID(r *http.Request) string {
	jobID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/info-jobs/"))
	return strings.Trim(jobID, "/")
}

// createInfoJob 会创建一个新的信息类后台任务，并立即启动后台执行流程。
func createInfoJob(kind, inputPath string, cleanup func(), bdinfoMode string) (*infoJob, error) {
	pruneInfoJobs(time.Now())

	jobID, err := buildInfoJobID()
	if err != nil {
		return nil, err
	}

	taskContext, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &infoJob{
		id:          jobID,
		kind:        kind,
		inputPath:   inputPath,
		bdinfoMode:  bdinfoMode,
		status:      infoJobStatusPending,
		createdAt:   now,
		updatedAt:   now,
		logger:      newInfoLogger(),
		cleanup:     cleanup,
		taskContext: taskContext,
		cancel:      cancel,
	}

	infoJobs.mu.Lock()
	infoJobs.items[jobID] = job
	infoJobs.mu.Unlock()

	go job.run()
	return job, nil
}

// getInfoJob 返回指定任务；如果任务不存在或已过期，则返回 false。
func getInfoJob(jobID string) (*infoJob, bool) {
	pruneInfoJobs(time.Now())

	infoJobs.mu.Lock()
	defer infoJobs.mu.Unlock()

	job, ok := infoJobs.items[jobID]
	if !ok {
		return nil, false
	}
	return job, true
}

// pruneInfoJobs 会删除已完成且超过保留时间的后台任务记录。
func pruneInfoJobs(now time.Time) {
	infoJobs.mu.Lock()
	defer infoJobs.mu.Unlock()

	for jobID, job := range infoJobs.items {
		if !job.expired(now) {
			continue
		}
		delete(infoJobs.items, jobID)
	}
}

// run 会在后台执行具体的信息类任务，并在结束后更新任务状态和结果。
func (j *infoJob) run() {
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

	switch j.kind {
	case infoKindMediaInfo:
		bin, err := system.ResolveBin(system.MediaInfoBinaryPath)
		if err != nil {
			j.logger.Logf("[mediainfo] 未找到可执行文件: %s", err.Error())
			j.fail(err)
			return
		}
		j.logger.Logf("[mediainfo] 输入路径: %s", j.inputPath)
		j.logger.Logf("[mediainfo] 使用命令: %s", bin)

		output, err := runMediaInfo(ctx, j.inputPath, j.logger, bin)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(output)
	case infoKindBDInfo:
		j.logger.Logf("[bdinfo] 输入路径: %s", j.inputPath)
		output, err := runBDInfo(ctx, j.inputPath, j.bdinfoMode, j.logger)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(output)
	default:
		j.fail(errors.New("unsupported info job kind"))
	}
}

// snapshot 会生成当前任务的安全快照，供 HTTP 接口直接返回。
func (j *infoJob) snapshot() transport.InfoJobResponse {
	j.mu.RLock()
	response := transport.InfoJobResponse{
		OK:     true,
		JobID:  j.id,
		Status: j.status,
		Kind:   j.kind,
		Output: j.output,
		Error:  j.errMessage,
	}
	logger := j.logger
	j.mu.RUnlock()

	var entries []transport.LogEntry
	if logger != nil {
		response.Logs = logger.String()
		entries = logger.Entries()
		response.LogEntries = entries
	}
	response.Progress = buildInfoTaskProgress(response.Kind, response.Status, entries)
	return response
}

// expired 会判断后台任务是否已经完成且超过保留时间。
func (j *infoJob) expired(now time.Time) bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	if j.completedAt.IsZero() {
		return false
	}
	return now.Sub(j.completedAt) > infoJobTTL
}

// beginRun 会把任务从 pending 切换到 running；如果任务已被取消，则返回 false。
func (j *infoJob) beginRun() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status != infoJobStatusPending {
		return false
	}
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		return false
	}

	j.status = infoJobStatusRunning
	j.updatedAt = time.Now()
	return true
}

// requestCancel 会请求取消当前任务，并立刻把状态推进到 canceling。
func (j *infoJob) requestCancel() {
	var cancel context.CancelFunc

	j.mu.Lock()
	switch j.status {
	case infoJobStatusSucceeded, infoJobStatusFailed, infoJobStatusCanceled:
		j.mu.Unlock()
		return
	case infoJobStatusCanceling:
		j.mu.Unlock()
		return
	default:
		j.cancelRequested = true
		j.status = infoJobStatusCanceling
		j.errMessage = "任务取消中。"
		j.updatedAt = time.Now()
		cancel = j.cancel
		j.mu.Unlock()
	}

	if cancel != nil {
		cancel()
	}
}

// succeed 会记录后台任务成功产出的最终输出。
func (j *infoJob) succeed(output string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = infoJobStatusCanceled
		j.output = ""
		j.errMessage = "任务已取消。"
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = infoJobStatusSucceeded
	j.output = output
	j.errMessage = ""
	j.updatedAt = now
	j.completedAt = now
}

// fail 会记录后台任务失败原因，并把状态切换为 failed。
func (j *infoJob) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	now := time.Now()
	if j.cancelRequested || isInfoJobCanceledError(err) || errors.Is(j.taskContext.Err(), context.Canceled) {
		j.status = infoJobStatusCanceled
		j.output = ""
		j.errMessage = "任务已取消。"
		j.updatedAt = now
		j.completedAt = now
		return
	}

	j.status = infoJobStatusFailed
	j.output = ""
	if err != nil {
		j.errMessage = err.Error()
	} else {
		j.errMessage = "job failed"
	}
	j.updatedAt = now
	j.completedAt = now
}

// finishCanceled 会把任务最终标记为 canceled，并记录完成时间。
func (j *infoJob) finishCanceled() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.status == infoJobStatusSucceeded || j.status == infoJobStatusFailed || j.status == infoJobStatusCanceled {
		return
	}

	now := time.Now()
	j.status = infoJobStatusCanceled
	j.output = ""
	j.errMessage = "任务已取消。"
	j.updatedAt = now
	j.completedAt = now
}

// isCancellationRequested 会判断当前任务是否已经收到了取消请求。
func (j *infoJob) isCancellationRequested() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return j.cancelRequested || errors.Is(j.taskContext.Err(), context.Canceled)
}

// normalizeInfoJobKind 规范化信息类任务种类；不支持的值返回空字符串。
func normalizeInfoJobKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case infoKindMediaInfo:
		return infoKindMediaInfo
	case infoKindBDInfo:
		return infoKindBDInfo
	default:
		return ""
	}
}

// isInfoJobCanceledError 会判断错误是否来自主动取消而不是普通失败。
func isInfoJobCanceledError(err error) bool {
	return errors.Is(err, context.Canceled)
}

// buildInfoJobID 生成适合 URL 使用的随机任务 ID。
func buildInfoJobID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// writeInfoJobResponse 会把信息类后台任务响应编码为 JSON，并显式关闭缓存。
func writeInfoJobResponse(w http.ResponseWriter, status int, payload transport.InfoJobResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeInfoJobError 会把信息类后台任务错误包装成统一 JSON 响应。
func writeInfoJobError(w http.ResponseWriter, status int, message string) {
	writeInfoJobResponse(w, status, transport.InfoJobResponse{
		OK:    false,
		Error: message,
	})
}
