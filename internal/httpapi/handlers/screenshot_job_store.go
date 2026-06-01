// Package handlers 提供截图后台任务的存储与创建逻辑。

package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"minfo/internal/httpapi/transport"
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
	hdrProcessor    string
	count           int
	proxyURL        string
	status          string
	output          string
	downloadURL     string
	linkItems       []transport.ImageLinkItem
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

// createScreenshotJob 会创建一个新的截图后台任务，并立即启动后台执行流程。
func createScreenshotJob(request screenshotRequest) (*screenshotJob, error) {
	pruneScreenshotJobs(time.Now())

	jobID, err := buildScreenshotJobID()
	if err != nil {
		return nil, err
	}

	taskContext, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &screenshotJob{
		id:           jobID,
		mode:         request.Mode,
		inputPath:    request.InputPath,
		variant:      request.Variant,
		subtitleMode: request.SubtitleMode,
		hdrProcessor: request.HDRProcessor,
		count:        request.Count,
		proxyURL:     request.ProxyURL,
		status:       screenshotJobStatusPending,
		createdAt:    now,
		updatedAt:    now,
		logger:       newInfoLogger(),
		cleanup:      request.Cleanup,
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

// buildScreenshotJobID 生成适合 URL 使用的随机任务 ID。
func buildScreenshotJobID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
