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
	screenshottimestamps "minfo/internal/screenshot/timestamps"
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
	Timestamps   []string
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
	timestamps, err := normalizeScreenshotFormTimestamps(r)
	if err != nil {
		cleanup()
		return screenshotRequest{}, err
	}
	if len(timestamps) > 0 {
		options.Count = len(timestamps)
	}

	return screenshotRequest{
		Mode:         screenshot.NormalizeMode(r.FormValue("mode")),
		InputPath:    inputPath,
		Cleanup:      cleanup,
		Variant:      options.Variant,
		SubtitleMode: options.SubtitleMode,
		HDRProcessor: options.HDRProcessor,
		Count:        options.Count,
		ProxyURL:     proxyURL,
		Timestamps:   timestamps,
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

// normalizeScreenshotFormTimestamps 会提取可选的指定截图时间点。
func normalizeScreenshotFormTimestamps(r *http.Request) ([]string, error) {
	values := make([]string, 0)
	if r != nil && r.Form != nil {
		values = append(values, r.Form["timestamp"]...)
		for _, value := range r.Form["timestamps"] {
			values = append(values, splitScreenshotTimestampList(value)...)
		}
	}

	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil, nil
	}
	if len(result) > 10 {
		return nil, fmt.Errorf("截图时间点数量不能超过 10 个")
	}
	if _, err := screenshottimestamps.ParseRequestedTimestamps(result); err != nil {
		return nil, fmt.Errorf("截图时间点无效: %w", err)
	}
	return result, nil
}

func splitScreenshotTimestampList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t'
	})
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
