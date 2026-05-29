// Package screenshot 提供截图服务的参数规范化辅助函数。

package screenshot

import (
	"strconv"
	"strings"
)

// NormalizeMode 规范化截图接口的 mode；未知值会回落为 zip。
func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeLinks:
		return ModeLinks
	default:
		return ModeZip
	}
}

// NormalizeVariant 规范化截图输出格式；未知值会回落为 png。
func NormalizeVariant(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case VariantJPG:
		return VariantJPG
	default:
		return VariantPNG
	}
}

// NormalizeSubtitleMode 规范化字幕模式；off 和 none 类输入会关闭字幕，其余值使用自动模式。
func NormalizeSubtitleMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SubtitleModeOff, "none", "nosub", "false", "0":
		return SubtitleModeOff
	default:
		return SubtitleModeAuto
	}
}

// NormalizeHDRProcessor 规范化 HDR / Dolby Vision 处理器选项；未知值会回落为 libplacebo。
func NormalizeHDRProcessor(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case HDRProcessorZscale:
		return HDRProcessorZscale
	default:
		return HDRProcessorLibplacebo
	}
}

// NormalizeCount 规范化截图数量，并限制在允许范围内。
func NormalizeCount(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return normalizeScreenshotCount(0)
	}

	count, err := strconv.Atoi(value)
	if err != nil {
		return normalizeScreenshotCount(0)
	}
	return normalizeScreenshotCount(count)
}

// normalizeScreenshotCount 规范化内部流程使用的截图数量。
func normalizeScreenshotCount(count int) int {
	switch {
	case count == 0:
		return defaultScreenshotCount
	case count < minScreenshotCount:
		return minScreenshotCount
	case count > maxScreenshotCount:
		return maxScreenshotCount
	default:
		return count
	}
}
