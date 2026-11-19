// Package bdinfo 提供 BDInfo 报告提取与筛选逻辑。

package bdinfo

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	sizeLinePattern   = regexp.MustCompile(`^Size:\s*([0-9,]+)\s+bytes`)
	lengthLinePattern = regexp.MustCompile(`^Length:\s*([0-9]+):([0-5][0-9]):([0-5][0-9](?:\.[0-9]{1,9})?)(?:\s|$)`)
)

// ExtractCodeBlock 从带有 [code]...[/code] 的输出中提取最有代表性的代码块；优先选择包含 DISC INFO 的块。
func ExtractCodeBlock(output string) string {
	matches := regexp.MustCompile(`(?is)\[code\](.*?)\[/code\]`).FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return output
	}

	best := ""
	bestScore := -1
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		block := strings.TrimSpace(match[1])
		if block == "" {
			continue
		}

		score := len(block)
		if strings.Contains(strings.ToUpper(block), "DISC INFO:") {
			score += 1_000_000
		}

		if score > bestScore {
			best = block
			bestScore = score
		}
	}

	if best == "" {
		return output
	}
	return best
}

// SelectLongestPlaylistBlock 保留时长最长的 PLAYLIST 区块及其前置说明；
// 时长相同时优先保留体积更大的区块。
func SelectLongestPlaylistBlock(report string) string {
	lines := splitLines(report)
	if len(lines) == 0 {
		return report
	}

	prefix := make([]string, 0, len(lines))
	blocks := make([][]string, 0, 4)
	blockSizes := make([]int64, 0, 4)
	blockLengths := make([]time.Duration, 0, 4)

	currentBlock := make([]string, 0, 64)
	var currentSize int64
	var currentLength time.Duration
	seenPlaylist := false

	saveBlock := func() {
		if len(currentBlock) == 0 {
			return
		}
		blockCopy := append([]string(nil), currentBlock...)
		blocks = append(blocks, blockCopy)
		blockSizes = append(blockSizes, currentSize)
		blockLengths = append(blockLengths, currentLength)
		currentBlock = currentBlock[:0]
		currentSize = 0
		currentLength = 0
	}

	for _, line := range lines {
		if !seenPlaylist {
			if strings.HasPrefix(line, "PLAYLIST:") {
				seenPlaylist = true
			} else {
				prefix = append(prefix, line)
				continue
			}
		}

		if strings.HasPrefix(line, "PLAYLIST:") {
			saveBlock()
		}

		currentBlock = append(currentBlock, line)
		if size := parsePlaylistSize(line); size > currentSize {
			currentSize = size
		}
		if length := parsePlaylistLength(line); length > currentLength {
			currentLength = length
		}
	}
	saveBlock()

	if len(blocks) == 0 {
		return strings.Join(prefix, "\n")
	}

	bestIndex := 0
	for index := 1; index < len(blocks); index++ {
		if blockLengths[index] > blockLengths[bestIndex] ||
			(blockLengths[index] == blockLengths[bestIndex] && blockSizes[index] > blockSizes[bestIndex]) {
			bestIndex = index
		}
	}

	output := make([]string, 0, len(prefix)+len(blocks[bestIndex]))
	output = append(output, prefix...)
	output = append(output, blocks[bestIndex]...)
	return strings.Join(output, "\n")
}

// parsePlaylistLength 读取 Length: h:mm:ss[.fraction]，保留亚秒精度。
func parsePlaylistLength(line string) time.Duration {
	matches := lengthLinePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 4 {
		return 0
	}
	length, err := time.ParseDuration(matches[1] + "h" + matches[2] + "m" + matches[3] + "s")
	if err != nil {
		return 0
	}
	return length
}

// parsePlaylistSize 会解析播放列表大小，并把原始输入转换成结构化结果。
func parsePlaylistSize(line string) int64 {
	matches := sizeLinePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 2 {
		return 0
	}

	raw := strings.ReplaceAll(matches[1], ",", "")
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// splitLines 统一换行符后返回逐行切分结果。
func splitLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
}
