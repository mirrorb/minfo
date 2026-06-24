// Package screenshot 对外暴露截图与上传服务入口。

package screenshot

import (
	"context"
	"strings"

	screenshotpixhost "minfo/internal/screenshot/pixhost"
)

const oversizeBytes = 10 * 1024 * 1024

// RunScreenshots 执行截图流程并仅返回生成的文件列表。
func RunScreenshots(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int) ([]string, error) {
	result, err := RunScreenshotsWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count)
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

// RunScreenshotsWithLogs 执行截图流程并返回文件列表与完整日志。
func RunScreenshotsWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int) (ScreenshotsResult, error) {
	return RunScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, nil)
}

// RunScreenshotsWithLiveLogs 会执行截图流程，并把实时日志通过回调逐行暴露给调用方。
func RunScreenshotsWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int, onLog LogHandler) (ScreenshotsResult, error) {
	return runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, onLog)
}

// RunScreenshotsAtTimestampsWithLiveLogs 会按指定时间点执行截图流程。
func RunScreenshotsAtTimestampsWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, timestamps []string, onLog LogHandler) (ScreenshotsResult, error) {
	return runEngineScreenshotsAtTimestampsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, timestamps, onLog)
}

// RunUpload 执行截图加上传流程并仅返回直链输出。
func RunUpload(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int) (string, error) {
	result, err := RunUploadWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// RunUploadWithLogs 执行截图加上传流程并返回直链输出与完整日志。
func RunUploadWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int) (UploadResult, error) {
	return RunUploadWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, nil)
}

// RunUploadWithLiveLogs 会执行截图加上传流程，并把实时日志通过回调逐行暴露给调用方。
func RunUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int, onLog LogHandler) (UploadResult, error) {
	return RunUploadWithLiveEvents(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, onLog, nil)
}

// RunUploadWithLiveLogsWithOptions 会按指定上传选项执行截图加上传流程。
func RunUploadWithLiveLogsWithOptions(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int, options UploadOptions, onLog LogHandler) (UploadResult, error) {
	return RunUploadWithLiveEventsWithOptions(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, options, onLog, nil)
}

// RunUploadWithLiveEvents 会执行截图加上传流程，并把实时日志和已完成图片逐步暴露给调用方。
func RunUploadWithLiveEvents(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	return RunUploadWithLiveEventsWithOptions(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, UploadOptions{}, onLog, onItem)
}

// RunUploadWithLiveEventsWithOptions 会按指定上传选项执行截图加上传流程，并逐步暴露实时事件。
func RunUploadWithLiveEventsWithOptions(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, count int, options UploadOptions, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	screenshotResult, err := runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, count, onLog)
	if err != nil {
		return UploadResult{Logs: screenshotResult.Logs}, err
	}

	return uploadScreenshotResult(ctx, screenshotResult, options, onLog, onItem)
}

// RunUploadAtTimestampsWithLiveEventsWithOptions 会按指定时间点截图并上传。
func RunUploadAtTimestampsWithLiveEventsWithOptions(ctx context.Context, inputPath, outputDir, variant, subtitleMode, hdrProcessor string, timestamps []string, options UploadOptions, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	screenshotResult, err := runEngineScreenshotsAtTimestampsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, hdrProcessor, timestamps, onLog)
	if err != nil {
		return UploadResult{Logs: screenshotResult.Logs}, err
	}

	return uploadScreenshotResult(ctx, screenshotResult, options, onLog, onItem)
}

// uploadScreenshotResult 会上传截图结果，并合并截图与上传阶段日志。
func uploadScreenshotResult(ctx context.Context, screenshotResult ScreenshotsResult, options UploadOptions, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	uploadResult, err := screenshotpixhost.UploadImagesWithOptions(ctx, screenshotResult.Files, screenshotResult.LossyPNGFiles, oversizeBytes, options, onLog, onItem)
	logs := mergeUploadLogs(screenshotResult.Logs, uploadResult.Logs)
	if err != nil {
		return UploadResult{
			Logs:            logs,
			Items:           uploadResult.Items,
			LossyPNGFiles:   screenshotResult.LossyPNGFiles,
			LossyPNGIndexes: uploadResult.LossyIndexes,
		}, err
	}
	return UploadResult{
		Output:          uploadResult.Output,
		Logs:            logs,
		Items:           uploadResult.Items,
		LossyPNGFiles:   screenshotResult.LossyPNGFiles,
		LossyPNGIndexes: uploadResult.LossyIndexes,
	}, nil
}

// mergeUploadLogs 会把截图阶段和上传阶段日志按原顺序拼接成最终输出。
func mergeUploadLogs(screenshotLogs, uploadLogs string) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(screenshotLogs) != "" {
		parts = append(parts, strings.TrimSpace(screenshotLogs))
	}
	if strings.TrimSpace(uploadLogs) != "" {
		parts = append(parts, strings.TrimSpace(uploadLogs))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}
