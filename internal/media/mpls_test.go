// Package media 验证 MPLS 解析和 BDInfo playlist 路径推导逻辑。

package media

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestReadMPLSDuration 验证 MPLS 文件时长可以从播放项的 in/out time 正确累加得到。
func TestReadMPLSDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "00080.MPLS")
	if err := os.WriteFile(path, buildTestMPLSFile([][2]uint32{
		{0, 60 * mplsClockRate},
		{0, 30 * mplsClockRate},
	}), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	duration, err := readMPLSDuration(path)
	if err != nil {
		t.Fatalf("readMPLSDuration() error: %v", err)
	}

	if got := formatMPLSDuration(duration); got != "0:01:30" {
		t.Fatalf("formatMPLSDuration(readMPLSDuration()) = %q, want %q", got, "0:01:30")
	}
}

// TestResolveBDInfoSourceFromMPLS 验证单个 MPLS 文件会被转换成蓝光根目录加指定 playlist。
func TestResolveBDInfoSourceFromMPLS(t *testing.T) {
	root := filepath.Join(t.TempDir(), "disc")
	playlistDir := filepath.Join(root, "BDMV", "PLAYLIST")
	if err := os.MkdirAll(playlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	mplsPath := filepath.Join(playlistDir, "00080.MPLS")
	if err := os.WriteFile(mplsPath, buildTestMPLSFile([][2]uint32{{0, 90 * mplsClockRate}}), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	source, cleanup, err := ResolveBDInfoSource(context.Background(), mplsPath)
	if err != nil {
		t.Fatalf("ResolveBDInfoSource() error: %v", err)
	}
	defer cleanup()

	if source.Path != root {
		t.Fatalf("source.Path = %q, want %q", source.Path, root)
	}
	if source.Playlist != "00080.MPLS" {
		t.Fatalf("source.Playlist = %q, want %q", source.Playlist, "00080.MPLS")
	}
}

// TestSuggestPathsIncludesMPLSDuration 验证路径联想会为 MPLS 文件附带格式化时长。
func TestSuggestPathsIncludesMPLSDuration(t *testing.T) {
	root := t.TempDir()
	playlistDir := filepath.Join(root, "BDMV", "PLAYLIST")
	if err := os.MkdirAll(playlistDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}

	mplsPath := filepath.Join(playlistDir, "00001.MPLS")
	if err := os.WriteFile(mplsPath, buildTestMPLSFile([][2]uint32{{0, 75 * mplsClockRate}}), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	items, _, err := SuggestPaths([]string{root}, playlistDir+string(filepath.Separator), 0)
	if err != nil {
		t.Fatalf("SuggestPaths() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Duration != "0:01:15" {
		t.Fatalf("items[0].Duration = %q, want %q", items[0].Duration, "0:01:15")
	}
}

// buildTestMPLSFile 构造只包含播放项时序信息的最小 MPLS 测试文件。
func buildTestMPLSFile(items [][2]uint32) []byte {
	const playlistOffset = 20

	data := make([]byte, playlistOffset+10+len(items)*22)
	copy(data[:8], []byte("MPLS0100"))
	binary.BigEndian.PutUint32(data[8:12], playlistOffset)
	binary.BigEndian.PutUint16(data[playlistOffset+6:playlistOffset+8], uint16(len(items)))

	pos := playlistOffset + 10
	for index, item := range items {
		itemStart := pos
		binary.BigEndian.PutUint16(data[pos:pos+2], 20)
		pos += 2

		clipName := []byte("00000")
		clipName[4] = byte('0' + index)
		copy(data[pos:pos+5], clipName)
		pos += 5

		copy(data[pos:pos+4], []byte("M2TS"))
		pos += 4
		pos += 1
		pos += 2

		binary.BigEndian.PutUint32(data[pos:pos+4], item[0])
		pos += 4
		binary.BigEndian.PutUint32(data[pos:pos+4], item[1])
		pos += 4

		pos = itemStart + 22
	}

	return data
}
