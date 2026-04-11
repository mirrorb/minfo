// Package bdinfo 验证 BDInfo 运行时配置。

package bdinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultBinaryPath 验证默认 BDInfo 路径已经统一到 /usr/local/bin/bdinfo。
func TestDefaultBinaryPath(t *testing.T) {
	const want = "/usr/local/bin/bdinfo"
	if defaultBinaryPath != want {
		t.Fatalf("defaultBinaryPath = %q, want %q", defaultBinaryPath, want)
	}
}

// TestBuildCommandArgsDefaultsToWholeDiscMode 验证未显式指定 playlist 时会自动附加 -w。
func TestBuildCommandArgsDefaultsToWholeDiscMode(t *testing.T) {
	t.Setenv("BDINFO_ARGS", "")

	args, err := buildCommandArgs("/media/disc", "/tmp/report", "")
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	want := []string{"-w", "/media/disc", "/tmp/report"}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d (%q)", len(args), len(want), args)
	}
	for index := range want {
		if args[index] != want[index] {
			t.Fatalf("args[%d] = %q, want %q", index, args[index], want[index])
		}
	}
}

// TestBuildCommandArgsKeepsExplicitPlaylistSelection 验证显式 playlist 参数不会再额外注入 -w。
func TestBuildCommandArgsKeepsExplicitPlaylistSelection(t *testing.T) {
	t.Setenv("BDINFO_ARGS", `-m "00006.MPLS,00009.MPLS"`)

	args, err := buildCommandArgs("/media/disc", "/tmp/report", "")
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	want := []string{"-m", "00006.MPLS,00009.MPLS", "/media/disc", "/tmp/report"}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d (%q)", len(args), len(want), args)
	}
	for index := range want {
		if args[index] != want[index] {
			t.Fatalf("args[%d] = %q, want %q", index, args[index], want[index])
		}
	}
}

// TestBuildCommandArgsUsesSelectedMPLS 验证单个 MPLS 输入会自动转换成 -m playlist。
func TestBuildCommandArgsUsesSelectedMPLS(t *testing.T) {
	t.Setenv("BDINFO_ARGS", "")

	args, err := buildCommandArgs("/media/disc", "/tmp/report", "00080.MPLS")
	if err != nil {
		t.Fatalf("buildCommandArgs returned error: %v", err)
	}

	want := []string{"-m", "00080.MPLS", "/media/disc", "/tmp/report"}
	if len(args) != len(want) {
		t.Fatalf("len(args) = %d, want %d (%q)", len(args), len(want), args)
	}
	for index := range want {
		if args[index] != want[index] {
			t.Fatalf("args[%d] = %q, want %q", index, args[index], want[index])
		}
	}
}

// TestHasPlaylistSelectionArg 验证常见 playlist 选择参数都能被正确识别。
func TestHasPlaylistSelectionArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "none", args: nil, want: false},
		{name: "whole short", args: []string{"-w"}, want: true},
		{name: "whole long", args: []string{"--whole"}, want: true},
		{name: "list short", args: []string{"-l"}, want: true},
		{name: "mpls short", args: []string{"-m", "00001.MPLS"}, want: true},
		{name: "mpls short inline", args: []string{"-m=00001.MPLS"}, want: true},
		{name: "mpls long inline", args: []string{"--mpls=00001.MPLS"}, want: true},
		{name: "other args", args: []string{"--version"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPlaylistSelectionArg(tt.args); got != tt.want {
				t.Fatalf("hasPlaylistSelectionArg(%q) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestFindReportFilePrefersTextReport 验证在多个候选文件中优先返回 BDInfo 文本报告。
func TestFindReportFilePrefersTextReport(t *testing.T) {
	workDir := t.TempDir()

	logPath := filepath.Join(workDir, "debug.log")
	if err := os.WriteFile(logPath, []byte("debug"), 0o644); err != nil {
		t.Fatalf("WriteFile(logPath) error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	reportPath := filepath.Join(workDir, "BDINFO.TEST_DISC.txt")
	if err := os.WriteFile(reportPath, []byte("report"), 0o644); err != nil {
		t.Fatalf("WriteFile(reportPath) error: %v", err)
	}

	got, err := findReportFile(workDir)
	if err != nil {
		t.Fatalf("findReportFile returned error: %v", err)
	}
	if got != reportPath {
		t.Fatalf("findReportFile() = %q, want %q", got, reportPath)
	}
}
