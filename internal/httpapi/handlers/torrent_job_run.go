package handlers

import (
	"context"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/torrent"
)

func (j *torrentJob) run() {
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

	tempDir, err := os.MkdirTemp("", "minfo-torrent-job-*")
	if err != nil {
		j.fail(err)
		return
	}
	j.mu.Lock()
	j.tempDir = tempDir
	j.mu.Unlock()

	outputPath := filepath.Join(tempDir, "output.torrent")
	j.logger.Logf("[torrent] 输入路径: %s", j.inputPath)

	onLine := func(stream, line string) {
		j.handleTorrentCommandLine(stream, line)
	}
	filename, err := torrent.Create(ctx, j.inputPath, outputPath, j.options, onLine)
	if err != nil {
		j.fail(err)
		return
	}

	downloadURL := "/api/torrent-jobs/" + j.id + "/download"
	j.logger.Logf("[torrent] 完成: %s", filename)
	j.succeed("种子已生成。", downloadURL, outputPath, filename)
}

func (j *torrentJob) handleTorrentCommandLine(stream, line string) {
	cleaned := torrent.StripANSI(strings.TrimSpace(line))
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return
	}

	if parsed, ok := torrent.ParseProgressLine(line); ok {
		j.updateProgress(torrentProgressSnapshot(parsed))
		return
	}
	j.logger.Logf("[mkbrr][%s] %s", stream, cleaned)
}

func torrentProgressSnapshot(progress torrent.Progress) *transport.TaskProgress {
	percent := progress.Percent
	indeterminate := percent <= 0 && !progress.Done
	if progress.Stage == "正在哈希" && percent < 2 {
		percent = 2
	}
	return progressSnapshot(percent, progress.Stage, progress.Detail, 0, 0, indeterminate)
}

func handleTorrentJobDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeTorrentJobError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	jobID := parseTorrentJobID(r)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeTorrentJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job, ok := getTorrentJob(jobID)
	if !ok {
		writeTorrentJobError(w, http.StatusNotFound, "job not found")
		return
	}

	job.mu.RLock()
	status := job.status
	outputPath := job.outputPath
	filename := job.filename
	job.mu.RUnlock()

	if status != torrentJobStatusSucceeded || outputPath == "" {
		writeTorrentJobError(w, http.StatusNotFound, "torrent file is not ready")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	http.ServeFile(w, r, outputPath)
}
