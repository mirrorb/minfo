package screenshot

import "testing"

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
