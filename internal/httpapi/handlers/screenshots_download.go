package handlers

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
	"minfo/internal/screenshot"
	screenshotdelivery "minfo/internal/screenshot/delivery"
	screenshotprogress "minfo/internal/screenshot/progress"
)

// handleScreenshotZipDownload 处理截图压缩包下载请求，也支持按令牌获取已准备好的 ZIP 文件。
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
	options := normalizeScreenshotQueryOptions(r)

	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()

	tempDir, err := createScreenshotTempDir("minfo-shots-*")
	if err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	if err := writeScreenshotZipResponse(ctx, w, path, tempDir, options.Variant, options.SubtitleMode, options.HDRProcessor, options.Count); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

// shouldPrepareDownload 会判断Prepare下载是否应该走当前分支。
func shouldPrepareDownload(r *http.Request) bool {
	return strings.TrimSpace(r.FormValue("prepare_download")) == "1"
}

// prepareScreenshotZipDownload 生成截图压缩包并保存到临时下载缓存，返回可复用的下载地址。
func prepareScreenshotZipDownload(ctx context.Context, path, tempDir, variant, subtitleMode, hdrProcessor string, count int, onLog screenshot.LogHandler) (string, string, error) {
	zipBytes, logs, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode, hdrProcessor, count, onLog)
	if err != nil {
		return "", logs, err
	}

	screenshotprogress.EmitStepLog(onLog, "整理", 4, 4, "正在写入下载缓存。")
	token, err := screenshotdelivery.SavePreparedDownload(zipBytes)
	if err != nil {
		return "", logs, err
	}
	return "/api/screenshots?token=" + token, logs, nil
}

// writeScreenshotZipResponse 生成截图压缩包并直接以附件形式写回响应。
func writeScreenshotZipResponse(ctx context.Context, w http.ResponseWriter, path, tempDir, variant, subtitleMode, hdrProcessor string, count int) error {
	zipBytes, _, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode, hdrProcessor, count, nil)
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

// servePreparedScreenshotDownload 根据令牌读取已准备好的 ZIP 文件并交给 HTTP 层输出。
func servePreparedScreenshotDownload(w http.ResponseWriter, r *http.Request, token string) {
	filePath, err := screenshotdelivery.GetPreparedDownload(token)
	if err != nil {
		transport.WriteError(w, http.StatusNotFound, "download expired or not found")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"screenshots.zip\"")
	http.ServeFile(w, r, filePath)
}

// inputPathFromQuery 从查询参数解析输入路径，并复用媒体路径解析逻辑处理 ISO 虚拟路径。
func inputPathFromQuery(r *http.Request) (string, func(), error) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	path = strings.Trim(path, "\"")
	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()
	return media.ResolveInputPath(ctx, path)
}
