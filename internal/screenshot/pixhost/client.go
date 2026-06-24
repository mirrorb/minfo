// Package pixhost 负责 Pixhost API 请求和直链归一化细节。

package pixhost

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
	"strings"

	"minfo/internal/config"
)

const apiURL = "https://api.pixhost.to/images"

// thumbHostPattern 用于识别 Pixhost 返回的缩略图域名，并改写为原图域名。
var thumbHostPattern = regexp.MustCompile(`^t([0-9]+)\.pixhost\.to$`)

// apiResponse 描述 Pixhost JSON 响应中当前流程实际使用的字段。
type apiResponse struct {
	ShowURL string `json:"show_url"`
	ThURL   string `json:"th_url"`
}

// endpoint 会返回本轮上传应使用的 Pixhost API 地址。
func endpoint() string {
	return config.Getenv("PIXHOST_API_URL", apiURL)
}

// newHTTPClient 构造 Pixhost 上传使用的 HTTP 客户端。
func newHTTPClient(options UploadOptions) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL := strings.TrimSpace(options.ProxyURL)
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport}, nil
}

// uploadSingleImage 上传单张图片到 Pixhost，并返回原图直链和 Pixhost 缩略图地址。
func uploadSingleImage(ctx context.Context, client *http.Client, apiURL, imagePath string) (string, string, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("img", filepath.Base(imagePath))
	if err != nil {
		return "", "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", "", err
	}
	if err := writer.WriteField("content_type", "0"); err != nil {
		return "", "", err
	}
	if err := writer.WriteField("max_th_size", "420"); err != nil {
		return "", "", err
	}
	if err := writer.Close(); err != nil {
		return "", "", err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &body)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()

	payloadBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", fmt.Errorf("pixhost returned HTTP %d", response.StatusCode)
	}

	var payload apiResponse
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(payload.ShowURL) == "" || strings.TrimSpace(payload.ThURL) == "" {
		return "", "", errors.New("pixhost response is missing show_url or th_url")
	}
	thumbnailURL, err := normalizeHTTPURL(payload.ThURL)
	if err != nil {
		return "", "", err
	}
	directURL, err := normalizeDirectURL(payload.ThURL)
	if err != nil {
		return "", "", err
	}
	return directURL, thumbnailURL, nil
}

// normalizeHTTPURL 会校验外部返回的 URL 是否是可直接用于前端展示的 HTTP 或 HTTPS 地址。
func normalizeHTTPURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("pixhost URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	result := parsed.String()
	if !strings.HasPrefix(result, "http://") && !strings.HasPrefix(result, "https://") {
		return "", errors.New("pixhost URL is invalid")
	}
	return result, nil
}

// normalizeDirectURL 会把 Pixhost 缩略图地址改写成直链，并校验结果仍然是有效的 HTTP 或 HTTPS URL。
func normalizeDirectURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("pixhost direct URL is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}

	parsed.Path = strings.Replace(parsed.Path, "/thumbs/", "/images/", 1)
	if matches := thumbHostPattern.FindStringSubmatch(strings.ToLower(parsed.Host)); len(matches) == 2 {
		parsed.Host = "img" + matches[1] + ".pixhost.to"
	}

	result := parsed.String()
	if !strings.HasPrefix(result, "http://") && !strings.HasPrefix(result, "https://") {
		return "", errors.New("pixhost direct URL is invalid")
	}
	return result, nil
}
