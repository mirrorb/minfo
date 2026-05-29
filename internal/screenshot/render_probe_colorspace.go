package screenshot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"minfo/internal/system"
)

// detectColorspace 读取视频流的色彩空间元数据，并整理成稳定键值。
func (r *screenshotRunner) detectColorspace() string {
	stdout, _, err := system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_space,color_primaries,color_transfer:stream_side_data=side_data_type,dv_profile",
		"-of", "json",
		r.sourcePath,
	)
	if err == nil {
		if info := parseColorspaceProbeJSON(stdout); info != "" {
			return info
		}
	}

	stdout, _, err = system.RunCommand(r.ctx, r.tools.FFprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_space,color_primaries,color_transfer",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return ""
	}

	lines := make([]string, 0, 3)
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "color_space=") || strings.HasPrefix(line, "color_primaries=") || strings.HasPrefix(line, "color_transfer=") {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "|") + "|"
}

// buildColorspaceChain 返回 ffmpeg 使用的色彩空间转换过滤器链。
func buildColorspaceChain(info, hdrProcessor string) string {
	switch {
	case shouldUseLibplaceboColorspace(info, hdrProcessor):
		// Follow FFmpeg's documented CPU/llvmpipe example and keep the HDR/DV
		// libplacebo path conservative by disabling the expensive peak detector.
		return buildLibplaceboColorspaceChain(info)
	case shouldUseAdvancedColorspaceChain(info):
		return "format=yuv420p10le,zscale=t=linear:npl=203,format=gbrpf32le,tonemap=mobius:param=0.3:desat=2.0,zscale=p=bt709:t=bt709:m=bt709,format=rgb24"
	case strings.Contains(info, "bt2020"):
		return "zscale=p=bt709:t=bt709:m=bt709,format=rgb24"
	default:
		return ""
	}
}

// shouldUseAdvancedColorspaceChain 会判断当前色彩元数据是否需要走 HDR / WCG 特殊处理链。
func shouldUseAdvancedColorspaceChain(info string) bool {
	if strings.TrimSpace(info) == "" {
		return false
	}
	if strings.Contains(info, "dolby_vision=1") {
		return true
	}
	return strings.Contains(info, "bt2020") && (strings.Contains(info, "smpte2084") || strings.Contains(info, "arib-std-b67"))
}

// shouldUseLibplaceboColorspace 会判断当前色彩元数据是否应该走 libplacebo 链路。
func shouldUseLibplaceboColorspace(info, hdrProcessor string) bool {
	return shouldUseAdvancedColorspaceChain(info) && NormalizeHDRProcessor(hdrProcessor) == HDRProcessorLibplacebo
}

// buildLibplaceboColorspaceChain 会构建 HDR/DV 转换到 sRGB 输出的 libplacebo 过滤器链。
func buildLibplaceboColorspaceChain(info string) string {
	// FFmpeg documents RGB output colorspace as gbr (AVCOL_SPC_RGB / sRGB).
	options := []string{
		"upscaler=none",
		"downscaler=none",
		"colorspace=gbr",
		"color_primaries=bt709",
		"color_trc=iec61966-2-1",
		"range=pc",
		"format=rgb24",
	}
	if strings.Contains(info, "dolby_vision=1") {
		options = append(options, "apply_dolbyvision=true")
	}
	options = append(options, "peak_detect=false")
	return "libplacebo=" + strings.Join(options, ":")
}

// parseColorspaceProbeJSON 会解析 ffprobe JSON 输出中的色彩空间和杜比视界元数据。
func parseColorspaceProbeJSON(stdout string) string {
	type ffprobeColorSideData struct {
		SideDataType string `json:"side_data_type"`
		DVProfile    int    `json:"dv_profile"`
	}
	type ffprobeColorStream struct {
		ColorSpace     string                 `json:"color_space"`
		ColorPrimaries string                 `json:"color_primaries"`
		ColorTransfer  string                 `json:"color_transfer"`
		SideDataList   []ffprobeColorSideData `json:"side_data_list"`
	}
	var payload struct {
		Streams []ffprobeColorStream `json:"streams"`
	}

	if strings.TrimSpace(stdout) == "" {
		return ""
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return ""
	}
	if len(payload.Streams) == 0 {
		return ""
	}

	stream := payload.Streams[0]
	lines := make([]string, 0, 5)
	if strings.TrimSpace(stream.ColorSpace) != "" {
		lines = append(lines, "color_space="+strings.TrimSpace(stream.ColorSpace))
	}
	if strings.TrimSpace(stream.ColorPrimaries) != "" {
		lines = append(lines, "color_primaries="+strings.TrimSpace(stream.ColorPrimaries))
	}
	if strings.TrimSpace(stream.ColorTransfer) != "" {
		lines = append(lines, "color_transfer="+strings.TrimSpace(stream.ColorTransfer))
	}
	for _, sideData := range stream.SideDataList {
		lowerType := strings.ToLower(strings.TrimSpace(sideData.SideDataType))
		if lowerType == "" {
			continue
		}
		if strings.Contains(lowerType, "dovi") || strings.Contains(lowerType, "dolby vision") {
			lines = append(lines, "dolby_vision=1")
			if sideData.DVProfile > 0 {
				lines = append(lines, fmt.Sprintf("dv_profile=%d", sideData.DVProfile))
			}
			break
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "|") + "|"
}
