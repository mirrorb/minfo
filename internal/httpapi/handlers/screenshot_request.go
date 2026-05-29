// Package handlers 提供截图请求的参数解析与运行选项规范化辅助函数。

package handlers

import (
	"net/http"
	"os"

	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

// screenshotRequest 表示一次截图表单请求解析后的完整运行参数。
type screenshotRequest struct {
	Mode         string
	InputPath    string
	Cleanup      func()
	Variant      string
	SubtitleMode string
	HDRProcessor string
	Count        int
}

// screenshotRunOptions 表示截图流程真正执行时需要的规格化选项。
type screenshotRunOptions struct {
	Variant      string
	SubtitleMode string
	HDRProcessor string
	Count        int
}

// parseScreenshotFormRequest 会把 multipart/form-data 请求解析成统一的截图运行参数。
func parseScreenshotFormRequest(r *http.Request) (screenshotRequest, error) {
	inputPath, cleanup, err := transport.InputPath(r)
	if err != nil {
		return screenshotRequest{}, err
	}

	options := normalizeScreenshotFormOptions(r)
	return screenshotRequest{
		Mode:         screenshot.NormalizeMode(r.FormValue("mode")),
		InputPath:    inputPath,
		Cleanup:      cleanup,
		Variant:      options.Variant,
		SubtitleMode: options.SubtitleMode,
		HDRProcessor: options.HDRProcessor,
		Count:        options.Count,
	}, nil
}

// normalizeScreenshotFormOptions 会从表单请求中提取并规范化截图运行选项。
func normalizeScreenshotFormOptions(r *http.Request) screenshotRunOptions {
	return screenshotRunOptions{
		Variant:      screenshot.NormalizeVariant(r.FormValue("variant")),
		SubtitleMode: screenshot.NormalizeSubtitleMode(r.FormValue("subtitle_mode")),
		HDRProcessor: screenshot.NormalizeHDRProcessor(r.FormValue("hdr_processor")),
		Count:        screenshot.NormalizeCount(r.FormValue("count")),
	}
}

// normalizeScreenshotQueryOptions 会从查询参数中提取并规范化截图运行选项。
func normalizeScreenshotQueryOptions(r *http.Request) screenshotRunOptions {
	return screenshotRunOptions{
		Variant:      screenshot.NormalizeVariant(r.URL.Query().Get("variant")),
		SubtitleMode: screenshot.NormalizeSubtitleMode(r.URL.Query().Get("subtitle_mode")),
		HDRProcessor: screenshot.NormalizeHDRProcessor(r.URL.Query().Get("hdr_processor")),
		Count:        screenshot.NormalizeCount(r.URL.Query().Get("count")),
	}
}

// createScreenshotTempDir 会为一次截图任务创建独立临时目录。
func createScreenshotTempDir(pattern string) (string, error) {
	return os.MkdirTemp("", pattern)
}
