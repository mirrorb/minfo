// Package screenshot 实现截图渲染和位图字幕绘制流程。

package screenshot

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	screenshotruntime "minfo/internal/screenshot/runtime"
	screenshottimestamps "minfo/internal/screenshot/timestamps"
)

const defaultPNGCompressionLevel = "3"

// captureScreenshot 会执行一次完整截图，并在文件过大时自动触发重编码兜底。
func (r *screenshotRunner) captureScreenshot(aligned float64, path string) error {
	r.activeShot.SetPhase(screenshotruntime.ActiveShotPhaseRender)
	if err := r.runRenderWithLibplaceboFallback(func() error {
		return r.capturePrimary(aligned, path)
	}); err != nil {
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
		r.logf("[提示] %s 大小 %.2fMB，先使用 oxipng 压缩...", filepath.Base(path), sizeMB)
		r.compressOversizedPNGIfNeeded(path)
		return nil
	}

	r.logf("[提示] %s 大小 %.2fMB，重拍降低质量...", filepath.Base(path), sizeMB)
	tempPath := path + ".tmp" + r.settings.Ext
	r.activeShot.SetPhase(screenshotruntime.ActiveShotPhaseReencode)
	if err := r.runRenderWithLibplaceboFallback(func() error {
		return r.captureReencoded(aligned, tempPath)
	}); err != nil {
		_ = os.Remove(tempPath)
		r.logf("[警告] 重拍失败，保留原始截图：%s", err.Error())
		r.activeShot.SetPhase(screenshotruntime.ActiveShotPhaseRender)
		return nil
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	r.activeShot.SetPhase(screenshotruntime.ActiveShotPhaseRender)
	return nil
}

// capturePrimary 执行首选截图路径，并根据字幕类型选择对应的渲染方案。
func (r *screenshotRunner) capturePrimary(aligned float64, path string) error {
	if r.subtitle.Mode == "external" {
		if _, err := os.Stat(r.subtitle.File); err != nil {
			return fmt.Errorf("subtitle file not found before render: %w", err)
		}
	}

	_, fineSecond, coarseHMS := r.splitCaptureTimeline(aligned, r.renderCoarseBack())

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSPrimary(coarseHMS, fineSecond, path)
		case r.isDVDSubtitle():
			return r.captureDVDPrimary(coarseHMS, fineSecond, path)
		}
	}

	filterChain := joinFilters(r.render.ColorChain, r.displayAspectFilter())

	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		return r.captureTextSubtitleWithOutputArgs(aligned, r.primaryOutputArgs(), path)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", screenshottimestamps.FormatFloat(fineSecond),
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
	_, fineSecond, coarseHMS := r.splitCaptureTimeline(aligned, r.renderCoarseBack())

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSPNGReencoded(coarseHMS, fineSecond, path)
		case r.isDVDSubtitle():
			return r.captureDVDPNGReencoded(coarseHMS, fineSecond, path)
		}
	}

	filterChain := joinFilters(r.render.ColorChain, r.displayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		return r.captureTextSubtitleWithOutputArgs(aligned, pngReencodeOutputArgs(), path)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", screenshottimestamps.FormatFloat(fineSecond),
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
	}
	args = append(args, pngReencodeOutputArgs()...)
	args = append(args, path)
	return r.runFFmpeg(args, fineSecond)
}

// capturePGSPNGReencoded 复用内部位图 PNG 重拍流程处理 PGS 截图。
func (r *screenshotRunner) capturePGSPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	return r.capturePGSBitmapWithOutputArgs(coarseHMS, fineSecond, pngReencodeOutputArgs(), path)
}

// pngReencodeOutputArgs 返回 PNG 重拍流程使用的输出编码参数。
func pngReencodeOutputArgs() []string {
	return []string{
		"-c:v", "png",
		"-compression_level", defaultPNGCompressionLevel,
	}
}

// captureDVDPNGReencoded 复用内部位图 PNG 重拍流程处理 DVD 截图。
func (r *screenshotRunner) captureDVDPNGReencoded(coarseHMS string, fineSecond float64, path string) error {
	return r.captureInternalBitmapPNGReencoded(coarseHMS, fineSecond, path)
}

// captureJPGReencoded 用更低质量的 JPG 参数重新截图以控制文件体积。
func (r *screenshotRunner) captureJPGReencoded(aligned float64, path string) error {
	_, fineSecond, coarseHMS := r.splitCaptureTimeline(aligned, r.renderCoarseBack())

	quality := fallbackJPGQScale(r.settings.JPGQuality)

	if r.subtitle.Mode == "internal" {
		switch {
		case r.isPGSSubtitle():
			return r.capturePGSJPGReencoded(coarseHMS, fineSecond, quality, path)
		case r.isDVDSubtitle():
			return r.captureDVDJPGReencoded(coarseHMS, fineSecond, quality, path)
		}
	}

	filterChain := joinFilters(r.render.ColorChain, r.displayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		return r.captureTextSubtitleWithOutputArgs(aligned, []string{
			"-c:v", "mjpeg",
			"-q:v", strconv.Itoa(quality),
		}, path)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-ss", screenshottimestamps.FormatFloat(fineSecond),
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

// primaryOutputArgs 返回主流程截图所需的输出编码参数。
func (r *screenshotRunner) primaryOutputArgs() []string {
	if r.variant == VariantJPG {
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(clampJPGQScale(r.settings.JPGQuality))}
	}
	return pngReencodeOutputArgs()
}

// splitCaptureTimeline 会把绝对截图时间拆成粗定位、细定位和 ffmpeg 用的 HMS 文本。
func (r *screenshotRunner) splitCaptureTimeline(aligned float64, coarseBack int) (int, float64, string) {
	if coarseBack < 0 {
		coarseBack = 0
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	return coarseSecond, fineSecond, screenshottimestamps.FormatTimestamp(coarseSecond)
}
