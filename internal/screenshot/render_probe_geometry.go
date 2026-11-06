package screenshot

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotsource "minfo/internal/screenshot/source"
	screenshottimestamps "minfo/internal/screenshot/timestamps"
	"minfo/internal/system"
)

// 智能校正允许 0.5% 的像素宽高比偏差，比较时不受输出尺寸取偶数影响。
const autoSARTolerance = 0.005

// buildDisplayAspectFilter 会构建显示参数显示比例过滤器，为后续流程准备好可直接使用的结果。
func buildDisplayAspectFilter() string {
	// DVD/VOB and other anamorphic sources often use non-square pixels.
	// Still-image formats do not reliably preserve SAR, so we expand to
	// square pixels before writing PNG/JPG.
	return "scale='trunc(iw*sar/2)*2:ih',setsar=1"
}

// detectDisplayGeometry 读取视频流的 SAR/DAR 元数据，并构建更适合静态截图的比例修正链和输出尺寸。
func (r *screenshotRunner) detectDisplayGeometry() (string, int, int) {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,sample_aspect_ratio,display_aspect_ratio",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		r.logf("[提示] 显示比例探测失败，将按所选像素宽高比 (SAR) 模式处理。")
		return r.resolveDisplayGeometry(r.media.VideoWidth, r.media.VideoHeight, "", "")
	}

	width := 0
	height := 0
	sar := ""
	dar := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "width="):
			width, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "width=")))
		case strings.HasPrefix(line, "height="):
			height, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "height=")))
		case strings.HasPrefix(line, "sample_aspect_ratio="):
			sar = strings.TrimSpace(strings.TrimPrefix(line, "sample_aspect_ratio="))
		case strings.HasPrefix(line, "display_aspect_ratio="):
			dar = strings.TrimSpace(strings.TrimPrefix(line, "display_aspect_ratio="))
		}
	}
	if width <= 0 {
		width = r.media.VideoWidth
	}
	if height <= 0 {
		height = r.media.VideoHeight
	}

	if screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		mediainfoDAR := ""
		if r.subtitleState.HasDVDMediaInfoResult {
			mediainfoDAR = strings.TrimSpace(r.subtitleState.DVDMediaInfoResult.DisplayAspectRatio)
		}
		r.logf("[信息] DVD 比例探测：ffprobe width=%d height=%d sar=%s dar=%s | mediainfo dar=%s",
			width,
			height,
			screenshottimestamps.DisplayProbeValue(sar),
			screenshottimestamps.DisplayProbeValue(dar),
			screenshottimestamps.DisplayProbeValue(mediainfoDAR),
		)
		if width2, height2, filter, ok := r.detectDVDDisplayAspectFilterFromMediaInfo(width, height); ok {
			r.logf("[信息] DVD 显示比例元数据采用 mediainfo DAR：%s", filter)
			return r.resolveDisplayGeometry(width2, height2, "", filter)
		}
		r.logf("[提示] mediainfo 未提供可用 DVD 比例，回退 ffprobe SAR/DAR。")
	}

	return r.resolveDisplayGeometry(width, height, sar, dar)
}

// resolveDisplayGeometry 将所选模式应用到视频和字幕共用的目标尺寸，并记录决策。
func (r *screenshotRunner) resolveDisplayGeometry(width, height int, sar, dar string) (string, int, int) {
	mode := NormalizeSizeMode(r.sizeMode)
	filter, outputWidth, outputHeight := screenshotGeometryForMetadata(width, height, sar, dar, mode)
	modeLabel := "智能校正"
	if mode == SizeModeOriginal {
		modeLabel = "原始像素"
	}
	reason := "保持原始尺寸，不进行宽高比缩放"
	if strings.Contains(filter, "scale=") {
		reason = "按显示比例缩放并将宽度向下取偶数"
	} else if mode == SizeModeAuto {
		reason = fmt.Sprintf("像素宽高比偏差不超过 %.1f%% 或比例未知，保留原始尺寸", autoSARTolerance*100)
	}
	r.logf("[信息] 像素宽高比 (SAR)：%s | %dx%d -> %dx%d | SAR=%s DAR=%s | %s",
		modeLabel, width, height, outputWidth, outputHeight,
		screenshottimestamps.DisplayProbeValue(sar), screenshottimestamps.DisplayProbeValue(dar), reason,
	)
	return filter, outputWidth, outputHeight
}

// screenshotGeometryForMetadata 为两种 SAR 处理模式同时确定滤镜和字幕目标尺寸。
func screenshotGeometryForMetadata(width, height int, sar, dar, mode string) (string, int, int) {
	mode = NormalizeSizeMode(mode)
	if mode == SizeModeOriginal || (mode == SizeModeAuto && isNearSquarePixelGeometry(width, height, sar, dar)) {
		return "setsar=1", width, height
	}
	outputWidth, outputHeight := detectDisplayDimensionsForMetadata(width, height, sar, dar)
	return buildDisplayAspectFilterForMetadata(width, height, sar, dar), outputWidth, outputHeight
}

func isNearSquarePixelGeometry(width, height int, sar, dar string) bool {
	// 保留现有 DAR 优先规则，兼容 DVD 的 MediaInfo 显示比例修正。
	_, _, hasSAR := parseAspectRatio(sar)
	if width > 0 && height > 0 {
		rawAspect := float64(width) / float64(height)
		if displayAspect, ok := parseAspectRatioValue(screenshotdvdinfo.NormalizeAspectRatio(dar)); ok && (!hasSAR || math.Abs(displayAspect-rawAspect) > 0.02) {
			return math.Abs(displayAspect/rawAspect-1) <= autoSARTolerance+1e-12
		}
	}
	if num, den, ok := parseAspectRatio(sar); ok {
		return math.Abs(float64(num)/float64(den)-1) <= autoSARTolerance+1e-12
	}
	return true
}

// detectDVDDisplayAspectFilterFromMediaInfo 会从 mediainfo 结果中提取 DVD 比例修正所需参数。
func (r *screenshotRunner) detectDVDDisplayAspectFilterFromMediaInfo(width, height int) (int, int, string, bool) {
	if r == nil || !r.subtitleState.HasDVDMediaInfoResult {
		return 0, 0, "", false
	}
	if !screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		return 0, 0, "", false
	}
	if width <= 0 || height <= 0 {
		return 0, 0, "", false
	}
	if strings.TrimSpace(r.subtitleState.DVDMediaInfoResult.DisplayAspectRatio) == "" {
		return 0, 0, "", false
	}
	return width, height, r.subtitleState.DVDMediaInfoResult.DisplayAspectRatio, true
}

// buildDisplayAspectFilterForMetadata 根据视频宽高和宽高比元数据生成静态截图所需的比例修正链。
func buildDisplayAspectFilterForMetadata(width, height int, sar, dar string) string {
	_, _, hasSAR := parseAspectRatio(sar)
	if width > 0 && height > 0 {
		normalizedDAR := screenshotdvdinfo.NormalizeAspectRatio(dar)
		if darNum, darDen, ok := parseAspectRatio(normalizedDAR); ok {
			rawAspect := float64(width) / float64(height)
			displayAspect := float64(darNum) / float64(darDen)
			if !hasSAR || math.Abs(displayAspect-rawAspect) > 0.02 {
				return fmt.Sprintf("scale='trunc(ih*%d/%d/2)*2:ih',setsar=1", darNum, darDen)
			}
		} else if displayAspect, ok := parseAspectRatioValue(normalizedDAR); ok {
			rawAspect := float64(width) / float64(height)
			if !hasSAR || math.Abs(displayAspect-rawAspect) > 0.02 {
				return fmt.Sprintf("scale='trunc(ih*%.6f/2)*2:ih',setsar=1", displayAspect)
			}
		}
	}

	if sarNum, sarDen, ok := parseAspectRatio(sar); ok {
		if sarNum == sarDen {
			return "setsar=1"
		}
	}
	return buildDisplayAspectFilter()
}

// detectDisplayDimensionsForMetadata 根据视频宽高和比例元数据估算最终截图尺寸。
func detectDisplayDimensionsForMetadata(width, height int, sar, dar string) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	_, _, hasSAR := parseAspectRatio(sar)

	normalizedDAR := screenshotdvdinfo.NormalizeAspectRatio(dar)
	if darNum, darDen, ok := parseAspectRatio(normalizedDAR); ok {
		rawAspect := float64(width) / float64(height)
		displayAspect := float64(darNum) / float64(darDen)
		if !hasSAR || math.Abs(displayAspect-rawAspect) > 0.02 {
			return evenFloorDimension(float64(height) * displayAspect), height
		}
	} else if displayAspect, ok := parseAspectRatioValue(normalizedDAR); ok {
		rawAspect := float64(width) / float64(height)
		if !hasSAR || math.Abs(displayAspect-rawAspect) > 0.02 {
			return evenFloorDimension(float64(height) * displayAspect), height
		}
	}

	if sarNum, sarDen, ok := parseAspectRatio(sar); ok {
		if sarNum == sarDen {
			return width, height
		}
		return evenFloorDimension(float64(width) * float64(sarNum) / float64(sarDen)), height
	}

	return width, height
}

// evenFloorDimension 会把尺寸向下规整为正偶数值。
func evenFloorDimension(value float64) int {
	if value <= 0 {
		return 0
	}
	size := int(math.Floor(value))
	if size <= 0 {
		return 0
	}
	if size%2 != 0 {
		size--
	}
	if size <= 0 {
		size = 2
	}
	return size
}

// parseAspectRatio 会把类似 16:9 的宽高比文本解析为整数分子分母。
func parseAspectRatio(raw string) (int, int, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || value == "N/A" || value == "0:1" {
		return 0, 0, false
	}

	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}

	num, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	den, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || num <= 0 || den <= 0 {
		return 0, 0, false
	}
	return num, den, true
}

// parseAspectRatioValue 会把宽高比文本解析成浮点值，并兼容 16:9 与 1.778 两种写法。
func parseAspectRatioValue(raw string) (float64, bool) {
	if num, den, ok := parseAspectRatio(raw); ok {
		return float64(num) / float64(den), true
	}

	value := strings.TrimSpace(strings.ReplaceAll(raw, ",", "."))
	if value == "" || value == "N/A" {
		return 0, false
	}

	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || ratio <= 0 {
		return 0, false
	}
	return ratio, true
}
