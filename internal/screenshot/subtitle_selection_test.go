package screenshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleNeedsBluraySupplementSkipsGenericChinese(t *testing.T) {
	if subtitleNeedsBluraySupplement("zho", "") {
		t.Fatalf("expected generic Chinese from bdsub to skip ffprobe supplement")
	}
}

func TestPreferPreferredSubtitleRankPrefersHigherPayloadBytesForSameLanguagePGS(t *testing.T) {
	best := preferredSubtitleRank{
		LangClass:       "zh",
		LangScore:       subtitleLanguageScore("zh"),
		BitmapKind:      bitmapSubtitlePGS,
		PayloadBytes:    100,
		UsePayloadBytes: true,
	}
	current := preferredSubtitleRank{
		LangClass:       "zh",
		LangScore:       subtitleLanguageScore("zh"),
		BitmapKind:      bitmapSubtitlePGS,
		PayloadBytes:    200,
		UsePayloadBytes: true,
	}

	if !preferPreferredSubtitleRank(current, best) {
		t.Fatalf("expected same-language PGS candidate with higher payload_bytes to win")
	}
}

func TestBlurayHelperNeedsPayloadScanForSameLanguagePGS(t *testing.T) {
	raw := []subtitleTrack{
		{StreamID: "0x1201", Codec: "hdmv_pgs_subtitle"},
		{StreamID: "0x1202", Codec: "hdmv_pgs_subtitle"},
	}
	helper := []blurayHelperTrack{
		{PID: 0x1201, Lang: "zho"},
		{PID: 0x1202, Lang: "zho"},
	}

	if !blurayHelperNeedsPayloadScan(raw, blurayHelperResult{BitrateMode: "metadata-only"}, helper, nil, "helper") {
		t.Fatalf("expected same-language PGS tracks to require payload scan")
	}
}

func TestBlurayHelperHasPayloadBytesAcceptsSampledMode(t *testing.T) {
	if !blurayHelperHasPayloadBytes(blurayHelperResult{BitrateMode: "sampled-payload-bytes"}) {
		t.Fatalf("expected sampled payload mode to be treated as payload-ready")
	}
}

func TestShouldExtractInternalTextSubtitleForTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		requested: []float64{10, 20, 30, 40},
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "subrip",
		},
	}

	if !runner.shouldExtractInternalTextSubtitle() {
		t.Fatalf("expected internal text subtitle task to extract once")
	}
}

func TestShouldExtractInternalTextSubtitleForSingleShotTask(t *testing.T) {
	runner := &screenshotRunner{
		requested: []float64{10},
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "subrip",
		},
	}

	if !runner.shouldExtractInternalTextSubtitle() {
		t.Fatalf("expected single-shot internal text subtitle task to extract once")
	}
}

func TestShouldExtractInternalTextSubtitleSkipsASSLikeCodecs(t *testing.T) {
	runner := &screenshotRunner{
		requested: []float64{10, 20},
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "ass",
		},
	}

	if !runner.shouldExtractInternalTextSubtitle() {
		t.Fatalf("expected ASS subtitles to extract in original format")
	}
}

func TestIsSupportedTextSubtitleCodec(t *testing.T) {
	supported := []string{"ass", "ssa", "subrip", "srt"}
	for _, codec := range supported {
		if !isSupportedTextSubtitleCodec(codec) {
			t.Fatalf("expected %q to be supported", codec)
		}
	}

	unsupported := []string{"mov_text", "webvtt", "text", "unknown"}
	for _, codec := range unsupported {
		if isSupportedTextSubtitleCodec(codec) {
			t.Fatalf("expected %q to be unsupported", codec)
		}
	}
}

func TestIsSupportedTextSubtitlePath(t *testing.T) {
	if !isSupportedTextSubtitlePath("/tmp/demo.ass") {
		t.Fatal("expected ASS path to be supported")
	}
	if !isSupportedTextSubtitlePath("/tmp/demo.srt") {
		t.Fatal("expected SRT path to be supported")
	}
	if isSupportedTextSubtitlePath("/tmp/demo.vtt") {
		t.Fatal("expected VTT path to be unsupported")
	}
}

func TestPrepareTextSubtitleRenderSourceReturnsErrorForUnsupportedTextSubtitle(t *testing.T) {
	runner := &screenshotRunner{
		subtitle: subtitleSelection{
			Mode:  "internal",
			Codec: "mov_text",
		},
	}

	err := runner.prepareTextSubtitleRenderSource()
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
		subtitle:     subtitleSelection{},
	}

	err := runner.chooseSubtitle()
	if err == nil {
		t.Fatal("expected unsupported external text subtitle error")
	}
	if !strings.Contains(err.Error(), "unsupported text subtitle codec") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInternalTextSubtitleExtractionPlanForASS(t *testing.T) {
	pattern, codecArg, extractedCodec, logMessage := internalTextSubtitleExtractionPlan("ass")

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
	pattern, codecArg, extractedCodec, logMessage := internalTextSubtitleExtractionPlan("ssa")

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
