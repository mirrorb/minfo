// Package media 提供 MPLS 文件识别、元数据解析和 BDInfo playlist 推导逻辑。

package media

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const mplsClockRate = 45000

// ListBDInfoPlaylists 返回通过 BDInfo 默认短列表和循环列表筛选的文件名。
// 与路径浏览共用规则，避免整盘扫描重新包含已隐藏的 playlist。
func ListBDInfoPlaylists(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isMPLSFile(entry.Name()) {
			continue
		}
		if _, visible := suggestedPlaylist(filepath.Join(dir, entry.Name())); visible {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// isMPLSFile 判断路径是否指向 MPLS 播放列表文件。
func isMPLSFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".mpls")
}

// resolveBDInfoPlaylistSelection 从单个 MPLS 文件路径推导蓝光根目录和对应 playlist 名称。
func resolveBDInfoPlaylistSelection(path string) (string, string, bool) {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" || !isMPLSFile(cleaned) {
		return "", "", false
	}

	playlistDir := filepath.Dir(cleaned)
	if !strings.EqualFold(filepath.Base(playlistDir), "PLAYLIST") {
		return "", "", false
	}

	bdmvDir := filepath.Dir(playlistDir)
	if !strings.EqualFold(filepath.Base(bdmvDir), "BDMV") {
		return "", "", false
	}

	root := filepath.Clean(filepath.Dir(bdmvDir))
	playlist := strings.ToUpper(filepath.Base(cleaned))
	if root == "" || playlist == "" {
		return "", "", false
	}
	return root, playlist, true
}

// readMPLSDuration 解析 MPLS 文件里主播放项的总时长。
func readMPLSDuration(path string) (time.Duration, error) {
	metadata, err := readMPLSMetadata(path)
	return metadata.Duration, err
}

type mplsMetadata struct {
	Duration time.Duration
	HasLoops bool
}

// readMPLSMetadata 读取主角度播放项；循环判定与 BDInfo 0.7.5.6 一致：
// 同一 clip 的同一入点出现多次，而非仅仅重复使用同一 clip。
func readMPLSMetadata(path string) (mplsMetadata, error) {
	var metadata mplsMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata, err
	}
	if len(data) < 18 {
		return metadata, fmt.Errorf("mpls: file too short")
	}

	fileType := string(data[:8])
	switch fileType {
	case "MPLS0100", "MPLS0200", "MPLS0300":
	default:
		return metadata, fmt.Errorf("mpls: unsupported file type %q", fileType)
	}

	playlistOffset := int(readUint32BE(data[8:12]))
	if playlistOffset < 0 || playlistOffset+10 > len(data) {
		return metadata, fmt.Errorf("mpls: invalid playlist offset")
	}

	pos := playlistOffset
	pos += 4 // playlist_length
	pos += 2 // reserved
	itemCount := int(readUint16BE(data[pos : pos+2]))
	pos += 2
	pos += 2 // subitem_count

	var totalTicks uint64
	type clipStart struct {
		name  string
		ticks uint32
	}
	seen := make(map[clipStart]struct{})
	for itemIndex := 0; itemIndex < itemCount; itemIndex++ {
		itemStart := pos
		if itemStart+2 > len(data) {
			return metadata, fmt.Errorf("mpls: truncated playlist item header")
		}

		itemLength := int(readUint16BE(data[pos : pos+2]))
		itemEnd := itemStart + itemLength + 2
		if itemEnd > len(data) {
			return metadata, fmt.Errorf("mpls: truncated playlist item body")
		}
		if itemLength < 20 {
			return metadata, fmt.Errorf("mpls: truncated in/out time fields")
		}
		clipName := strings.ToUpper(string(data[itemStart+2 : itemStart+7]))

		pos += 2 // item_length
		pos += 5 // clip_information_file_name
		pos += 4 // clip_codec_identifier
		pos += 1 // reserved
		pos += 2 // flags

		inTime := readUint32BE(data[pos : pos+4])
		pos += 4
		outTime := readUint32BE(data[pos : pos+4])
		pos += 4
		key := clipStart{name: clipName, ticks: inTime}
		if _, exists := seen[key]; exists {
			metadata.HasLoops = true
		}
		seen[key] = struct{}{}

		if outTime > inTime {
			totalTicks += uint64(outTime - inTime)
		}

		pos = itemEnd
	}

	metadata.Duration = time.Duration(totalTicks * uint64(time.Second) / mplsClockRate)
	return metadata, nil
}

// formatMPLSDuration 把 MPLS 时长格式化为 H:MM:SS。
func formatMPLSDuration(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}

	totalSeconds := int64((duration + time.Second/2) / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
}

// readUint16BE 按大端序读取两个字节的无符号整数。
func readUint16BE(data []byte) uint16 {
	return uint16(data[0])<<8 | uint16(data[1])
}

// readUint32BE 按大端序读取四个字节的无符号整数。
func readUint32BE(data []byte) uint32 {
	return uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
}
