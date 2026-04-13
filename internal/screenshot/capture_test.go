// Package screenshot 验证截图过滤器拼接逻辑的关键回归场景。

package screenshot

import (
	"strings"
	"testing"
	"time"
)

// TestBuildTextSubtitleFilterForInternalTextSubtitle 验证内封文字字幕过滤器会保持与 shell 一致的 si 写法。
func TestBuildTextSubtitleFilterForInternalTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		sourcePath:  "/media/example/video.mkv",
		videoWidth:  1920,
		videoHeight: 1080,
		subtitle: subtitleSelection{
			Mode:          "internal",
			RelativeIndex: 1,
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, "subtitles='/media/example/video.mkv'") {
		t.Fatalf("expected shell-style subtitles path in filter, got %q", filter)
	}
	if !strings.Contains(filter, ":si=1") {
		t.Fatalf("expected shell-style si option in filter, got %q", filter)
	}
	if !strings.Contains(filter, ":original_size=1920x1080") {
		t.Fatalf("expected original_size in filter, got %q", filter)
	}
}

// TestBuildTextSubtitleFilterForExternalSubtitle 验证外挂文字字幕过滤器也会保持 shell 的位置参数写法。
func TestBuildTextSubtitleFilterForExternalSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		videoWidth:  1280,
		videoHeight: 720,
		subtitle: subtitleSelection{
			Mode: "external",
			File: "/media/example/subtitle.srt",
		},
	}

	filter := runner.buildTextSubtitleFilter()
	if !strings.Contains(filter, "subtitles='/media/example/subtitle.srt'") {
		t.Fatalf("expected shell-style subtitles path in filter, got %q", filter)
	}
	if strings.Contains(filter, ":si=") {
		t.Fatalf("did not expect si for external subtitle, got %q", filter)
	}
}

// TestShellStyleTextSubtitleChain 验证文字字幕过滤器链保持 shell 的 setpts 后接 subtitles 顺序。
func TestShellStyleTextSubtitleChain(t *testing.T) {
	filter := joinFilters(
		"setpts=PTS-STARTPTS,select='gte(t,1.000)'",
		"setpts=PTS-STARTPTS+61.000/TB",
		"subtitles='/media/example/video.mkv':original_size=3840x2160:si=1",
	)

	expected := "setpts=PTS-STARTPTS,select='gte(t,1.000)',setpts=PTS-STARTPTS+61.000/TB,subtitles='/media/example/video.mkv':original_size=3840x2160:si=1"
	if filter != expected {
		t.Fatalf("expected shell-style filter chain %q, got %q", expected, filter)
	}
}

func TestApproximateRenderProgressPercentFromSpeed(t *testing.T) {
	state := &ffmpegRealtimeState{
		startedAt:     time.Now().Add(-2 * time.Second),
		windowSeconds: 4,
		speed:         "2.0x",
	}

	percent, ok := approximateRenderProgressPercent(state)
	if !ok {
		t.Fatal("approximateRenderProgressPercent returned ok=false")
	}
	if percent < 90 || percent > 94 {
		t.Fatalf("percent = %.1f, want between 90 and 94", percent)
	}
}

func TestApproximateRenderProgressPercentFromLocalOutTime(t *testing.T) {
	state := &ffmpegRealtimeState{
		windowSeconds:   8,
		firstOutTimeMS:  2_000_000,
		outTimeMS:       5_000_000,
		hasFirstOutTime: true,
	}

	percent, ok := approximateRenderProgressPercent(state)
	if !ok {
		t.Fatal("approximateRenderProgressPercent returned ok=false")
	}
	if percent != 37.5 {
		t.Fatalf("percent = %.1f, want 37.5", percent)
	}
}

func TestApproximateRenderProgressPercentFromElapsedFallback(t *testing.T) {
	state := &ffmpegRealtimeState{
		startedAt:     time.Now().Add(-2 * time.Second),
		windowSeconds: 0,
	}

	percent, ok := approximateRenderProgressPercent(state)
	if !ok {
		t.Fatal("approximateRenderProgressPercent returned ok=false")
	}
	if percent < 50 || percent > 65 {
		t.Fatalf("percent = %.1f, want between 50 and 65", percent)
	}
}

func TestFFmpegSubtitleProgressPercentNormalizesFromFirstSubtitleTimestamp(t *testing.T) {
	runner := &screenshotRunner{
		duration: 7200,
	}
	state := &ffmpegRealtimeState{
		outTimeMS:       3_900_000_000,
		firstOutTimeMS:  3_600_000_000,
		hasFirstOutTime: true,
	}

	percent := runner.ffmpegProgressPercent("字幕", "continue", state)
	if percent < 8 || percent > 9 {
		t.Fatalf("percent = %.2f, want between 8 and 9 after normalizing from first subtitle timestamp", percent)
	}
}

func TestNormalizeRenderProgressWindow(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{input: 0, want: 0.5},
		{input: 0.2, want: 0.5},
		{input: 1.5, want: 1.5},
	}

	for _, tt := range tests {
		if got := normalizeRenderProgressWindow(tt.input); got != tt.want {
			t.Fatalf("normalizeRenderProgressWindow(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBuildColorspaceChainForHDR(t *testing.T) {
	info := "color_primaries=bt2020|color_space=bt2020nc|color_transfer=smpte2084|"
	chain := buildColorspaceChain(info)

	if !strings.Contains(chain, "zscale=t=linear:npl=203") {
		t.Fatalf("expected HDR colorspace chain to raise nominal peak luminance, got %q", chain)
	}
	if !strings.Contains(chain, "tonemap=mobius") {
		t.Fatalf("expected HDR colorspace chain to include tone mapping, got %q", chain)
	}
	if !strings.Contains(chain, "tonemap=mobius:param=0.3:desat=2.0,zscale=p=bt709:t=bt709:m=bt709") {
		t.Fatalf("expected HDR colorspace chain to tonemap before bt709 conversion, got %q", chain)
	}
	if strings.Contains(chain, "zscale=p=bt709:t=bt709,tonemap=mobius") {
		t.Fatalf("expected HDR colorspace chain to avoid bt709 conversion before tonemap, got %q", chain)
	}
}

func TestBuildColorspaceChainForBT2020SDR(t *testing.T) {
	info := "color_primaries=bt2020|color_space=bt2020nc|color_transfer=bt709|"
	chain := buildColorspaceChain(info)

	if !strings.Contains(chain, "zscale=p=bt709:t=bt709:m=bt709") {
		t.Fatalf("expected HDR colorspace chain to map to bt709, got %q", chain)
	}
	if strings.Contains(chain, "scale=in_color_matrix=bt2020:out_color_matrix=bt709") {
		t.Fatalf("expected BT.2020 SDR colorspace chain to avoid scale matrix-only conversion, got %q", chain)
	}
}

func TestBuildColorspaceChainForSDR(t *testing.T) {
	info := "color_primaries=bt709|color_space=bt709|color_transfer=bt709|"
	chain := buildColorspaceChain(info)

	if chain != "" {
		t.Fatalf("expected SDR colorspace chain to be empty, got %q", chain)
	}
}

func TestBuildDisplayAspectFilterForMetadataUsesDARForAnamorphicDVD(t *testing.T) {
	filter := buildDisplayAspectFilterForMetadata(720, 480, "32:27", "16:9")
	if filter != "scale='trunc(ih*16/9/2)*2:ih',setsar=1" {
		t.Fatalf("filter = %q, want DAR-based widescreen expansion", filter)
	}
}

func TestBuildDisplayAspectFilterForMetadataKeepsSquarePixelVideo(t *testing.T) {
	filter := buildDisplayAspectFilterForMetadata(1920, 1080, "1:1", "16:9")
	if filter != "setsar=1" {
		t.Fatalf("filter = %q, want setsar-only for square-pixel video", filter)
	}
}

func TestBuildDisplayAspectFilterForMetadataSupportsMediaInfoFloatDAR(t *testing.T) {
	filter := buildDisplayAspectFilterForMetadata(720, 480, "", "1.778")
	if filter != "scale='trunc(ih*16/9/2)*2:ih',setsar=1" {
		t.Fatalf("filter = %q, want MediaInfo float DAR to normalize to exact 16:9 expansion", filter)
	}
}

func TestNormalizeMediaInfoAspectRatioMapsCommonWidescreenValue(t *testing.T) {
	ratio := normalizeMediaInfoAspectRatio("1.778")
	if ratio != "16:9" {
		t.Fatalf("ratio = %q, want 16:9", ratio)
	}
}

func TestNormalizeMediaInfoAspectRatioMapsCommonFullscreenValue(t *testing.T) {
	ratio := normalizeMediaInfoAspectRatio("1.333")
	if ratio != "4:3" {
		t.Fatalf("ratio = %q, want 4:3", ratio)
	}
}

func TestBuildAggressivePNGCompressionArgs(t *testing.T) {
	args := buildAggressivePNGCompressionArgs("/tmp/input.png", "/tmp/output.png")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "palettegen=stats_mode=single:max_colors=256") {
		t.Fatalf("expected palettegen in aggressive PNG args, got %q", joined)
	}
	if !strings.Contains(joined, "paletteuse=new=1:dither=sierra2_4a") {
		t.Fatalf("expected paletteuse in aggressive PNG args, got %q", joined)
	}
	if !strings.Contains(joined, "-pix_fmt pal8") {
		t.Fatalf("expected pal8 output in aggressive PNG args, got %q", joined)
	}
}
