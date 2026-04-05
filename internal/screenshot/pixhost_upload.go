// Package screenshot 负责 Pixhost 上传和结果整理。

package screenshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"minfo/internal/config"
)

const pixhostAPIURL = "https://api.pixhost.to/images"

var pixhostThumbHostPattern = regexp.MustCompile(`^t([0-9]+)\.pixhost\.to$`)

type pixhostResponse struct {
	ShowURL string `json:"show_url"`
	ThURL   string `json:"th_url"`
}

// runPixhostUploadWithLiveLogs 会先生成截图，再把图片上传到 Pixhost，并合并两阶段日志。
func runPixhostUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (UploadResult, error) {
	screenshotResult, err := runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
	if err != nil {
		return UploadResult{Logs: screenshotResult.Logs}, err
	}

	output, uploadLogs, err := uploadImagesToPixhost(ctx, screenshotResult.Files, onLog)
	logs := strings.TrimSpace(strings.Join(filterNonEmptyStrings(screenshotResult.Logs, uploadLogs), "\n\n"))
	if err != nil {
		return UploadResult{Logs: logs}, err
	}
	return UploadResult{Output: output, Logs: logs}, nil
}

// uploadImagesToPixhost 过滤可上传图片，逐个上传到 Pixhost，并返回整理后的直链文本和日志。
func uploadImagesToPixhost(ctx context.Context, files []string, onLog LogHandler) (string, string, error) {
	logLines := make([]string, 0)
	images := collectUploadableImages(files)
	if len(images) == 0 {
		logLines = appendUploadLogLine(logLines, onLog, "警告: 未找到有效图片文件")
		return "", strings.Join(logLines, "\n"), errors.New("no uploadable screenshots were found")
	}

	logLines = appendUploadLogLine(logLines, onLog, "开始处理 %d 个文件...", len(images))
	client := &http.Client{}
	apiURL := config.Getenv("PIXHOST_API_URL", pixhostAPIURL)

	links := make([]string, 0, len(images))
	successCount := 0
	for _, imagePath := range images {
		directURL, err := uploadSinglePixhostImage(ctx, client, apiURL, imagePath)
		if err != nil {
			logLines = appendUploadLogLine(logLines, onLog, "上传失败: %s (%s)", filepath.Base(imagePath), err.Error())
			continue
		}
		successCount++
		links = append(links, directURL)
		logLines = appendUploadLogLine(logLines, onLog, "已上传并校准域名: %s", filepath.Base(imagePath))
	}

	logLines = appendUploadLogLine(logLines, onLog, "")
	logLines = appendUploadLogLine(logLines, onLog, "处理完成! 成功: %d/%d", successCount, len(images))

	if len(links) == 0 {
		return "", strings.Join(logLines, "\n"), errors.New("pixhost upload completed but returned no links")
	}
	return strings.Join(extractDirectLinks(strings.Join(links, "\n")), "\n"), strings.Join(logLines, "\n"), nil
}

// appendUploadLogLine 追加一条上传日志；若提供实时回调则同步推送。
func appendUploadLogLine(logLines []string, onLog LogHandler, format string, args ...any) []string {
	line := fmt.Sprintf(format, args...)
	logLines = append(logLines, line)
	if onLog != nil {
		onLog(line)
	}
	return logLines
}

// collectUploadableImages 从路径列表中筛出可上传的图片，并按文件名排序。
func collectUploadableImages(paths []string) []string {
	candidates := make([]string, 0, len(paths))
	for _, path := range paths {
		if !isUploadableImage(path) {
			continue
		}
		candidates = append(candidates, path)
	}
	sort.Strings(candidates)
	return candidates
}

// isUploadableImage 检查文件是否存在、尺寸合理且 MIME 类型为图片。
func isUploadableImage(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if info.Size() <= 0 || info.Size() > oversizeBytes {
		return false
	}

	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}

	contentType := http.DetectContentType(header[:n])
	return strings.HasPrefix(contentType, "image/")
}

// uploadSinglePixhostImage 上传单张图片到 Pixhost，并把返回的缩略图地址转换成直链。
func uploadSinglePixhostImage(ctx context.Context, client *http.Client, apiURL, imagePath string) (string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("img", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.WriteField("content_type", "0"); err != nil {
		return "", err
	}
	if err := writer.WriteField("max_th_size", "420"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	payloadBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("pixhost returned HTTP %d", response.StatusCode)
	}

	var payload pixhostResponse
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.ShowURL) == "" || strings.TrimSpace(payload.ThURL) == "" {
		return "", errors.New("pixhost response is missing show_url or th_url")
	}

	return normalizePixhostDirectURL(payload.ThURL)
}

// normalizePixhostDirectURL 会把 Pixhost 缩略图地址改写成直链，并校验结果仍然是有效的 HTTP 或 HTTPS URL。
func normalizePixhostDirectURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("pixhost direct URL is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}

	parsed.Path = strings.Replace(parsed.Path, "/thumbs/", "/images/", 1)
	if matches := pixhostThumbHostPattern.FindStringSubmatch(strings.ToLower(parsed.Host)); len(matches) == 2 {
		parsed.Host = "img" + matches[1] + ".pixhost.to"
	}

	result := parsed.String()
	if !strings.HasPrefix(result, "http://") && !strings.HasPrefix(result, "https://") {
		return "", errors.New("pixhost direct URL is invalid")
	}
	return result, nil
}
