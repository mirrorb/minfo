package handlers

import (
	"strings"
	"time"

	"minfo/internal/httpapi/transport"
	"minfo/internal/screenshot"
)

// appendLinkItem 会在后台任务运行中逐步追加已完成上传的图片链接。
func (j *screenshotJob) appendLinkItem(item transport.ImageLinkItem) {
	if strings.TrimSpace(item.URL) == "" {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	for _, existing := range j.linkItems {
		if existing.URL == item.URL {
			return
		}
	}

	j.linkItems = append(j.linkItems, item)
	j.updatedAt = time.Now()
}

// buildTransportImageLinkItems 会把截图上传结果批量转换为 HTTP 响应结构。
func buildTransportImageLinkItems(items []screenshot.UploadedImage) []transport.ImageLinkItem {
	if len(items) == 0 {
		return nil
	}

	result := make([]transport.ImageLinkItem, 0, len(items))
	for _, item := range items {
		if normalized := buildTransportImageLinkItem(item); strings.TrimSpace(normalized.URL) != "" {
			result = append(result, normalized)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// buildTransportImageLinkItem 会把单个截图上传结果转换为 HTTP 响应结构。
func buildTransportImageLinkItem(item screenshot.UploadedImage) transport.ImageLinkItem {
	return transport.ImageLinkItem{
		URL:          strings.TrimSpace(item.URL),
		ThumbnailURL: strings.TrimSpace(item.ThumbnailURL),
		Filename:     item.Filename,
		Size:         item.Size,
		Width:        item.Width,
		Height:       item.Height,
	}
}
