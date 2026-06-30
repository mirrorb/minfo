package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"time"

	"minfo/internal/httpapi/transport"
	"minfo/internal/torrent"
)

const (
	torrentJobStatusPending   = "pending"
	torrentJobStatusRunning   = "running"
	torrentJobStatusCanceling = "canceling"
	torrentJobStatusSucceeded = "succeeded"
	torrentJobStatusFailed    = "failed"
	torrentJobStatusCanceled  = "canceled"
	torrentJobTTL             = 30 * time.Minute
)

var torrentJobs = struct {
	mu    sync.Mutex
	items map[string]*torrentJob
}{
	items: make(map[string]*torrentJob),
}

type torrentJob struct {
	mu          sync.RWMutex
	id          string
	inputPath   string
	options     torrent.Options
	status      string
	output      string
	downloadURL string
	outputPath  string
	filename    string
	errMessage  string
	progress    *transport.TaskProgress
	createdAt   time.Time
	updatedAt   time.Time
	completedAt time.Time
	logger      *infoLogger
	cleanup     func()
	tempDir     string
	taskContext context.Context
	cancel      context.CancelFunc

	cancelRequested bool
}

func createTorrentJob(request torrentRequest) (*torrentJob, error) {
	pruneTorrentJobs(time.Now())

	jobID, err := buildTorrentJobID()
	if err != nil {
		return nil, err
	}

	taskContext, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &torrentJob{
		id:          jobID,
		inputPath:   request.InputPath,
		options:     request.Options,
		status:      torrentJobStatusPending,
		createdAt:   now,
		updatedAt:   now,
		logger:      newInfoLogger(),
		cleanup:     request.Cleanup,
		taskContext: taskContext,
		cancel:      cancel,
	}

	torrentJobs.mu.Lock()
	torrentJobs.items[jobID] = job
	torrentJobs.mu.Unlock()

	go job.run()
	return job, nil
}

func getTorrentJob(jobID string) (*torrentJob, bool) {
	pruneTorrentJobs(time.Now())

	torrentJobs.mu.Lock()
	defer torrentJobs.mu.Unlock()

	job, ok := torrentJobs.items[jobID]
	if !ok {
		return nil, false
	}
	return job, true
}

func pruneTorrentJobs(now time.Time) {
	torrentJobs.mu.Lock()
	defer torrentJobs.mu.Unlock()

	for jobID, job := range torrentJobs.items {
		if !job.expired(now) {
			continue
		}
		job.removeTempDir()
		delete(torrentJobs.items, jobID)
	}
}

func buildTorrentJobID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (j *torrentJob) removeTempDir() {
	j.mu.RLock()
	tempDir := j.tempDir
	j.mu.RUnlock()
	if tempDir != "" {
		_ = os.RemoveAll(tempDir)
	}
}
