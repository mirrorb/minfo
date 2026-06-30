// Package torrent wraps mkbrr for BitTorrent metainfo generation.
// It does not encode torrent metadata itself.
package torrent

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"minfo/internal/system"
)

const (
	DefaultPieceLength = int64(4 << 20)
	MinPieceLength     = int64(16 << 10)
	MaxPieceLength     = int64(128 << 20)
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
var percentPattern = regexp.MustCompile(`(?i)hashing pieces.*?([0-9]{1,3})%`)

// Options contains mkbrr settings exposed by the Web UI.
type Options struct {
	Format      string
	PieceLength int64
	Private     bool
	Trackers    []string
	WebSeeds    []string
	Comment     string
	Source      string
	Name        string
}

// Progress contains a parsed mkbrr progress update.
type Progress struct {
	Percent float64
	Stage   string
	Detail  string
	Done    bool
}

// Create runs mkbrr and writes the generated .torrent to outputPath.
func Create(ctx context.Context, input, outputPath string, options Options, onLine system.OutputLineHandler) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	bin, err := system.ResolveBin(system.MkbrrBinaryPath)
	if err != nil {
		return "", err
	}

	args, err := BuildMkbrrArgs(input, outputPath, options)
	if err != nil {
		return "", err
	}

	stdout, stderr, err := system.RunCommandLive(ctx, bin, onLine, args...)
	if err != nil {
		return "", fmt.Errorf("%s", system.BestErrorMessage(err, stderr, stdout))
	}
	if _, err := os.Stat(outputPath); err != nil {
		return "", err
	}
	return TorrentFilename(input, options.Name), nil
}

// BuildMkbrrArgs converts user-facing options into mkbrr CLI arguments.
func BuildMkbrrArgs(input, outputPath string, options Options) ([]string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, errors.New("missing path")
	}
	if strings.TrimSpace(outputPath) == "" {
		return nil, errors.New("missing output path")
	}
	if normalizeFormat(options.Format) != "v1" {
		return nil, errors.New("mkbrr only supports Torrent V1")
	}

	pieceLength := options.PieceLength
	if pieceLength <= 0 {
		pieceLength = DefaultPieceLength
	}
	pieceExp, err := PieceLengthExponent(pieceLength)
	if err != nil {
		return nil, err
	}

	args := []string{"create", input, "--output", outputPath, "--skip-prefix", "--piece-length", strconv.Itoa(pieceExp)}
	args = append(args, "--private="+strconv.FormatBool(options.Private))
	for _, tracker := range normalizeList(options.Trackers) {
		args = append(args, "--tracker", tracker)
	}
	for _, webSeed := range normalizeList(options.WebSeeds) {
		args = append(args, "--web-seed", webSeed)
	}
	if comment := strings.TrimSpace(options.Comment); comment != "" {
		args = append(args, "--comment", comment)
	}
	if source := strings.TrimSpace(options.Source); source != "" {
		args = append(args, "--source", source)
	}
	if name := cleanName(options.Name); name != "" {
		args = append(args, "--name", name)
	}
	return args, nil
}

// ParseProgressLine extracts useful progress from a mkbrr output line.
func ParseProgressLine(line string) (Progress, bool) {
	cleaned := StripANSI(strings.TrimSpace(line))
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return Progress{}, false
	}

	if strings.Contains(cleaned, "Hashing pieces") {
		progress := Progress{
			Stage:  "正在哈希",
			Detail: "正在计算 torrent 分块哈希。",
		}
		if match := percentPattern.FindStringSubmatch(cleaned); len(match) == 2 {
			percent, err := strconv.ParseFloat(match[1], 64)
			if err == nil {
				progress.Percent = percent
			}
		}
		return progress, true
	}
	if strings.Contains(cleaned, "Files being hashed") || strings.Contains(cleaned, "Concurrency:") {
		return Progress{Percent: 1, Stage: "准备", Detail: "正在整理待制种文件。"}, true
	}
	if strings.HasPrefix(cleaned, "Wrote ") {
		return Progress{Percent: 100, Stage: "完成", Detail: "种子文件已生成。", Done: true}, true
	}
	return Progress{}, false
}

// StripANSI removes terminal color and cursor-control sequences.
func StripANSI(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

// PieceLengthExponent returns the mkbrr --piece-length exponent for a power-of-two piece length.
func PieceLengthExponent(pieceLength int64) (int, error) {
	if pieceLength < MinPieceLength || pieceLength > MaxPieceLength {
		return 0, fmt.Errorf("piece length must be between %s and %s", FormatBytes(MinPieceLength), FormatBytes(MaxPieceLength))
	}
	if pieceLength&(pieceLength-1) != 0 {
		return 0, errors.New("piece length must be a power of two")
	}
	return bits.TrailingZeros64(uint64(pieceLength)), nil
}

// TorrentFilename returns a browser-facing .torrent filename.
func TorrentFilename(input, name string) string {
	filename := cleanName(name)
	if filename == "" {
		filename = cleanName(filepath.Base(strings.TrimSpace(input)))
	}
	if filename == "" {
		filename = "download"
	}
	if strings.EqualFold(filepath.Ext(filename), ".torrent") {
		return filename
	}
	return filename + ".torrent"
}

// FormatBytes renders piece-size validation bounds in MiB/KiB terms.
func FormatBytes(value int64) string {
	if value%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", value/(1<<20))
	}
	if value%(1<<10) == 0 {
		return fmt.Sprintf("%d KiB", value/(1<<10))
	}
	return fmt.Sprintf("%d bytes", value)
}

func normalizeFormat(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "v1"
	}
	return value
}

func normalizeList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cleanName(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\x00", ""))
	value = strings.ReplaceAll(value, "\\", "/")
	value = filepath.Base(value)
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == string(filepath.Separator) {
		return ""
	}
	return value
}
