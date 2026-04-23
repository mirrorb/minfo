package screenshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	screenshotprogress "minfo/internal/screenshot/progress"
	screenshotruntime "minfo/internal/screenshot/runtime"
	screenshotsubtitle "minfo/internal/screenshot/subtitle"
)

func TestSubtitleNeedsBluraySupplementSkipsGenericChinese(t *testing.T) {
	if screenshotsubtitle.NeedsBluraySupplement("zho", "") {
		t.Fatalf("expected generic Chinese from bdsub to skip ffprobe supplement")
	}
}

func TestPreferPreferredSubtitleRankPrefersHigherPayloadBytesForSameLanguagePGS(t *testing.T) {
	best := screenshotruntime.PreferredSubtitleRank{
		LangClass:       "zh",
		LangScore:       screenshotsubtitle.LanguageScore("zh"),
		BitmapKind:      screenshotruntime.BitmapSubtitlePGS,
		PayloadBytes:    100,
		UsePayloadBytes: true,
	}
	current := screenshotruntime.PreferredSubtitleRank{
		LangClass:       "zh",
		LangScore:       screenshotsubtitle.LanguageScore("zh"),
		BitmapKind:      screenshotruntime.BitmapSubtitlePGS,
		PayloadBytes:    200,
		UsePayloadBytes: true,
	}

	if !screenshotsubtitle.PreferRank(current, best) {
		t.Fatalf("expected same-language PGS candidate with higher payload_bytes to win")
	}
}

func TestBlurayHelperNeedsPayloadScanForSameLanguagePGS(t *testing.T) {
	raw := []screenshotruntime.SubtitleTrack{
		{StreamID: "0x1201", Codec: "hdmv_pgs_subtitle"},
		{StreamID: "0x1202", Codec: "hdmv_pgs_subtitle"},
	}
	helper := []screenshotruntime.BlurayHelperTrack{
		{PID: 0x1201, Lang: "zho"},
		{PID: 0x1202, Lang: "zho"},
	}

	if !screenshotsubtitle.HelperNeedsPayloadScan(raw, screenshotruntime.BlurayHelperResult{BitrateMode: "metadata-only"}, helper, nil, "helper") {
		t.Fatalf("expected same-language PGS tracks to require payload scan")
	}
}

func TestBlurayHelperHasPayloadBytesAcceptsSampledMode(t *testing.T) {
	if !screenshotsubtitle.HelperHasPayloadBytes(screenshotruntime.BlurayHelperResult{BitrateMode: "sampled-payload-bytes"}) {
		t.Fatalf("expected sampled payload mode to be treated as payload-ready")
	}
}

func TestShouldExtractInternalTextSubtitleForTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		requested: []float64{10, 20, 30, 40},
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "internal",
			Codec: "subrip",
		},
	}

	if !runner.subtitleFlow().ShouldExtractInternalTextSubtitle() {
		t.Fatalf("expected internal text subtitle task to extract once")
	}
}

func TestShouldExtractInternalTextSubtitleForSingleShotTask(t *testing.T) {
	runner := &screenshotRunner{
		requested: []float64{10},
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "internal",
			Codec: "subrip",
		},
	}

	if !runner.subtitleFlow().ShouldExtractInternalTextSubtitle() {
		t.Fatalf("expected single-shot internal text subtitle task to extract once")
	}
}

func TestShouldExtractInternalTextSubtitleSkipsASSLikeCodecs(t *testing.T) {
	runner := &screenshotRunner{
		requested: []float64{10, 20},
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "internal",
			Codec: "ass",
		},
	}

	if !runner.subtitleFlow().ShouldExtractInternalTextSubtitle() {
		t.Fatalf("expected ASS subtitles to extract in original format")
	}
}

func TestShouldUseEmbeddedSubtitleFontsForMatroskaASS(t *testing.T) {
	runner := &screenshotRunner{
		sourcePath: "/tmp/demo.mkv",
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "external",
			Codec: "ass",
		},
	}

	if !runner.subtitleFlow().ShouldUseEmbeddedFonts() {
		t.Fatal("expected MKV ASS subtitle render to prefer embedded fonts")
	}
}

func TestShouldUseEmbeddedSubtitleFontsSkipsNonASSAndNonMatroska(t *testing.T) {
	tests := []screenshotRunner{
		{
			sourcePath: "/tmp/demo.mp4",
			subtitle: screenshotruntime.SubtitleSelection{
				Mode:  "external",
				Codec: "ass",
			},
		},
		{
			sourcePath: "/tmp/demo.mkv",
			subtitle: screenshotruntime.SubtitleSelection{
				Mode:  "external",
				Codec: "subrip",
			},
		},
	}

	for _, runner := range tests {
		if runner.subtitleFlow().ShouldUseEmbeddedFonts() {
			t.Fatalf("expected %+v to skip embedded MKV font preparation", runner.subtitle)
		}
	}
}

func TestIsSupportedTextSubtitleCodec(t *testing.T) {
	supported := []string{"ass", "ssa", "subrip", "srt"}
	for _, codec := range supported {
		if !screenshotsubtitle.IsSupportedTextCodec(codec) {
			t.Fatalf("expected %q to be supported", codec)
		}
	}

	unsupported := []string{"mov_text", "webvtt", "text", "unknown"}
	for _, codec := range unsupported {
		if screenshotsubtitle.IsSupportedTextCodec(codec) {
			t.Fatalf("expected %q to be unsupported", codec)
		}
	}
}

func TestIsSupportedTextSubtitlePath(t *testing.T) {
	if !screenshotsubtitle.IsSupportedTextPath("/tmp/demo.ass") {
		t.Fatal("expected ASS path to be supported")
	}
	if !screenshotsubtitle.IsSupportedTextPath("/tmp/demo.srt") {
		t.Fatal("expected SRT path to be supported")
	}
	if screenshotsubtitle.IsSupportedTextPath("/tmp/demo.vtt") {
		t.Fatal("expected VTT path to be unsupported")
	}
}

func TestPrepareTextSubtitleRenderSourceReturnsErrorForUnsupportedTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "internal",
			Codec: "mov_text",
		},
	}

	err := runner.subtitleFlow().PrepareTextSubtitleRenderSource()
	if err == nil {
		t.Fatal("expected unsupported text subtitle codec error")
	}
	if !strings.Contains(err.Error(), "unsupported text subtitle codec") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChooseSubtitleReturnsErrorForUnsupportedExternalTextSubtitle(t *testing.T) {
	workDir := t.TempDir()
	videoPath := filepath.Join(workDir, "movie.mkv")
	subtitlePath := filepath.Join(workDir, "movie.en.vtt")
	if err := os.WriteFile(videoPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile(videoPath) error: %v", err)
	}
	if err := os.WriteFile(subtitlePath, []byte("WEBVTT"), 0o644); err != nil {
		t.Fatalf("WriteFile(subtitlePath) error: %v", err)
	}

	runner := &screenshotRunner{
		sourcePath:   videoPath,
		subtitleMode: SubtitleModeAuto,
		subtitle:     screenshotruntime.SubtitleSelection{},
	}

	err := runner.subtitleFlow().Choose()
	if err == nil {
		t.Fatal("expected unsupported external text subtitle error")
	}
	if !strings.Contains(err.Error(), "unsupported text subtitle codec") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInternalTextSubtitleExtractionPlanForASS(t *testing.T) {
	pattern, codecArg, extractedCodec, logMessage := screenshotsubtitle.InternalTextSubtitleExtractionPlan("ass")

	if pattern != "minfo-sub-*.ass" {
		t.Fatalf("pattern = %q, want %q", pattern, "minfo-sub-*.ass")
	}
	if codecArg != "copy" {
		t.Fatalf("codecArg = %q, want %q", codecArg, "copy")
	}
	if extractedCodec != "ass" {
		t.Fatalf("extractedCodec = %q, want %q", extractedCodec, "ass")
	}
	if logMessage == "" {
		t.Fatal("expected non-empty log message for ASS extraction")
	}
}

func TestInternalTextSubtitleExtractionPlanForSSA(t *testing.T) {
	pattern, codecArg, extractedCodec, logMessage := screenshotsubtitle.InternalTextSubtitleExtractionPlan("ssa")

	if pattern != "minfo-sub-*.ssa" {
		t.Fatalf("pattern = %q, want %q", pattern, "minfo-sub-*.ssa")
	}
	if codecArg != "copy" {
		t.Fatalf("codecArg = %q, want %q", codecArg, "copy")
	}
	if extractedCodec != "ssa" {
		t.Fatalf("extractedCodec = %q, want %q", extractedCodec, "ssa")
	}
	if logMessage == "" {
		t.Fatal("expected non-empty log message for SSA extraction")
	}
}

func TestSubtitleHeartbeatStepPercentApproachesCeiling(t *testing.T) {
	percent := screenshotprogress.SubtitleHeartbeatStepPercent(10 * time.Second)
	if percent <= 0 {
		t.Fatalf("percent = %.1f, want > 0", percent)
	}
	if percent >= 94 {
		t.Fatalf("percent = %.1f, want < 94", percent)
	}
}

func TestSubtitleHeartbeatDetailIncludesElapsedTime(t *testing.T) {
	detail := screenshotprogress.SubtitleHeartbeatDetail("正在探测内封字幕轨。", 75*time.Second)
	if !strings.Contains(detail, "正在探测内封字幕轨。") {
		t.Fatalf("detail = %q, want original message", detail)
	}
	if !strings.Contains(detail, "已耗时 1m15s") {
		t.Fatalf("detail = %q, want compact elapsed time", detail)
	}
}

func TestPreloadDVDMediaInfoLogsProgressBeforeProbe(t *testing.T) {
	runner := &screenshotRunner{
		ctx:          nil,
		sourcePath:   "/tmp/VIDEO_TS/VTS_01_1.VOB",
		subtitleMode: SubtitleModeAuto,
		tools: screenshotruntime.Toolchain{
			MediaInfoBin: "__missing_mediainfo__",
		},
	}

	runner.subtitleFlow().PreloadDVDMediaInfo()

	if !strings.Contains(runner.logs(), "[进度] 字幕 1/3: 正在读取 DVD MediaInfo 字幕元数据。") {
		t.Fatalf("logs = %q, want dvd mediainfo subtitle progress", runner.logs())
	}
}

func TestShouldEmitSubtitleIndexProgressForPGS(t *testing.T) {
	runner := &screenshotRunner{
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "internal",
			Codec: "hdmv_pgs_subtitle",
		},
	}

	if !runner.subtitleFlow().ShouldEmitIndexProgress() {
		t.Fatal("expected PGS subtitle indexing to emit progress")
	}
}

func TestShouldEmitSubtitleIndexProgressSkipsExtractedText(t *testing.T) {
	runner := &screenshotRunner{
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:          "external",
			Codec:         "subrip",
			ExtractedText: true,
		},
	}

	if runner.subtitleFlow().ShouldEmitIndexProgress() {
		t.Fatal("expected extracted text subtitle indexing to avoid duplicate progress stage")
	}
}
