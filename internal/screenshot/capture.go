// Package screenshot 实现截图渲染和位图字幕绘制流程。

package screenshot

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"minfo/internal/system"
)

// captureScreenshot 会执行一次完整截图，并在文件过大时自动触发重编码兜底。
func (r *screenshotRunner) captureScreenshot(aligned float64, path string) error {
	r.activeRenderPhase = "render"
	if err := r.capturePrimary(aligned, path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= oversizeBytes {
		return nil
	}

	sizeMB := float64(info.Size()) / 1024.0 / 1024.0
	if r.variant != VariantJPG {
		r.logf("[提示] %s 大小 %.2fMB，直接使用调色板 PNG 压缩...", filepath.Base(path), sizeMB)
		r.compressOversizedPNGIfNeeded(path, path)
		return nil
	}

	r.logf("[提示] %s 大小 %.2fMB，重拍降低质量...", filepath.Base(path), sizeMB)
	tempPath := path + ".tmp" + r.settings.Ext
	r.activeRenderPhase = "reencode"
	if err := r.captureReencoded(aligned, tempPath); err != nil {
		_ = os.Remove(tempPath)
		r.logf("[警告] 重拍失败，保留原始截图：%s", err.Error())
		r.activeRenderPhase = "render"
		return nil
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	r.activeRenderPhase = "render"
	return nil
}

func (r *screenshotRunner) compressOversizedPNGIfNeeded(tempPath, finalPath string) {
	info, err := os.Stat(tempPath)
	if err != nil || info.Size() <= oversizeBytes {
		return
	}

	beforeMB := float64(info.Size()) / 1024.0 / 1024.0
	r.logf("[提示] %s 重拍后仍为 %.2fMB，继续使用调色板 PNG 压缩...", filepath.Base(finalPath), beforeMB)

	if err := r.compressAggressivePNG(tempPath); err != nil {
		r.logf("[警告] %s 调色板 PNG 压缩失败，保留当前重拍结果：%s", filepath.Base(finalPath), err.Error())
		return
	}

	afterInfo, err := os.Stat(tempPath)
	if err != nil {
		return
	}

	afterMB := float64(afterInfo.Size()) / 1024.0 / 1024.0
	if afterInfo.Size() > oversizeBytes {
		r.logf("[警告] %s 调色板 PNG 压缩后仍为 %.2fMB，图床上传可能跳过该文件。", filepath.Base(finalPath), afterMB)
		return
	}

	r.logf("[信息] %s 调色板 PNG 压缩后大小 %.2fMB。", filepath.Base(finalPath), afterMB)
}

func (r *screenshotRunner) compressAggressivePNG(path string) error {
	compressedPath := path + ".pal.png"
	_ = os.Remove(compressedPath)

	args := buildAggressivePNGCompressionArgs(path, compressedPath)
	stdout, stderr, err := system.RunCommand(r.ctx, r.ffmpegBin, args...)
	if err != nil {
		_ = os.Remove(compressedPath)
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	if err := os.Rename(compressedPath, path); err != nil {
		_ = os.Remove(compressedPath)
		return err
	}
	return nil
}

func buildAggressivePNGCompressionArgs(inputPath, outputPath string) []string {
	filter := "[0:v]split[p][v];[p]palettegen=stats_mode=single:max_colors=256[pal];[v][pal]paletteuse=new=1:dither=sierra2_4a[out]"
	return []string{
		"-v", "error",
		"-i", inputPath,
		"-filter_complex", filter,
		"-map", "[out]",
		"-frames:v", "1",
		"-pix_fmt", "pal8",
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
		"-y",
		outputPath,
	}
}

// bitmapSubtitleVisibleAt 判断当前内部位图字幕在给定时间点是否真的可见。
func (r *screenshotRunner) bitmapSubtitleVisibleAt(aligned float64) (bool, error) {
	if !r.isSupportedBitmapSubtitle() || r.subtitle.Mode != "internal" {
		return false, nil
	}

	switch {
	case r.isPGSSubtitle():
		return r.pgsSubtitleVisibleAt(aligned)
	case r.isDVDSubtitle():
		return r.dvdSubtitleVisibleAt(aligned)
	default:
		return false, nil
	}
}

// pgsSubtitleVisibleAt 复用通用的内部位图可见性检测逻辑判断 PGS 字幕是否可见。
func (r *screenshotRunner) pgsSubtitleVisibleAt(aligned float64) (bool, error) {
	return r.internalBitmapSubtitleVisibleAt(aligned)
}

// dvdSubtitleVisibleAt 复用通用的内部位图可见性检测逻辑判断 DVD 字幕是否可见。
func (r *screenshotRunner) dvdSubtitleVisibleAt(aligned float64) (bool, error) {
	return r.internalBitmapSubtitleVisibleAt(aligned)
}

// internalBitmapSubtitleVisibleAt 通过比较有无字幕叠加的探测帧来判断位图字幕是否显示出来。
func (r *screenshotRunner) internalBitmapSubtitleVisibleAt(aligned float64) (bool, error) {
	baseFrame, err := r.captureBitmapProbeFrame(r.sourcePath, aligned, false)
	if err != nil {
		return false, err
	}
	subFrame, err := r.captureBitmapProbeFrame(r.sourcePath, aligned, true)
	if err != nil {
		return false, err
	}
	return baseFrame != subFrame, nil
}

// captureBitmapProbeFrame 抓取一帧灰度探测图，用于判断位图字幕在该时刻是否可见。
func (r *screenshotRunner) captureBitmapProbeFrame(inputPath string, localTime float64, withSubtitle bool) (string, error) {
	coarseBack := r.settings.CoarseBackPGS
	coarseSecond := int(math.Max(math.Floor(localTime)-float64(coarseBack), 0))
	fineSecond := localTime - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", inputPath,
		"-ss", formatFloat(fineSecond),
		"-frames:v", "1",
		"-f", "rawvideo",
		"-pix_fmt", "gray",
	}

	if withSubtitle {
		filterComplex := fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10),%s,format=gray[out]",
			r.subtitle.RelativeIndex,
			r.displayAspectFilter(),
		)
		args = append(args,
			"-filter_complex", filterComplex,
			"-map", "[out]",
			"-",
		)
	} else {
		filterChain := joinFilters(r.displayAspectFilter(), "format=gray")
		args = append(args,
			"-map", "0:v:0",
			"-vf", filterChain,
			"-",
		)
	}

	stdout, stderr, err := system.RunCommand(r.ctx, r.ffmpegBin, args...)
	if err != nil {
		return "", fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return stdout, nil
}

// capturePGSPrimary 复用内部位图字幕主流程渲染 PGS 截图。
func (r *screenshotRunner) capturePGSPrimary(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPrimary(coarseHMS, fineSecond, path)
}

// captureDVDPrimary 复用内部位图字幕主流程渲染 DVD 截图。
func (r *screenshotRunner) captureDVDPrimary(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPrimary(coarseHMS, fineSecond, path)
}

// captureInternalBitmapPrimary 使用 overlay 叠加字幕轨，渲染内挂位图字幕的主流程截图。
func (r *screenshotRunner) captureInternalBitmapPrimary(coarseHMS string, fineSecond float64, path string) error {
	filterComplex := joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.colorChain,
		r.displayAspectFilter(),
	)
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
		"-frames:v", "1",
		"-y",
	}
	args = append(args, r.primaryOutputArgs()...)
	args = append(args, path)
	return r.runFFmpeg(args, fineSecond)
}

// capturePrimary 执行首选截图路径，并根据字幕类型选择对应的渲染方案。
func (r *screenshotRunner) capturePrimary(aligned float64, path string) error {
	if r.subtitle.Mode == "external" {
		if _, err := os.Stat(r.subtitle.File); err != nil {
			return fmt.Errorf("subtitle file not found before render: %w", err)
		}
	}

	coarseBack := r.settings.CoarseBackText
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		coarseBack = r.settings.CoarseBackPGS
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSPrimary(coarseHMS, fineSecond, path)
		case r.isDVDSubtitle():
			return r.captureDVDPrimary(coarseHMS, fineSecond, path)
		}
	}

	frameSelect := fmt.Sprintf("setpts=PTS-STARTPTS,select='gte(t,%s)'", formatFloat(fineSecond))
	filterChain := joinFilters(frameSelect, r.colorChain, r.displayAspectFilter())

	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = joinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(aligned)),
			subFilter,
			r.colorChain,
		)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-y",
		"-frames:v", "1",
		"-vf", filterChain,
	}
	args = append(args, r.primaryOutputArgs()...)
	args = append(args, path)
	return r.runFFmpeg(args, fineSecond)
}

// captureReencoded 在原始截图过大时用更保守的编码参数重新截图。
func (r *screenshotRunner) captureReencoded(aligned float64, path string) error {
	if r.variant == VariantJPG {
		return r.captureJPGReencoded(aligned, path)
	}
	return r.capturePNGReencoded(aligned, path)
}

// capturePNGReencoded 用 PNG 重拍截图，并在需要时加入色彩空间转换链。
func (r *screenshotRunner) capturePNGReencoded(aligned float64, path string) error {
	coarseBack := r.settings.CoarseBackText
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		coarseBack = r.settings.CoarseBackPGS
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSPNGReencoded(coarseHMS, fineSecond, path)
		case r.isDVDSubtitle():
			return r.captureDVDPNGReencoded(coarseHMS, fineSecond, path)
		}
	}

	frameSelect := fmt.Sprintf("setpts=PTS-STARTPTS,select='gte(t,%s)'", formatFloat(fineSecond))
	filterChain := joinFilters(frameSelect, r.colorChain, r.displayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = joinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(aligned)),
			subFilter,
			r.colorChain,
		)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSPNGReencoded 复用内部位图 PNG 重拍流程处理 PGS 截图。
func (r *screenshotRunner) capturePGSPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPNGReencoded(coarseHMS, fineSecond, path)
}

// captureDVDPNGReencoded 复用内部位图 PNG 重拍流程处理 DVD 截图。
func (r *screenshotRunner) captureDVDPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPNGReencoded(coarseHMS, fineSecond, path)
}

// captureInternalBitmapPNGReencoded 用 PNG 重新渲染带内挂位图字幕的截图。
func (r *screenshotRunner) captureInternalBitmapPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	filterComplex := joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.colorChain,
		r.displayAspectFilter(),
	)
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
		"-frames:v", "1",
		"-y",
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// captureJPGReencoded 用更低质量的 JPG 参数重新截图以控制文件体积。
func (r *screenshotRunner) captureJPGReencoded(aligned float64, path string) error {
	coarseBack := r.settings.CoarseBackText
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		coarseBack = r.settings.CoarseBackPGS
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := formatTimestamp(coarseSecond)

	quality := fallbackJPGQScale(r.settings.JPGQuality)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSJPGReencoded(coarseHMS, fineSecond, quality, path)
		case r.isDVDSubtitle():
			return r.captureDVDJPGReencoded(coarseHMS, fineSecond, quality, path)
		}
	}

	frameSelect := fmt.Sprintf("setpts=PTS-STARTPTS,select='gte(t,%s)'", formatFloat(fineSecond))
	filterChain := joinFilters(frameSelect, r.colorChain, r.displayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = joinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(aligned)),
			subFilter,
			r.colorChain,
		)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
		"-c:v", "mjpeg",
		"-q:v", strconv.Itoa(quality),
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSJPGReencoded 复用内部位图 JPG 重拍流程处理 PGS 截图。
func (r *screenshotRunner) capturePGSJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	return r.captureInternalBitmapJPGReencoded(coarseHMS, fineSecond, quality, path)
}

// captureDVDJPGReencoded 复用内部位图 JPG 重拍流程处理 DVD 截图。
func (r *screenshotRunner) captureDVDJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	return r.captureInternalBitmapJPGReencoded(coarseHMS, fineSecond, quality, path)
}

// captureInternalBitmapJPGReencoded 用 JPG 重新渲染带内挂位图字幕的截图。
func (r *screenshotRunner) captureInternalBitmapJPGReencoded(coarseHMS string, fineSecond float64, quality int, path string) error {
	filterComplex := joinFilters(
		fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
		r.colorChain,
		r.displayAspectFilter(),
	)
	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", formatFloat(fineSecond),
		"-filter_complex", filterComplex,
		"-frames:v", "1",
		"-y",
		"-c:v", "mjpeg",
		"-q:v", strconv.Itoa(quality),
		path,
	}
	return r.runFFmpeg(args, fineSecond)
}

// primaryOutputArgs 返回主流程截图所需的输出编码参数。
func (r *screenshotRunner) primaryOutputArgs() []string {
	if r.variant == VariantJPG {
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(clampJPGQScale(r.settings.JPGQuality))}
	}
	return []string{"-c:v", "png", "-compression_level", "9", "-pred", "mixed"}
}

// runFFmpeg 会执行FFmpeg，并把结果和错误状态返回给调用方。
func (r *screenshotRunner) runFFmpeg(args []string, localWindowSeconds float64) error {
	_, _, err := r.runFFmpegLive(args, "渲染", normalizeRenderProgressWindow(localWindowSeconds), r.ffmpegRenderProgressDetail)
	return err
}

// runFFmpegSubtitleExtract 会执行带实时进度的字幕提取 FFmpeg 命令。
func (r *screenshotRunner) runFFmpegSubtitleExtract(args []string) (string, string, error) {
	return r.runFFmpegLive(args, "字幕", 0, r.ffmpegSubtitleProgressDetail)
}

func (r *screenshotRunner) runFFmpegLive(args []string, stage string, localWindowSeconds float64, detailBuilder func(*ffmpegRealtimeState) string) (string, string, error) {
	ffmpegArgs := make([]string, 0, len(args)+4)
	ffmpegArgs = append(ffmpegArgs, "-progress", "pipe:1", "-nostats")
	ffmpegArgs = append(ffmpegArgs, args...)

	progress := ffmpegRealtimeState{
		startedAt:     time.Now(),
		windowSeconds: localWindowSeconds,
	}
	done := make(chan struct{})
	defer close(done)

	if stage == "渲染" || stage == "字幕" {
		go func() {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-r.ctx.Done():
					return
				case <-done:
					return
				case <-ticker.C:
					r.emitFFmpegRealtimeProgress("continue", &progress, stage, detailBuilder)
				}
			}
		}()
	}

	stdout, stderr, err := system.RunCommandLive(r.ctx, r.ffmpegBin, func(stream, line string) {
		if stream != "stdout" {
			return
		}
		r.consumeFFmpegProgressLine(strings.TrimSpace(line), &progress, stage, detailBuilder)
	}, ffmpegArgs...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = err.Error()
		}
		return stdout, stderr, fmt.Errorf("%s", message)
	}
	return stdout, stderr, nil
}

type ffmpegRealtimeState struct {
	mu                sync.Mutex
	frame             string
	fps               string
	outTime           string
	outTimeMS         int64
	speed             string
	totalSize         string
	heartbeatCount    int
	lastLoggedPercent float64
	lastLoggedDetail  string
	startedAt         time.Time
	windowSeconds     float64
	firstOutTimeMS    int64
	hasFirstOutTime   bool
}

func (r *screenshotRunner) consumeFFmpegProgressLine(line string, state *ffmpegRealtimeState, stage string, detailBuilder func(*ffmpegRealtimeState) string) {
	if line == "" {
		return
	}

	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}

	if key == "progress" {
		r.emitFFmpegRealtimeProgress(strings.TrimSpace(value), state, stage, detailBuilder)
		return
	}

	state.mu.Lock()
	switch key {
	case "frame":
		state.frame = value
	case "fps":
		state.fps = value
	case "out_time":
		state.outTime = value
	case "out_time_ms":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			state.outTimeMS = parsed
			if !state.hasFirstOutTime {
				state.firstOutTimeMS = parsed
				state.hasFirstOutTime = true
			}
		}
	case "speed":
		state.speed = value
	case "total_size":
		state.totalSize = value
	}
	state.mu.Unlock()
}

func (r *screenshotRunner) emitFFmpegRealtimeProgress(status string, state *ffmpegRealtimeState, stage string, detailBuilder func(*ffmpegRealtimeState) string) {
	if status == "" {
		return
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	percent := r.ffmpegProgressPercent(stage, status, state)
	detail := detailBuilder(state)
	if percent == state.lastLoggedPercent && detail == state.lastLoggedDetail {
		return
	}

	r.logProgressPercent(stage, percent, detail)
	state.lastLoggedPercent = percent
	state.lastLoggedDetail = detail
}

func (r *screenshotRunner) ffmpegProgressPercent(stage, status string, state *ffmpegRealtimeState) float64 {
	if status == "end" {
		return 100
	}

	if stage == "字幕" && r.duration > 0 && state.outTimeMS > 0 {
		totalUS := int64(r.duration * 1_000_000)
		if totalUS > 0 {
			processedUS := state.outTimeMS
			rangeUS := totalUS
			if state.hasFirstOutTime && state.firstOutTimeMS > 0 && state.firstOutTimeMS < totalUS {
				processedUS = maxInt64(state.outTimeMS-state.firstOutTimeMS, 0)
				rangeUS = totalUS - state.firstOutTimeMS
			}
			if rangeUS <= 0 {
				rangeUS = totalUS
			}
			percent := float64(processedUS) / float64(rangeUS) * 100
			if percent < 0.1 {
				percent = 0.1
			}
			return clampProgressPercent(minFloat(percent, 94))
		}
	}

	if stage == "渲染" {
		if percent, ok := approximateRenderProgressPercent(state); ok {
			return percent
		}
	}

	state.heartbeatCount++
	percent := 12 + state.heartbeatCount*8
	if strings.TrimSpace(state.speed) != "" {
		if percent < 26 {
			percent = 26
		}
	}
	if state.outTimeMS > 0 || strings.TrimSpace(state.totalSize) != "" {
		if percent < 48 {
			percent = 48
		}
	}
	if frame, err := strconv.Atoi(strings.TrimSpace(state.frame)); err == nil && frame > 0 {
		if percent < 78 {
			percent = 78
		}
	}
	return clampProgressPercent(minFloat(float64(percent), 94))
}

func approximateRenderProgressPercent(state *ffmpegRealtimeState) (float64, bool) {
	if state == nil || state.windowSeconds <= 0 {
		if percent, ok := approximateUnknownRenderProgressPercent(state); ok {
			return percent, true
		}
		return 0, false
	}

	if state.hasFirstOutTime && state.outTimeMS > state.firstOutTimeMS {
		processedSeconds := float64(state.outTimeMS-state.firstOutTimeMS) / 1_000_000.0
		if processedSeconds > 0 {
			percent := processedSeconds / state.windowSeconds * 100
			if percent < 0.1 {
				percent = 0.1
			}
			return clampProgressPercent(minFloat(percent, 94)), true
		}
	}

	speed, ok := parseFFmpegSpeed(state.speed)
	if !ok || speed <= 0 {
		return 0, false
	}
	elapsed := time.Since(state.startedAt).Seconds()
	if elapsed <= 0 {
		return 0, false
	}
	estimatedTotal := state.windowSeconds / speed
	if estimatedTotal <= 0 {
		if percent, ok := approximateUnknownRenderProgressPercent(state); ok {
			return percent, true
		}
		return 0, false
	}
	percent := elapsed / estimatedTotal * 100
	if percent < 0.1 {
		percent = 0.1
	}
	return clampProgressPercent(minFloat(percent, 94)), true
}

func approximateUnknownRenderProgressPercent(state *ffmpegRealtimeState) (float64, bool) {
	if state == nil || state.startedAt.IsZero() {
		return 0, false
	}
	elapsed := time.Since(state.startedAt).Seconds()
	if elapsed <= 0 {
		return 0, false
	}

	// 单帧截图经常拿不到稳定的 ffmpeg 实时指标，这里用一个平滑的
	// elapsed-time 估算，让进度条持续前进但不会很快冲到头。
	estimate := 1.5
	if state.windowSeconds > 0 {
		estimate = maxFloat(estimate, minFloat(state.windowSeconds, 3.0))
	}

	percent := 94.0 * elapsed / (elapsed + estimate)
	if percent < 0.1 {
		percent = 0.1
	}
	return clampProgressPercent(percent), true
}

func (r *screenshotRunner) ffmpegRenderProgressDetail(state *ffmpegRealtimeState) string {
	base := r.activeRenderProgressLabel()
	return base + r.ffmpegProgressMetricsSuffix(state)
}

func (r *screenshotRunner) ffmpegSubtitleProgressDetail(state *ffmpegRealtimeState) string {
	base := "正在提取内挂文字字幕。"
	return base + r.ffmpegProgressMetricsSuffix(state)
}

func (r *screenshotRunner) ffmpegProgressMetricsSuffix(state *ffmpegRealtimeState) string {
	parts := make([]string, 0, 4)
	if isUsefulFFmpegFrame(state.frame) {
		parts = append(parts, "frame="+strings.TrimSpace(state.frame))
	}
	if isUsefulFFmpegFPS(state.fps) {
		parts = append(parts, "fps="+strings.TrimSpace(state.fps))
	}
	if strings.TrimSpace(state.outTime) != "" && r.activeRenderPhase == "" {
		parts = append(parts, "time="+strings.TrimSpace(state.outTime))
	}
	if isUsefulFFmpegSpeed(state.speed) {
		parts = append(parts, "speed="+strings.TrimSpace(state.speed))
	}
	if len(parts) == 0 {
		return ""
	}
	return " | " + strings.Join(parts, " | ")
}

func (r *screenshotRunner) activeRenderProgressLabel() string {
	if r.activeShotIndex <= 0 || r.activeShotTotal <= 0 || strings.TrimSpace(r.activeShotName) == "" {
		return "正在渲染截图。"
	}
	if r.activeRenderPhase == "reencode" {
		return fmt.Sprintf("正在重拍第 %d/%d 张截图：%s", r.activeShotIndex, r.activeShotTotal, r.activeShotName)
	}
	return fmt.Sprintf("正在渲染第 %d/%d 张截图：%s", r.activeShotIndex, r.activeShotTotal, r.activeShotName)
}

// displayAspectFilter 返回当前截图任务应使用的显示宽高比修正过滤器链。
func (r *screenshotRunner) displayAspectFilter() string {
	if strings.TrimSpace(r.aspectChain) != "" {
		return r.aspectChain
	}
	return buildDisplayAspectFilter()
}

func normalizeRenderProgressWindow(seconds float64) float64 {
	switch {
	case seconds <= 0:
		return 0.5
	case seconds < 0.5:
		return 0.5
	default:
		return seconds
	}
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func isUsefulFFmpegFrame(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	value, err := strconv.Atoi(trimmed)
	return err == nil && value > 0
}

func isUsefulFFmpegFPS(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "n/a") {
		return false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	return err == nil && value > 0
}

func isUsefulFFmpegSpeed(raw string) bool {
	speed, ok := parseFFmpegSpeed(raw)
	return ok && speed > 0
}

func parseFFmpegSpeed(raw string) (float64, bool) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "x"))
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

// buildTextSubtitleFilter 构建 ffmpeg 文本字幕过滤器，适配外挂字幕和内封文字字幕两种场景。
func (r *screenshotRunner) buildTextSubtitleFilter() string {
	if r.subtitle.Mode == "none" {
		return ""
	}

	sizePart := ""
	if r.videoWidth > 0 && r.videoHeight > 0 {
		sizePart = fmt.Sprintf(":original_size=%dx%d", r.videoWidth, r.videoHeight)
	}

	switch r.subtitle.Mode {
	case "external":
		return fmt.Sprintf("subtitles='%s'%s", escapeFilterValue(r.subtitle.File), sizePart)
	case "internal":
		return fmt.Sprintf("subtitles='%s'%s:si=%d", escapeFilterValue(r.sourcePath), sizePart, r.subtitle.RelativeIndex)
	default:
		return ""
	}
}

// bitmapSubtitleKind 返回当前字幕 codec 对应的位图字幕类型。
func (r *screenshotRunner) bitmapSubtitleKind() bitmapSubtitleKind {
	return bitmapSubtitleKindFromCodec(r.subtitle.Codec)
}

// isPGSSubtitle 会判断PGS字幕是否满足当前条件。
func (r *screenshotRunner) isPGSSubtitle() bool {
	return r.bitmapSubtitleKind() == bitmapSubtitlePGS
}

// isDVDSubtitle 会判断DVD字幕是否满足当前条件。
func (r *screenshotRunner) isDVDSubtitle() bool {
	return r.bitmapSubtitleKind() == bitmapSubtitleDVD
}

// isSupportedBitmapSubtitle 会判断受支持位图字幕是否满足当前条件。
func (r *screenshotRunner) isSupportedBitmapSubtitle() bool {
	return r.isPGSSubtitle() || r.isDVDSubtitle()
}

// bitmapSubtitleKindFromCodec 把 codec 名称映射到内部使用的位图字幕类型枚举。
func bitmapSubtitleKindFromCodec(codec string) bitmapSubtitleKind {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hdmv_pgs_subtitle", "pgssub":
		return bitmapSubtitlePGS
	case "dvd_subtitle":
		return bitmapSubtitleDVD
	default:
		return bitmapSubtitleNone
	}
}

// isUnsupportedBitmapSubtitleCodec 会判断Unsupported位图字幕Codec是否满足当前条件。
func isUnsupportedBitmapSubtitleCodec(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "dvb_subtitle", "xsub", "vobsub":
		return true
	default:
		return false
	}
}
