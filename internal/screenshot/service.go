// Package screenshot 对外暴露截图、上传和时间点计算服务。

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

	"minfo/internal/system"
)

const (
	defaultScreenshotCount = 4
	minScreenshotCount     = 1
	maxScreenshotCount     = 10

	dvdPacketDiscontinuityGap = 30.0
)

var dvdTitleVOBPattern = regexp.MustCompile(`(?i)^VTS_(\d{2})_([1-9]\d*)\.VOB$`)

const (
	ModeZip   = "zip"
	ModeLinks = "links"

	VariantPNG = "png"
	VariantJPG = "jpg"

	SubtitleModeAuto = "auto"
	SubtitleModeOff  = "off"
)

// ScreenshotsResult 表示一次截图流程返回的文件列表和日志。
type ScreenshotsResult struct {
	Files           []string
	Logs            string
	LossyPNGFiles   []string
	LossyPNGIndexes []int
}

// UploadedImage 表示一次图床上传后返回的单张图片结果。
type UploadedImage struct {
	URL      string
	Filename string
	Size     int64
}

// UploadItemHandler 处理图床上传过程中单张已完成图片的实时回调。
type UploadItemHandler func(item UploadedImage)

// UploadResult 表示一次截图上传流程返回的直链文本和日志。
type UploadResult struct {
	Output          string
	Logs            string
	Items           []UploadedImage
	LossyPNGFiles   []string
	LossyPNGIndexes []int
}

// LogHandler 处理截图流程产生的单行实时日志。
type LogHandler func(line string)

// NormalizeMode 规范化截图接口的 mode；未知值会回落为 zip。
func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeLinks:
		return ModeLinks
	default:
		return ModeZip
	}
}

// NormalizeVariant 规范化截图输出格式；未知值会回落为 png。
func NormalizeVariant(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case VariantJPG:
		return VariantJPG
	default:
		return VariantPNG
	}
}

// NormalizeSubtitleMode 规范化字幕模式；off 和 none 类输入会关闭字幕，其余值使用自动模式。
func NormalizeSubtitleMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case SubtitleModeOff, "none", "nosub", "false", "0":
		return SubtitleModeOff
	default:
		return SubtitleModeAuto
	}
}

// NormalizeCount 规范化截图数量，并限制在允许范围内。
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

// RunScreenshots 执行截图流程并仅返回生成的文件列表。
func RunScreenshots(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) ([]string, error) {
	result, err := RunScreenshotsWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count)
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

// RunScreenshotsWithLogs 执行截图流程并返回文件列表与完整日志。
func RunScreenshotsWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (ScreenshotsResult, error) {
	return RunScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, nil)
}

// RunScreenshotsWithLiveLogs 会执行截图流程，并把实时日志通过回调逐行暴露给调用方。
func RunScreenshotsWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (ScreenshotsResult, error) {
	return runEngineScreenshotsWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog)
}

// RunUpload 执行截图加上传流程并仅返回直链输出。
func RunUpload(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (string, error) {
	result, err := RunUploadWithLogs(ctx, inputPath, outputDir, variant, subtitleMode, count)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

// RunUploadWithLogs 执行截图加上传流程并返回直链输出与完整日志。
func RunUploadWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int) (UploadResult, error) {
	return RunUploadWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, nil)
}

// RunUploadWithLiveLogs 会执行截图加上传流程，并把实时日志通过回调逐行暴露给调用方。
func RunUploadWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (UploadResult, error) {
	return RunUploadWithLiveEvents(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog, nil)
}

// RunUploadWithLiveEvents 会执行截图加上传流程，并把实时日志和已完成图片逐步暴露给调用方。
func RunUploadWithLiveEvents(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler, onItem UploadItemHandler) (UploadResult, error) {
	return runPixhostUploadWithLiveLogs(ctx, inputPath, outputDir, variant, subtitleMode, count, onLog, onItem)
}

// randomScreenshotTimestampsForSource 针对已经解析好的媒体源生成随机截图时间点。
func randomScreenshotTimestampsForSource(ctx context.Context, sourcePath string, count int) ([]string, error) {
	count = normalizeCountValue(count)

	ffprobe, err := system.ResolveBin(system.FFprobeBinaryPath)
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
		timestamps = append(timestamps, formatTimestamp(second))
	}
	return timestamps, nil
}

// probeMediaDuration 优先通过 ffprobe 探测时长；必要时回退到 DVD 包时长或 MediaInfo。
func probeMediaDuration(ctx context.Context, ffprobe, path string) (float64, error) {
	if isDVDTitleVOB(path) {
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

// isDVDTitleVOB 会判断DVD标题VOB是否满足当前条件。
func isDVDTitleVOB(path string) bool {
	return dvdTitleVOBPattern.MatchString(filepath.Base(strings.TrimSpace(path)))
}

// runFFprobeDuration 运行一次 ffprobe 时长查询，并按指定 entries 返回原始输出。
func runFFprobeDuration(ctx context.Context, ffprobe, path, entries string) (string, string, error) {
	return system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", entries,
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
}

// probeDVDTitleVOBPacketDuration 通过视频包时间戳累加估算 DVD 标题 VOB 的实际时长。
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

	var payload ffprobePacketsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return 0, err
	}

	duration, ok := accumulateDVDPacketDuration(payload.Packets, startOffset, dvdPacketDiscontinuityGap)
	if !ok || duration <= 0 {
		return 0, errors.New("ffprobe returned unusable packet duration")
	}
	return duration, nil
}

// probeVideoStartOffset 读取视频流或封装层记录的 start_time。
func probeVideoStartOffset(ctx context.Context, ffprobe, path string) (float64, error) {
	stdout, stderr, err := system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	if err == nil {
		if value, ok := firstFloatLine(stdout); ok {
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
		if value, ok := firstFloatLine(stdout); ok {
			return value, nil
		}
	}
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return 0, errors.New("ffprobe returned empty start_time")
}

// probeMediaInfoDuration 使用 mediainfo 的 General Duration 结果补充时长探测。
func probeMediaInfoDuration(ctx context.Context, path string) (float64, error) {
	mediainfo, err := system.ResolveBin(system.MediaInfoBinaryPath)
	if err != nil {
		return 0, err
	}

	stdout, stderr, err := system.RunCommand(ctx, mediainfo, "--Output=General;%Duration%", path)
	if err != nil {
		return 0, fmt.Errorf("mediainfo duration probe failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}
	return parseMediaInfoDurationOutput(stdout)
}

// parseMediaInfoDurationOutput 从 mediainfo 输出中解析第一个有效的毫秒时长。
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

// parseDurationOutput 从 ffprobe 输出中挑选最大的有效时长值。
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

// accumulateDVDPacketDuration 根据包时间戳累计连续区间时长，并忽略明显跳变。
func accumulateDVDPacketDuration(packets []ffprobePacket, startOffset, discontinuityGap float64) (float64, bool) {
	if discontinuityGap <= 0 {
		discontinuityGap = dvdPacketDiscontinuityGap
	}

	clusterStart := 0.0
	clusterEnd := 0.0
	total := 0.0
	started := false

	for _, packet := range packets {
		pts, ok := parseFloatString(packet.PTSTime)
		if !ok {
			continue
		}
		durationValue, ok := parseFloatString(packet.DurationTime)
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

// buildRandomTimestampSeconds 在媒体时长范围内按分段随机的方式生成截图秒数。
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

// normalizeCountValue 规范化内部使用的截图数量。
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

// listScreenshotFiles 会列出截图文件，并按当前规则返回排序后的结果列表。
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

// extractDirectLinks 从文本中筛出唯一的 HTTP 或 HTTPS 直链。
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

// filterNonEmptyStrings 过滤空字符串并保留原顺序。
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
