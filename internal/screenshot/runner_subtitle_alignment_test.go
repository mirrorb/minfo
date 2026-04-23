package screenshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	screenshotruntime "minfo/internal/screenshot/runtime"
	screenshotsubtitle "minfo/internal/screenshot/subtitle"
)

// TestParseFFprobePacketCompactLineKeyless 会验证无键名 compact 输出能被正确解析。
func TestParseFFprobePacketCompactLineKeyless(t *testing.T) {
	packet, ok := screenshotsubtitle.ParseFFprobePacketCompactLine("12.500000|0.040000|2048")
	if !ok {
		t.Fatal("expected compact packet line to parse")
	}
	if packet.PTSTime != "12.500000" || packet.DurationTime != "0.040000" || packet.Size != "2048" {
		t.Fatalf("unexpected packet: %+v", packet)
	}
}

// TestParseFFprobePacketCompactLineKeyed 会验证带键名 compact 输出能被正确解析。
func TestParseFFprobePacketCompactLineKeyed(t *testing.T) {
	packet, ok := screenshotsubtitle.ParseFFprobePacketCompactLine("packet|pts_time=12.500000|duration_time=0.040000|size=2048")
	if !ok {
		t.Fatal("expected keyed compact packet line to parse")
	}
	if packet.PTSTime != "12.500000" || packet.DurationTime != "0.040000" || packet.Size != "2048" {
		t.Fatalf("unexpected packet: %+v", packet)
	}
}

// TestSubtitleIndexScanProgressPercent 会验证字幕索引扫描百分比的换算结果。
func TestSubtitleIndexScanProgressPercent(t *testing.T) {
	percent := screenshotsubtitle.IndexScanProgressPercent(50, 100)
	if percent != 47 {
		t.Fatalf("percent = %.1f, want 47.0", percent)
	}
}

// TestSubtitleIndexScanProgressDetail 会验证字幕索引扫描详情文案的拼接格式。
func TestSubtitleIndexScanProgressDetail(t *testing.T) {
	detail := screenshotsubtitle.IndexScanProgressDetail("正在扫描全片 PGS 字幕索引。", 50, 100)
	if detail != "正在扫描全片 PGS 字幕索引。 | 已扫描 00:00:50 / 00:01:40" {
		t.Fatalf("detail = %q, want scan detail", detail)
	}
}

// TestProbePacketSpansParsesCompactOutput 会验证流式 compact 输出能生成正确字幕区间。
func TestProbePacketSpansParsesCompactOutput(t *testing.T) {
	script := writeExecutableTestScript(t, "printf '1.000|0.500|600\n2.000|0.250|50\n3.000|0.250|700\n'\n")
	runner := &screenshotRunner{
		ctx: context.Background(),
		tools: screenshotruntime.Toolchain{
			FFprobeBin: script,
		},
	}

	spans, err := runner.subtitleFlow().ProbePacketSpans(nil, true, 100, -1, 0)
	if err != nil {
		t.Fatalf("probePacketSpans() error = %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("len(spans) = %d, want 2", len(spans))
	}
	if spans[0].Start != 1.0 || spans[0].End != 1.5 {
		t.Fatalf("spans[0] = %+v, want 1.0-1.5", spans[0])
	}
	if spans[1].Start != 3.0 || spans[1].End != 3.25 {
		t.Fatalf("spans[1] = %+v, want 3.0-3.25", spans[1])
	}
}

// TestProbePacketSpansEmitsPTSBasedProgress 会验证进度日志会按 pts 扫描位置实时更新。
func TestProbePacketSpansEmitsPTSBasedProgress(t *testing.T) {
	script := writeExecutableTestScript(t, "printf '10.000|0.500|600\n50.000|0.250|700\n90.000|0.250|900\n'\n")
	runner := &screenshotRunner{
		ctx: context.Background(),
		tools: screenshotruntime.Toolchain{
			FFprobeBin: script,
		},
		media: screenshotruntime.MediaState{
			Duration: 100,
		},
		subtitle: screenshotruntime.SubtitleSelection{
			Mode:  "internal",
			Codec: "hdmv_pgs_subtitle",
		},
	}

	_, err := runner.subtitleFlow().ProbePacketSpans(nil, true, 100, -1, 0)
	if err != nil {
		t.Fatalf("probePacketSpans() error = %v", err)
	}

	logs := runner.logs()
	if !strings.Contains(logs, "[进度] 字幕 47%: 正在扫描全片 PGS 字幕索引。 | 已扫描 00:00:50 / 00:01:40") {
		t.Fatalf("logs = %q, want pts-based subtitle index progress", logs)
	}
}

// writeExecutableTestScript 会生成一个可执行脚本，供测试替代 ffprobe 输出。
func writeExecutableTestScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-ffprobe.sh")
	content := "#!/bin/sh\nset -eu\n" + body
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}
