package handlers

import (
	"context"

	"minfo/internal/screenshot"
	screenshotdelivery "minfo/internal/screenshot/delivery"
	screenshotprogress "minfo/internal/screenshot/progress"
)

// generateScreenshotZip 运行截图流程并将输出文件打包成 ZIP 数据。
func generateScreenshotZip(ctx context.Context, path, tempDir, variant, subtitleMode, hdrProcessor string, count int, onLog screenshot.LogHandler) ([]byte, string, error) {
	result, err := screenshot.RunScreenshotsWithLiveLogs(ctx, path, tempDir, variant, subtitleMode, hdrProcessor, count, onLog)
	if err != nil {
		return nil, result.Logs, err
	}

	screenshotprogress.EmitStepLog(onLog, "整理", 2, 4, "正在压缩截图文件。")
	zipBytes, err := screenshotdelivery.ZipFiles(result.Files)
	if err != nil {
		return nil, result.Logs, err
	}
	screenshotprogress.EmitStepLog(onLog, "整理", 3, 4, "截图压缩包已生成。")
	return zipBytes, result.Logs, nil
}
