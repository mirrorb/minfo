package handlers

import (
	"context"
	"net/http"
	"os"

	"minfo/internal/config"
	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

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

	request, err := parseScreenshotFormRequest(r)
	if err != nil {
		transport.WriteJSON(w, http.StatusBadRequest, transport.InfoResponse{
			OK:         false,
			Error:      err.Error(),
			Logs:       logger.String(),
			LogEntries: pickRealtimeLogEntries(logger),
		})
		return
	}
	defer request.Cleanup()

	ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
	defer cancel()

	tempDir, err := createScreenshotTempDir("minfo-shots-*")
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

	if request.Mode == screenshot.ModeLinks {
		result, err := screenshot.RunUploadWithLiveLogsWithOptions(
			ctx,
			request.InputPath,
			tempDir,
			request.Variant,
			request.SubtitleMode,
			request.HDRProcessor,
			request.Count,
			screenshot.UploadOptions{ProxyURL: request.ProxyURL},
			logger.LogLine,
		)
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
			LinkItems:       buildTransportImageLinkItems(result.Items),
			PNGLossyFiles:   result.LossyPNGFiles,
			PNGLossyIndexes: result.LossyPNGIndexes,
		})
		return
	}

	if shouldPrepareDownload(r) {
		downloadURL, logs, err := prepareScreenshotZipDownload(ctx, request.InputPath, tempDir, request.Variant, request.SubtitleMode, request.HDRProcessor, request.Count, logger.LogLine)
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

	if err := writeScreenshotZipResponse(ctx, w, request.InputPath, tempDir, request.Variant, request.SubtitleMode, request.HDRProcessor, request.Count); err != nil {
		transport.WriteError(w, http.StatusInternalServerError, err.Error())
	}
}
