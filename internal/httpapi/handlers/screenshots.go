package handlers

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"minfo/internal/config"
	"minfo/internal/httpapi/logstream"
	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
	"minfo/internal/screenshot"
)

func ScreenshotsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleScreenshotZipDownload(w, r)
	case http.MethodHead:
		handleScreenshotZipDownload(w, r)
	case http.MethodPost:
		handleScreenshotsPost(w, r)
	default:
		transport.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleScreenshotsPost(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		transport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	logger := newInfoLogger(logstream.Open(r.FormValue("log_session")))
	defer logger.Close()

	path, cleanup, err := transport.InputPath(r)
	if err != nil {
		transport.WriteJSON(w, http.StatusBadRequest, transport.InfoResponse{OK: false, Error: err.Error(), Logs: logger.String()})
		return
	}
	defer cleanup()

	mode := screenshot.NormalizeMode(r.FormValue("mode"))
	variant := screenshot.NormalizeVariant(r.FormValue("variant"))
	subtitleMode := screenshot.NormalizeSubtitleMode(r.FormValue("subtitle_mode"))
	count := screenshot.NormalizeCount(r.FormValue("count"))

	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "minfo-shots-*")
	if err != nil {
		transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{OK: false, Error: err.Error(), Logs: logger.String()})
		return
	}
	defer os.RemoveAll(tempDir)

	if mode == screenshot.ModeLinks {
		result, err := screenshot.RunUploadWithLiveLogs(ctx, path, tempDir, variant, subtitleMode, count, logger.LogLine)
		if err != nil {
			transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{OK: false, Error: err.Error(), Logs: pickRealtimeLogs(logger, result.Logs)})
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{OK: true, Output: result.Output, Logs: pickRealtimeLogs(logger, result.Logs)})
		return
	}

	if shouldPrepareDownload(r) {
		downloadURL, logs, err := prepareScreenshotZipDownload(ctx, path, tempDir, variant, subtitleMode, count, logger.LogLine)
		if err != nil {
			transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{OK: false, Error: err.Error(), Logs: pickRealtimeLogs(logger, logs)})
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{OK: true, Output: downloadURL, Logs: pickRealtimeLogs(logger, logs)})
		return
	}

	if err := writeScreenshotZipResponse(ctx, w, path, tempDir, variant, subtitleMode, count); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

func handleScreenshotZipDownload(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token != "" {
		servePreparedScreenshotDownload(w, r, token)
		return
	}

	path, cleanup, err := inputPathFromQuery(r)
	if err != nil {
		transport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()
	variant := screenshot.NormalizeVariant(r.URL.Query().Get("variant"))
	subtitleMode := screenshot.NormalizeSubtitleMode(r.URL.Query().Get("subtitle_mode"))
	count := screenshot.NormalizeCount(r.URL.Query().Get("count"))

	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "minfo-shots-*")
	if err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	if err := writeScreenshotZipResponse(ctx, w, path, tempDir, variant, subtitleMode, count); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

func shouldPrepareDownload(r *http.Request) bool {
	return strings.TrimSpace(r.FormValue("prepare_download")) == "1"
}

func prepareScreenshotZipDownload(ctx context.Context, path, tempDir, variant, subtitleMode string, count int, onLog screenshot.LogHandler) (string, string, error) {
	zipBytes, logs, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return "", logs, err
	}

	token, err := screenshot.SavePreparedDownload(zipBytes)
	if err != nil {
		return "", logs, err
	}
	return "/api/screenshots?token=" + token, logs, nil
}

func writeScreenshotZipResponse(ctx context.Context, w http.ResponseWriter, path, tempDir, variant, subtitleMode string, count int) error {
	zipBytes, _, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode, count, nil)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"screenshots.zip\"")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(zipBytes); err != nil {
		log.Printf("write response: %v", err)
	}
	return nil
}

func generateScreenshotZip(ctx context.Context, path, tempDir, variant, subtitleMode string, count int, onLog screenshot.LogHandler) ([]byte, string, error) {
	result, err := screenshot.RunScriptWithLiveLogs(ctx, path, tempDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return nil, result.Logs, err
	}

	zipBytes, err := screenshot.ZipFiles(result.Files)
	if err != nil {
		return nil, result.Logs, err
	}
	return zipBytes, result.Logs, nil
}

func pickRealtimeLogs(logger *infoLogger, fallback string) string {
	if logger == nil {
		return fallback
	}
	if logs := logger.String(); strings.TrimSpace(logs) != "" {
		return logs
	}
	return fallback
}

func servePreparedScreenshotDownload(w http.ResponseWriter, r *http.Request, token string) {
	filePath, err := screenshot.GetPreparedDownload(token)
	if err != nil {
		transport.WriteError(w, http.StatusNotFound, "download expired or not found")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"screenshots.zip\"")
	http.ServeFile(w, r, filePath)
}

func inputPathFromQuery(r *http.Request) (string, func(), error) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	path = strings.Trim(path, "\"")
	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()
	return media.ResolveInputPath(ctx, path)
}
