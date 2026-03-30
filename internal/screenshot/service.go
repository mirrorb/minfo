package screenshot

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"minfo/internal/media"
	"minfo/internal/system"
)

const screenshotScriptDir = "/usr/local/share/minfo/scripts"
const screenshotCount = 4

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

func subtitleModeArgs(mode string) []string {
	if mode == SubtitleModeOff {
		return []string{"-nosub"}
	}
	return nil
}

func screenshotScriptName(variant string) string {
	switch variant {
	case VariantJPG:
		return "screenshots_jpg.sh"
	default:
		return "screenshots.sh"
	}
}

func resolveScript(envKey, fallbackName string) (string, error) {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		info, err := os.Stat(value)
		if err != nil {
			return "", fmt.Errorf("%s not found: %v", envKey, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s must point to a file", envKey)
		}
		return value, nil
	}

	candidate := filepath.Join(screenshotScriptDir, fallbackName)
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() {
		return candidate, nil
	}

	return "", fmt.Errorf("%s not found in %s; rebuild the image or set %s to override", fallbackName, screenshotScriptDir, envKey)
}

func RunScript(ctx context.Context, inputPath, outputDir, variant, subtitleMode string) ([]string, error) {
	result, err := RunScriptWithLogs(ctx, inputPath, outputDir, variant, subtitleMode)
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

func RunScriptWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string) (ScriptResult, error) {
	scriptPath, err := resolveScript("SCREENSHOT_SCRIPT", screenshotScriptName(variant))
	if err != nil {
		return ScriptResult{}, err
	}

	sourcePath, cleanup, err := media.ResolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return ScriptResult{}, err
	}
	defer cleanup()

	timestamps, err := randomScreenshotTimestampsForSource(ctx, sourcePath, screenshotCount)
	if err != nil {
		return ScriptResult{}, err
	}

	args := append([]string{scriptPath}, subtitleModeArgs(subtitleMode)...)
	args = append(args, sourcePath, outputDir)
	args = append(args, timestamps...)
	stdout, stderr, err := system.RunCommand(ctx, "bash", args...)
	logs := system.CombineCommandOutput(stdout, stderr)
	if err != nil {
		return ScriptResult{Logs: logs}, fmt.Errorf("screenshot generation failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}

	files, err := listScreenshotFiles(outputDir)
	if err != nil {
		return ScriptResult{Logs: logs}, err
	}
	return ScriptResult{Files: files, Logs: logs}, nil
}

func RunUpload(ctx context.Context, inputPath, outputDir, variant, subtitleMode string) (string, error) {
	result, err := RunUploadWithLogs(ctx, inputPath, outputDir, variant, subtitleMode)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func RunUploadWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string) (UploadResult, error) {
	uploadScript, err := resolveScript("SCREENSHOT_UPLOAD_SCRIPT", "PixhostUpload.sh")
	if err != nil {
		return UploadResult{}, err
	}

	screenshotResult, err := RunScriptWithLogs(ctx, inputPath, outputDir, variant, subtitleMode)
	if err != nil {
		return UploadResult{Logs: screenshotResult.Logs}, err
	}

	stdout, stderr, err := system.RunCommand(ctx, "bash", uploadScript, outputDir)
	uploadLogs := system.CombineCommandOutput(stdout, stderr)
	logs := strings.TrimSpace(strings.Join(filterNonEmptyStrings(screenshotResult.Logs, uploadLogs), "\n\n"))
	if err != nil {
		return UploadResult{Logs: logs}, fmt.Errorf("screenshot upload failed: %s", system.BestErrorMessage(err, stderr, stdout))
	}

	links := extractDirectLinks(stdout)
	if len(links) == 0 {
		output := strings.TrimSpace(stdout)
		if output == "" {
			output = strings.TrimSpace(stderr)
		}
		if output == "" {
			return UploadResult{Logs: logs}, errors.New("pixhost upload completed but returned no links")
		}
		return UploadResult{Output: output, Logs: logs}, nil
	}
	return UploadResult{Output: strings.Join(links, "\n"), Logs: logs}, nil
}

func randomScreenshotTimestamps(ctx context.Context, inputPath string, count int) ([]string, error) {
	if count <= 0 {
		count = screenshotCount
	}

	sourcePath, cleanup, err := media.ResolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	return randomScreenshotTimestampsForSource(ctx, sourcePath, count)
}

func randomScreenshotTimestampsForSource(ctx context.Context, sourcePath string, count int) ([]string, error) {
	if count <= 0 {
		count = screenshotCount
	}

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

	return 0, fmt.Errorf("ffprobe returned unusable duration: format probe (%v); stream probe (%v)", parseErr, streamErr)
}

func runFFprobeDuration(ctx context.Context, ffprobe, path, entries string) (string, string, error) {
	return system.RunCommand(ctx, ffprobe,
		"-v", "error",
		"-show_entries", entries,
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
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

func buildRandomTimestampSeconds(duration float64, count int) []int {
	if count <= 0 {
		count = screenshotCount
	}

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
