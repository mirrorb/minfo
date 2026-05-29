// Package handlers 提供截图后台任务的执行与状态流转逻辑。

package handlers

import (
	"context"
	"os"

	"minfo/internal/config"
	"minfo/internal/screenshot"
)

// run 会在后台执行具体截图任务，并在结束后更新任务状态和结果。
func (j *screenshotJob) run() {
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

	tempDir, err := createScreenshotTempDir("minfo-screenshot-job-*")
	if err != nil {
		j.fail(err)
		return
	}
	defer os.RemoveAll(tempDir)

	switch j.mode {
	case screenshot.ModeLinks:
		result, err := screenshot.RunUploadWithLiveEvents(
			ctx,
			j.inputPath,
			tempDir,
			j.variant,
			j.subtitleMode,
			j.hdrProcessor,
			j.count,
			j.logger.LogLine,
			func(item screenshot.UploadedImage) {
				j.appendLinkItem(buildTransportImageLinkItem(item))
			},
		)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed(result.Output, "", buildTransportImageLinkItems(result.Items), result.LossyPNGFiles, result.LossyPNGIndexes)
	default:
		downloadURL, _, err := prepareScreenshotZipDownload(ctx, j.inputPath, tempDir, j.variant, j.subtitleMode, j.hdrProcessor, j.count, j.logger.LogLine)
		if err != nil {
			j.fail(err)
			return
		}
		j.succeed("", downloadURL, nil, nil, nil)
	}
}
