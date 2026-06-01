// Package handlers 提供截图请求的参数解析与运行选项规范化辅助函数。

package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

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
	ProxyURL     string
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

	proxyURL, err := normalizeProxyURL(r.FormValue("proxy_url"))
	if err != nil {
		cleanup()
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
		ProxyURL:     proxyURL,
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

// normalizeProxyURL 校验前端传入的图床代理地址；空值表示使用服务端默认代理环境。
func normalizeProxyURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("代理地址无效: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("代理地址无效")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5":
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("代理地址协议不支持: %s", parsed.Scheme)
	}
}
