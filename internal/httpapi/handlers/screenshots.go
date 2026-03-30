package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
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

	path, cleanup, err := transport.InputPath(r)
	if err != nil {
		transport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()

	mode := screenshot.NormalizeMode(r.FormValue("mode"))
	variant := screenshot.NormalizeVariant(r.FormValue("variant"))
	subtitleMode := screenshot.NormalizeSubtitleMode(r.FormValue("subtitle_mode"))

	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "minfo-shots-*")
	if err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	if mode == screenshot.ModeLinks {
		result, err := screenshot.RunUploadWithLogs(ctx, path, tempDir, variant, subtitleMode)
		if err != nil {
			transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{OK: false, Error: err.Error(), Logs: result.Logs})
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{OK: true, Output: result.Output, Logs: result.Logs})
		return
	}

	if shouldPrepareDownload(r) {
		downloadURL, logs, err := prepareScreenshotZipDownload(ctx, path, tempDir, variant, subtitleMode)
		if err != nil {
			transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{OK: false, Error: err.Error(), Logs: logs})
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{OK: true, Output: downloadURL, Logs: logs})
		return
	}

	if err := writeScreenshotZipResponse(ctx, w, path, tempDir, variant, subtitleMode); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

func handleScreenshotZipDownload(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token != "" {
		servePreparedScreenshotDownload(w, r, token)
		return
	}

	path, err := inputPathFromQuery(r)
	if err != nil {
		transport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	variant := screenshot.NormalizeVariant(r.URL.Query().Get("variant"))
	subtitleMode := screenshot.NormalizeSubtitleMode(r.URL.Query().Get("subtitle_mode"))

	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "minfo-shots-*")
	if err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	if err := writeScreenshotZipResponse(ctx, w, path, tempDir, variant, subtitleMode); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

func shouldPrepareDownload(r *http.Request) bool {
	return strings.TrimSpace(r.FormValue("prepare_download")) == "1"
}

func prepareScreenshotZipDownload(ctx context.Context, path, tempDir, variant, subtitleMode string) (string, string, error) {
	zipBytes, logs, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode)
	if err != nil {
		return "", logs, err
	}

	token, err := screenshot.SavePreparedDownload(zipBytes)
	if err != nil {
		return "", logs, err
	}
	return "/api/screenshots?token=" + token, logs, nil
}

func writeScreenshotZipResponse(ctx context.Context, w http.ResponseWriter, path, tempDir, variant, subtitleMode string) error {
	zipBytes, _, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode)
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

func generateScreenshotZip(ctx context.Context, path, tempDir, variant, subtitleMode string) ([]byte, string, error) {
	result, err := screenshot.RunScriptWithLogs(ctx, path, tempDir, variant, subtitleMode)
	if err != nil {
		return nil, result.Logs, err
	}

	zipBytes, err := screenshot.ZipFiles(result.Files)
	if err != nil {
		return nil, result.Logs, err
	}
	return zipBytes, result.Logs, nil
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

func inputPathFromQuery(r *http.Request) (string, error) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	path = strings.Trim(path, "\"")
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("path not found: %v", err)
	}
	return path, nil
}
