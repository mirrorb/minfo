// Package screenshot 实现截图渲染和位图字幕绘制流程。

package screenshot

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"minfo/internal/system"
)

// captureScreenshot 会执行一次完整截图，并在文件过大时自动触发重编码兜底。
func (r *screenshotRunner) captureScreenshot(aligned float64, path string) error {
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
	if r.variant == VariantJPG {
		r.logf("[提示] %s 大小 %.2fMB，重拍降低质量...", filepath.Base(path), sizeMB)
	} else {
		r.logf("[提示] %s 大小 %.2fMB，重拍并映射到 SDR...", filepath.Base(path), sizeMB)
	}

	tempPath := path + ".tmp" + r.settings.Ext
	if err := r.captureReencoded(aligned, tempPath); err != nil {
		_ = os.Remove(tempPath)
		r.logf("[警告] 重拍失败，保留原始截图：%s", err.Error())
		return nil
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
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
			buildDisplayAspectFilter(),
		)
		args = append(args,
			"-filter_complex", filterComplex,
			"-map", "[out]",
			"-",
		)
	} else {
		filterChain := joinFilters(buildDisplayAspectFilter(), "format=gray")
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
		buildDisplayAspectFilter(),
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
	return r.runFFmpeg(args)
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
	filterChain := joinFilters(frameSelect, buildDisplayAspectFilter())

	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = joinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(aligned)),
			subFilter,
			buildDisplayAspectFilter(),
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
	return r.runFFmpeg(args)
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
	filterChain := joinFilters(frameSelect, r.colorChain, buildDisplayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = joinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(aligned)),
			subFilter,
			r.colorChain,
			buildDisplayAspectFilter(),
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
	return r.runFFmpeg(args)
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
		buildDisplayAspectFilter(),
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
	return r.runFFmpeg(args)
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
	filterChain := joinFilters(frameSelect, buildDisplayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = joinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", formatFloat(aligned)),
			subFilter,
			buildDisplayAspectFilter(),
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
	return r.runFFmpeg(args)
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
		buildDisplayAspectFilter(),
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
	return r.runFFmpeg(args)
}

// primaryOutputArgs 返回主流程截图所需的输出编码参数。
func (r *screenshotRunner) primaryOutputArgs() []string {
	if r.variant == VariantJPG {
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(clampJPGQScale(r.settings.JPGQuality))}
	}
	return []string{"-c:v", "png", "-compression_level", "9", "-pred", "mixed"}
}

// runFFmpeg 会执行FFmpeg，并把结果和错误状态返回给调用方。
func (r *screenshotRunner) runFFmpeg(args []string) error {
	stdout, stderr, err := system.RunCommand(r.ctx, r.ffmpegBin, args...)
	if err != nil {
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return nil
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
