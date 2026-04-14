// Package handlers 提供截图生成、上传和下载接口。

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
)

// ScreenshotsHandler 会根据请求方法分发截图生成、预下载和文件下载逻辑。
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

// handleScreenshotsPost 处理截图或上传请求，并根据模式决定返回压缩包还是图片链接。
func handleScreenshotsPost(w http.ResponseWriter, r *http.Request) {
	if !transport.EnsurePost(w, r) {
		return
	}
	if err := transport.ParseForm(w, r); err != nil {
		transport.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer transport.CleanupMultipart(r)

	logger := newInfoLogger()
	defer logger.Close()

	path, cleanup, err := transport.InputPath(r)
	if err != nil {
		transport.WriteJSON(w, http.StatusBadRequest, transport.InfoResponse{
			OK:         false,
			Error:      err.Error(),
			Logs:       logger.String(),
			LogEntries: pickRealtimeLogEntries(logger),
		})
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
		transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{
			OK:         false,
			Error:      err.Error(),
			Logs:       logger.String(),
			LogEntries: pickRealtimeLogEntries(logger),
		})
		return
	}
	defer os.RemoveAll(tempDir)

	if mode == screenshot.ModeLinks {
		result, err := screenshot.RunUploadWithLiveLogs(ctx, path, tempDir, variant, subtitleMode, count, logger.LogLine)
		if err != nil {
			transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{
				OK:         false,
				Error:      err.Error(),
				Logs:       pickRealtimeLogs(logger, result.Logs),
				LogEntries: pickRealtimeLogEntries(logger),
			})
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{
			OK:              true,
			Output:          result.Output,
			Logs:            pickRealtimeLogs(logger, result.Logs),
			LogEntries:      pickRealtimeLogEntries(logger),
			PNGLossyFiles:   result.LossyPNGFiles,
			PNGLossyIndexes: result.LossyPNGIndexes,
		})
		return
	}

	if shouldPrepareDownload(r) {
		downloadURL, logs, err := prepareScreenshotZipDownload(ctx, path, tempDir, variant, subtitleMode, count, logger.LogLine)
		if err != nil {
			transport.WriteJSON(w, http.StatusInternalServerError, transport.InfoResponse{
				OK:         false,
				Error:      err.Error(),
				Logs:       pickRealtimeLogs(logger, logs),
				LogEntries: pickRealtimeLogEntries(logger),
			})
			return
		}
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{
			OK:         true,
			Output:     downloadURL,
			Logs:       pickRealtimeLogs(logger, logs),
			LogEntries: pickRealtimeLogEntries(logger),
		})
		return
	}

	if err := writeScreenshotZipResponse(ctx, w, path, tempDir, variant, subtitleMode, count); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}

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

// shouldPrepareDownload 会判断Prepare下载是否应该走当前分支。
func shouldPrepareDownload(r *http.Request) bool {
	return strings.TrimSpace(r.FormValue("prepare_download")) == "1"
}

// prepareScreenshotZipDownload 生成截图压缩包并保存到临时下载缓存，返回可复用的下载地址。
func prepareScreenshotZipDownload(ctx context.Context, path, tempDir, variant, subtitleMode string, count int, onLog screenshot.LogHandler) (string, string, error) {
	zipBytes, logs, err := generateScreenshotZip(ctx, path, tempDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return "", logs, err
	}

	screenshot.EmitProgressLog(onLog, "整理", 4, 4, "正在写入下载缓存。")
	token, err := screenshot.SavePreparedDownload(zipBytes)
	if err != nil {
		return "", logs, err
	}
	return "/api/screenshots?token=" + token, logs, nil
}

// writeScreenshotZipResponse 生成截图压缩包并直接以附件形式写回响应。
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

// generateScreenshotZip 运行截图流程并将输出文件打包成 ZIP 数据。
func generateScreenshotZip(ctx context.Context, path, tempDir, variant, subtitleMode string, count int, onLog screenshot.LogHandler) ([]byte, string, error) {
	result, err := screenshot.RunScreenshotsWithLiveLogs(ctx, path, tempDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return nil, result.Logs, err
	}

	screenshot.EmitProgressLog(onLog, "整理", 2, 4, "正在压缩截图文件。")
	zipBytes, err := screenshot.ZipFiles(result.Files)
	if err != nil {
		return nil, result.Logs, err
	}
	screenshot.EmitProgressLog(onLog, "整理", 3, 4, "截图压缩包已生成。")
	return zipBytes, result.Logs, nil
}

// pickRealtimeLogs 优先返回实时日志会话里已经收集的内容；为空时回退到 fallback。
func pickRealtimeLogs(logger *infoLogger, fallback string) string {
	if logger == nil {
		return fallback
	}
	if logs := logger.String(); strings.TrimSpace(logs) != "" {
		return logs
	}
	return fallback
}

// pickRealtimeLogEntries 优先返回实时日志会话里已经收集的结构化日志；为空时返回 nil。
func pickRealtimeLogEntries(logger *infoLogger) []transport.LogEntry {
	if logger == nil {
		return nil
	}
	return logger.Entries()
}

// servePreparedScreenshotDownload 根据令牌读取已准备好的 ZIP 文件并交给 HTTP 层输出。
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

// inputPathFromQuery 从查询参数解析输入路径，并复用媒体路径解析逻辑处理 ISO 虚拟路径。
func inputPathFromQuery(r *http.Request) (string, func(), error) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	path = strings.Trim(path, "\"")
	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()
	return media.ResolveInputPath(ctx, path)
}
