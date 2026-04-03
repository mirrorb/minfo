package screenshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"minfo/internal/media"
	"minfo/internal/system"
)

const (
	defaultScreenshotCount = 4
	minScreenshotCount     = 1
	maxScreenshotCount     = 10

	nativeDVDPacketDiscontinuityGap = 30.0
)

var nativeDVDTitleVOBPattern = regexp.MustCompile(`(?i)^VTS_(\d{2})_([1-9]\d*)\.VOB$`)

const (
	ModeZip   = "zip"
	ModeLinks = "links"

	VariantPNG = "png"
	VariantJPG = "jpg"

	SubtitleModeAuto = "auto"
	SubtitleModeOff  = "off"
)

type ScriptResult struct {
	Files []string
	Logs  string
}

type UploadResult struct {
	Output string
	Logs   string
}

type LogHandler func(line string)

func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeLinks:
		return ModeLinks
	default:
		return ModeZip
	}
}

func NormalizeVariant(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case VariantJPG:
		return VariantJPG
	default:
		return VariantPNG
	}
}

func NormalizeSubtitleMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SubtitleModeOff, "none", "nosub", "false", "0":
		return SubtitleModeOff
	default:
		return SubtitleModeAuto
	}
}

func NormalizeCount(raw string) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultScreenshotCount
	}

	count, err := strconv.Atoi(value)
	if err != nil {
		return defaultScreenshotCount
	}
	switch {
	case count < minScreenshotCount:
		return minScreenshotCount
	case count > maxScreenshotCount:
		return maxScreenshotCount
	default:
		return count
	}
}

func RunScript(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) ([]string, error) {
	result, err := RunScriptWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count)
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

func RunScriptWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (ScriptResult, error) {
	return RunScriptWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, nil)
}

func RunScriptWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (ScriptResult, error) {
	return runNativeScreenshotsWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
}

func RunUpload(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (string, error) {
	result, err := RunUploadWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func RunUploadWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (UploadResult, error) {
	return RunUploadWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, nil)
}

func RunUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (UploadResult, error) {
	return runNativeUploadWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
}

func randomScreenshotTimestamps(ctx context.Context, inputPath string, count int) ([]string, error) {
	count = normalizeCountValue(count)

	sourcePath, cleanup, err := media.ResolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	return randomScreenshotTimestampsForSource(ctx, sourcePath, count)
}

func randomScreenshotTimestampsForSource(ctx context.Context, sourcePath string, count int) ([]string, error) {
	count = normalizeCountValue(count)

	ffprobe, err := system.ResolveBin("FFPROBE_BIN", "ffprobe")
	if err != nil {
		return nil, err
	}

	duration, err := probeMediaDuration(ctx, ffprobe, sourcePath)
	if err != nil {
		return nil, err
	}

	seconds := buildRandomTimestampSeconds(duration, count)
	timestamps := make([]string, 0, len(seconds))
	for _, second := range seconds {
		timestamps = append(timestamps, formatScriptTimestamp(second))
	}
	return timestamps, nil
}

func probeMediaDuration(ctx context.Context, ffprobe, path string) (float64, error) {
	if nativeIsDVDTitleVOB(path) {
		if duration, err := probeDVDTitleVOBPacketDuration(ctx, ffprobe, path); err == nil {
			return duration, nil
		}
	}

	stdout, stderr, err := runFFprobeDuration(ctx, ffprobe, path, "format=duration")
	if err != nil {
		return 0, fmt.Errorf("ffprobe format duration probe failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}

	duration, parseErr := parseDurationOutput(stdout)
	if parseErr == nil {
		return duration, nil
	}

	stdout, stderr, err = runFFprobeDuration(ctx, ffprobe, path, "stream=duration")
	if err != nil {
		return 0, fmt.Errorf("ffprobe format duration unavailable (%v); stream duration probe failed: %s", parseErr, system.BestErrorMessage(err, stderr, stdout))
	}

	duration, streamErr := parseDurationOutput(stdout)
	if streamErr == nil {
		return duration, nil
	}

	if duration, mediaErr := probeMediaInfoDuration(ctx, path); mediaErr == nil {
		return duration, nil
	}

	return 0, fmt.Errorf("ffprobe returned unusable duration: format probe (%v); stream probe (%v)", parseErr, streamErr)
}

func nativeIsDVDTitleVOB(path string) bool {
	return nativeDVDTitleVOBPattern.MatchString(filepath.Base(strings.TrimSpace(path)))
}

func runFFprobeDuration(ctx context.Context, ffprobe, path, entries string) (string, string, error) {
	return system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", entries,
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
}

func probeDVDTitleVOBPacketDuration(ctx context.Context, ffprobe, path string) (float64, error) {
	startOffset, err := probeVideoStartOffset(ctx, ffprobe, path)
	if err != nil {
		return 0, err
	}

	stdout, stderr, err := system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "json",
		path,
	)
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return 0, errors.New("ffprobe returned empty packet payload")
	}

	var payload nativeFFprobePacketsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return 0, err
	}

	duration, ok := nativeAccumulateDVDPacketDuration(payload.Packets, startOffset, nativeDVDPacketDiscontinuityGap)
	if !ok || duration <= 0 {
		return 0, errors.New("ffprobe returned unusable packet duration")
	}
	return duration, nil
}

func probeVideoStartOffset(ctx context.Context, ffprobe, path string) (float64, error) {
	stdout, stderr, err := system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	if err == nil {
		if value, ok := nativeFirstFloatLine(stdout); ok {
			return value, nil
		}
	}

	stdout, stderr, err = system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "format=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	if err == nil {
		if value, ok := nativeFirstFloatLine(stdout); ok {
			return value, nil
		}
	}
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return 0, errors.New("ffprobe returned empty start_time")
}

func probeMediaInfoDuration(ctx context.Context, path string) (float64, error) {
	mediainfo, err := system.ResolveBin("MEDIAINFO_BIN", "mediainfo")
	if err != nil {
		return 0, err
	}

	stdout, stderr, err := system.RunCommand(ctx, mediainfo, "--Output=General;%Duration%", path)
	if err != nil {
		return 0, fmt.Errorf("mediainfo duration probe failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}
	return parseMediaInfoDurationOutput(stdout)
}

func parseMediaInfoDurationOutput(output string) (float64, error) {
	values := strings.FieldsFunc(output, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ';'
	})
	invalid := make([]string, 0, len(values))

	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		milliseconds, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || milliseconds <= 0 {
			invalid = append(invalid, value)
			continue
		}

		return milliseconds / 1000.0, nil
	}

	if len(invalid) == 0 {
		return 0, errors.New("mediainfo returned empty duration")
	}
	return 0, fmt.Errorf("mediainfo returned invalid duration values: %s", strings.Join(invalid, ", "))
}

func parseDurationOutput(output string) (float64, error) {
	lines := strings.Split(output, "\n")
	best := 0.0
	found := false
	invalid := make([]string, 0, len(lines))

	for _, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}

		duration, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
			invalid = append(invalid, value)
			continue
		}

		if !found || duration > best {
			best = duration
			found = true
		}
	}

	if found {
		return best, nil
	}
	if len(invalid) == 0 {
		return 0, errors.New("ffprobe returned empty duration")
	}
	return 0, fmt.Errorf("ffprobe returned invalid duration values: %s", strings.Join(invalid, ", "))
}

func nativeAccumulateDVDPacketDuration(packets []nativeFFprobePacket, startOffset, discontinuityGap float64) (float64, bool) {
	if discontinuityGap <= 0 {
		discontinuityGap = nativeDVDPacketDiscontinuityGap
	}

	clusterStart := 0.0
	clusterEnd := 0.0
	total := 0.0
	started := false

	for _, packet := range packets {
		pts, ok := nativeParseFloatString(packet.PTSTime)
		if !ok {
			continue
		}
		durationValue, ok := nativeParseFloatString(packet.DurationTime)
		if !ok || durationValue < 0 {
			durationValue = 0
		}

		packetStart := pts
		packetEnd := pts + durationValue
		if packetEnd < packetStart {
			packetEnd = packetStart
		}

		if !started {
			clusterStart = math.Min(startOffset, packetStart)
			clusterEnd = packetEnd
			started = true
			continue
		}

		if packetStart > clusterEnd+discontinuityGap || packetEnd < clusterStart-discontinuityGap || packetStart < clusterStart-discontinuityGap {
			if clusterEnd > clusterStart {
				total += clusterEnd - clusterStart
			}
			clusterStart = packetStart
			clusterEnd = packetEnd
			continue
		}

		if packetStart < clusterStart {
			clusterStart = packetStart
		}
		if packetEnd > clusterEnd {
			clusterEnd = packetEnd
		}
	}

	if !started {
		return 0, false
	}
	if clusterEnd > clusterStart {
		total += clusterEnd - clusterStart
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, false
	}
	return total, true
}

func buildRandomTimestampSeconds(duration float64, count int) []int {
	count = normalizeCountValue(count)

	start := 0.0
	end := duration
	if duration > 120 {
		margin := duration * 0.08
		if margin < 15 {
			margin = 15
		}
		if margin > 300 {
			margin = 300
		}
		start = margin
		end = duration - margin
		if end <= start {
			start = 0
			end = duration
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	step := (end - start) / float64(count)
	if step <= 0 {
		step = duration / float64(count+1)
	}

	values := make([]int, 0, count)
	used := make(map[int]struct{}, count)
	for index := 0; index < count; index++ {
		segmentStart := start + step*float64(index)
		segmentEnd := segmentStart + step
		if index == count-1 || segmentEnd > end {
			segmentEnd = end
		}
		if segmentEnd <= segmentStart {
			segmentEnd = segmentStart + 1
		}

		value := int(segmentStart + rng.Float64()*(segmentEnd-segmentStart))
		if value < 0 {
			value = 0
		}
		maxSecond := int(duration)
		if maxSecond > 0 && value >= maxSecond {
			value = maxSecond - 1
		}
		for try := 0; try < 8; try++ {
			if _, exists := used[value]; !exists {
				break
			}
			value++
		}
		used[value] = struct{}{}
		values = append(values, value)
	}

	sort.Ints(values)
	return values
}

func normalizeCountValue(count int) int {
	switch {
	case count == 0:
		return defaultScreenshotCount
	case count < minScreenshotCount:
		return minScreenshotCount
	case count > maxScreenshotCount:
		return maxScreenshotCount
	default:
		return count
	}
}

func formatScriptTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func listScreenshotFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no screenshots were generated")
	}

	sort.Strings(files)
	return files, nil
}

func extractDirectLinks(output string) []string {
	lines := strings.Split(output, "\n")
	links := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			continue
		}
		if strings.ContainsAny(line, " []()<>\"") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		links = append(links, line)
	}
	return links
}

func filterNonEmptyStrings(values ...string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}
