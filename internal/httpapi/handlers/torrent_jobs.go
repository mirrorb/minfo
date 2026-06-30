package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"minfo/internal/httpapi/transport"
)

// TorrentJobsHandler creates a torrent generation job and immediately returns its ID.
func TorrentJobsHandler(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		writeTorrentJobError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	request, err := parseTorrentFormRequest(r)
	if err != nil {
		writeTorrentJobError(w, http.StatusBadRequest, err.Error())
		return
	}

	job, err := createTorrentJob(request)
	if err != nil {
		if request.Cleanup != nil {
			request.Cleanup()
		}
		writeTorrentJobError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTorrentJobResponse(w, http.StatusAccepted, job.snapshot())
}

// TorrentJobHandler returns torrent job status, handles cancel, or serves the generated file.
func TorrentJobHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/download") {
		handleTorrentJobDownload(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		handleTorrentJobGet(w, r)
	case http.MethodDelete:
		handleTorrentJobDelete(w, r)
	default:
		writeTorrentJobError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleTorrentJobGet(w http.ResponseWriter, r *http.Request) {
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

	writeTorrentJobResponse(w, http.StatusOK, job.snapshot())
}

func handleTorrentJobDelete(w http.ResponseWriter, r *http.Request) {
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

	job.requestCancel()
	writeTorrentJobResponse(w, http.StatusOK, job.snapshot())
}

func parseTorrentJobID(r *http.Request) string {
	path := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/torrent-jobs/"))
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, "/download")
	return strings.Trim(path, "/")
}

func writeTorrentJobResponse(w http.ResponseWriter, status int, payload transport.TorrentJobResponse) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeTorrentJobError(w http.ResponseWriter, status int, message string) {
	writeTorrentJobResponse(w, status, transport.TorrentJobResponse{
		OK:    false,
		Error: message,
	})
}
