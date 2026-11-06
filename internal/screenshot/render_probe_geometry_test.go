package screenshot

import (
	"context"
	"strings"
	"testing"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

func TestScreenshotGeometryModes(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		sar, dar      string
		mode          string
		wantWidth     int
		wantScale     bool
	}{
		{name: "WEB-DL auto", width: 3840, height: 1744, sar: "1199:1200", dar: "11:5", mode: SizeModeAuto, wantWidth: 3840},
		{name: "WEB-DL original", width: 3840, height: 1744, sar: "1199:1200", dar: "11:5", mode: SizeModeOriginal, wantWidth: 3840},
		{name: "legacy display mode defaults to intelligent correction", width: 3840, height: 1744, sar: "1199:1200", dar: "11:5", mode: "display", wantWidth: 3840},
		{name: "near-square DAR without SAR auto", width: 3840, height: 1744, dar: "11:5", mode: SizeModeAuto, wantWidth: 3840},
		{name: "DAR without SAR within widened tolerance", width: 4000, height: 2000, dar: "2.004", mode: SizeModeAuto, wantWidth: 4000},
		{name: "DAR without SAR outside tolerance", width: 4000, height: 2000, dar: "2.012", mode: SizeModeAuto, wantWidth: 4024, wantScale: true},
		{name: "missing mode defaults to auto", width: 3840, height: 1744, sar: "1199:1200", dar: "11:5", wantWidth: 3840},
		{name: "DVD auto uses DAR override", width: 720, height: 480, sar: "8:9", dar: "16:9", mode: SizeModeAuto, wantWidth: 852, wantScale: true},
		{name: "DVD original", width: 720, height: 480, sar: "32:27", dar: "16:9", mode: SizeModeOriginal, wantWidth: 720},
		{name: "DVD MediaInfo DAR", width: 720, height: 576, dar: "1.778", mode: SizeModeAuto, wantWidth: 1024, wantScale: true},
		{name: "anamorphic MKV", width: 1440, height: 1080, sar: "4:3", dar: "16:9", mode: SizeModeAuto, wantWidth: 1920, wantScale: true},
		{name: "tolerance upper boundary", width: 4000, height: 2000, sar: "1005:1000", mode: SizeModeAuto, wantWidth: 4000},
		{name: "tolerance lower boundary", width: 4000, height: 2000, sar: "995:1000", mode: SizeModeAuto, wantWidth: 4000},
		{name: "within widened tolerance", width: 4000, height: 2000, sar: "1002:1000", mode: SizeModeAuto, wantWidth: 4000},
		{name: "outside tolerance expands", width: 4000, height: 2000, sar: "1006:1000", mode: SizeModeAuto, wantWidth: 4024, wantScale: true},
		{name: "outside tolerance shrinks", width: 4000, height: 2000, sar: "994:1000", mode: SizeModeAuto, wantWidth: 3976, wantScale: true},
		{name: "square pixels", width: 1920, height: 1080, sar: "1:1", dar: "16:9", mode: SizeModeAuto, wantWidth: 1920},
		{name: "original keeps odd width", width: 1921, height: 1080, sar: "4:3", mode: SizeModeOriginal, wantWidth: 1921},
		{name: "unknown aspect keeps dimensions", width: 3840, height: 1744, sar: "N/A", dar: "N/A", mode: SizeModeAuto, wantWidth: 3840},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, width, height := screenshotGeometryForMetadata(tt.width, tt.height, tt.sar, tt.dar, tt.mode)
			if width != tt.wantWidth || height != tt.height {
				t.Fatalf("geometry = %dx%d, want %dx%d", width, height, tt.wantWidth, tt.height)
			}
			if scaled := strings.Contains(filter, "scale="); scaled != tt.wantScale {
				t.Fatalf("filter = %q, wantScale = %v", filter, tt.wantScale)
			}
			if !tt.wantScale && filter != "setsar=1" {
				t.Fatalf("original dimensions must use only setsar=1, got %q", filter)
			}
		})
	}
}

func TestScreenshotSizeModeControlsPGSCanvasAndLogs(t *testing.T) {
	for _, mode := range []string{SizeModeAuto, SizeModeOriginal} {
		t.Run(mode, func(t *testing.T) {
			runner := newScreenshotRunner(context.Background(), "video.mkv", "", t.TempDir(), VariantPNG, SubtitleModeAuto, HDRProcessorLibplacebo, mode, nil)
			runner.render.AspectChain, runner.media.DisplayWidth, runner.media.DisplayHeight = runner.resolveDisplayGeometry(3840, 1744, "1199:1200", "11:5")
			runner.subtitle = screenshotruntime.SubtitleSelection{Mode: "internal", Codec: "hdmv_pgs_subtitle"}
			runner.render.SubtitleCanvasWidth, runner.render.SubtitleCanvasHeight = 1920, 1080

			wantScale := "scale=3840:1744"
			wantGeometry := "3840x1744 -> 3840x1744"
			if got := runner.buildPGSSubtitleScaleChain(); got != wantScale {
				t.Fatalf("PGS scale = %q, want %q", got, wantScale)
			}
			for _, detail := range []string{wantGeometry, "SAR=1199:1200", "DAR=11:5"} {
				if !strings.Contains(runner.logs(), detail) {
					t.Fatalf("missing %q in geometry log: %s", detail, runner.logs())
				}
			}
		})
	}
}
