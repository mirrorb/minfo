// Package screenshot 负责截图运行器的初始化与资源清理编排。

package screenshot

import (
	"os"
	"path/filepath"
	"strings"

	screenshotdvdinfo "minfo/internal/screenshot/dvdinfo"
	screenshotruntime "minfo/internal/screenshot/runtime"
	screenshotsource "minfo/internal/screenshot/source"
	screenshotsubtitle "minfo/internal/screenshot/subtitle"
	screenshottimestamps "minfo/internal/screenshot/timestamps"
	"minfo/internal/system"
)

// init 会初始化截图运行器依赖、时间点、字幕状态和渲染参数。
func (r *screenshotRunner) init(timestamps []string) error {
	if err := r.resolveRuntimeTools(); err != nil {
		return err
	}
	if err := r.prepareRequestedTimestamps(timestamps); err != nil {
		return err
	}
	if err := r.prepareOutputDir(); err != nil {
		return err
	}
	if err := r.prepareMediaTimeline(); err != nil {
		return err
	}
	if err := r.prepareSubtitlePipeline(); err != nil {
		return err
	}
	r.prepareRenderPipeline()
	return nil
}

// resolveRuntimeTools 会解析截图流程依赖的外部工具路径。
func (r *screenshotRunner) resolveRuntimeTools() error {
	var err error

	r.tools.FFmpegBin, err = system.ResolveBin(system.FFmpegBinaryPath)
	if err != nil {
		return err
	}
	r.tools.FFprobeBin, err = system.ResolveBin(system.FFprobeBinaryPath)
	if err != nil {
		return err
	}
	if bin, binErr := system.ResolveBin(system.MediaInfoBinaryPath); binErr == nil {
		r.tools.MediaInfoBin = bin
	}
	if bin, binErr := system.ResolveBin(system.OxiPNGBinaryPath); binErr == nil {
		r.tools.OxiPNGBin = bin
	}
	if bin, binErr := system.ResolveBin(system.PNGQuantBinaryPath); binErr == nil {
		r.tools.PNGQuantBin = bin
	}
	if bin, binErr := system.ResolveBin(system.BDSubBinaryPath); binErr == nil {
		r.tools.BDSubBin = bin
	}
	return nil
}

// prepareRequestedTimestamps 会解析请求里的截图时间点并写回运行器状态。
func (r *screenshotRunner) prepareRequestedTimestamps(timestamps []string) error {
	requested, err := screenshottimestamps.ParseRequestedTimestamps(timestamps)
	if err != nil {
		return err
	}
	r.requested = requested
	return nil
}

// prepareOutputDir 会创建并清空本轮截图任务的输出目录。
func (r *screenshotRunner) prepareOutputDir() error {
	if err := os.MkdirAll(r.outputDir, 0o755); err != nil {
		return err
	}
	return clearDir(r.outputDir)
}

// prepareMediaTimeline 会准备截图流程需要的媒体起点、时长和 DVD 预读信息。
func (r *screenshotRunner) prepareMediaTimeline() error {
	r.media.StartOffset = r.detectStartOffset()

	duration, err := screenshottimestamps.ProbeMediaDuration(r.ctx, r.tools.FFprobeBin, r.sourcePath)
	if err != nil {
		return err
	}
	r.media.Duration = duration

	if screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		r.subtitleFlow().PreloadDVDMediaInfo()
	}
	return nil
}

// prepareSubtitlePipeline 会完成选轨、提取、字体准备和字幕索引建立。
func (r *screenshotRunner) prepareSubtitlePipeline() error {
	subtitleRunner := r.subtitleFlow()
	if r.subtitleMode != SubtitleModeOff {
		subtitleRunner.PrepareBlurayProbeContext()
	}
	if err := subtitleRunner.Choose(); err != nil {
		return err
	}
	if err := subtitleRunner.PrepareTextSubtitleRenderSource(); err != nil {
		return err
	}
	subtitleRunner.PrepareEmbeddedFonts()
	subtitleRunner.LogSelectedSubtitleSummary()
	if r.subtitle.Mode != "none" {
		r.ensureSubtitleIndex()
	}
	return nil
}

// prepareRenderPipeline 会准备截图渲染所需的几何、色彩和最终日志摘要。
func (r *screenshotRunner) prepareRenderPipeline() {
	r.logProgress("准备", 1, 3, "正在分析画面参数。")
	r.prepareRenderGeometry()

	r.logProgress("准备", 2, 3, "正在分析色彩空间。")
	r.prepareColorspaceState()

	r.logProgress("准备", 3, 3, "正在准备截图参数。")
	r.finalizeRenderPreparation()
}

// prepareRenderGeometry 会探测视频尺寸、显示几何和位图字幕画布信息。
func (r *screenshotRunner) prepareRenderGeometry() {
	r.media.VideoWidth, r.media.VideoHeight = r.detectVideoDimensions()
	r.render.AspectChain, r.media.DisplayWidth, r.media.DisplayHeight = r.detectDisplayGeometry()
	r.prepareBitmapSubtitleCanvas()
}

// prepareBitmapSubtitleCanvas 会在 PGS 场景下探测字幕画布，并记录对应的提示日志。
func (r *screenshotRunner) prepareBitmapSubtitleCanvas() {
	if !r.isPGSSubtitle() {
		return
	}

	r.render.SubtitleCanvasWidth, r.render.SubtitleCanvasHeight = r.detectBitmapSubtitleCanvasDimensions()
	if !r.hasUsablePGSCanvas() {
		return
	}

	targetWidth, targetHeight := r.bitmapSubtitleTargetSize()
	r.logf("[信息] PGS 画布尺寸：%dx%d | 目标渲染尺寸：%dx%d | 将先处理视频色彩，再按目标尺寸缩放 PGS 后叠加。",
		r.render.SubtitleCanvasWidth,
		r.render.SubtitleCanvasHeight,
		targetWidth,
		targetHeight,
	)
}

// prepareColorspaceState 会探测色彩信息并决定是否优先启用 libplacebo。
func (r *screenshotRunner) prepareColorspaceState() {
	r.render.ColorInfo = r.detectColorspace()
	if !shouldPreferLibplaceboColorspace(r.render.ColorInfo) {
		return
	}

	r.tools.LibplaceboReady = true
	r.logf("[信息] HDR/Dolby Vision 主截图将优先尝试使用 libplacebo 处理。")
	if r.requiresTextSubtitleFilter() {
		r.logf("[信息] 文字字幕场景将优先尝试先执行 libplacebo 色彩映射，再交给 libass 渲染字幕。")
	}
}

// finalizeRenderPreparation 会生成最终色彩链，并输出截图前的统一摘要日志。
func (r *screenshotRunner) finalizeRenderPreparation() {
	r.render.ColorChain = buildColorspaceChain(r.render.ColorInfo, r.tools.LibplaceboReady)
	r.logColorspacePlan()
	r.logProgressPercent("准备", 100, "画面参数准备完成。")
	r.logf("[信息] 容器起始偏移：%.3fs | 影片总时长：%s", r.media.StartOffset, screenshottimestamps.SecToHMS(r.media.Duration))
}

// logColorspacePlan 会输出当前色彩探测与渲染链的统一日志摘要。
func (r *screenshotRunner) logColorspacePlan() {
	if r.render.ColorInfo == "" {
		r.logf("[信息] 无法检测色彩空间，将使用标准转换")
		return
	}

	r.logf("[信息] 检测到色彩空间：%s", strings.TrimSuffix(r.render.ColorInfo, "|"))
	if r.render.ColorChain == "" {
		return
	}
	if r.tools.LibplaceboReady && strings.Contains(r.render.ColorChain, "libplacebo=") {
		r.logf("[信息] HDR/WCG 主截图将统一应用 libplacebo tone mapping / 色域映射。")
		return
	}
	r.logf("[信息] HDR/WCG 主截图将统一应用 tone mapping / 色域映射。")
}

// cleanupTemporarySubtitleResources 会在截图任务结束时清理提取出的字幕临时资源。
func (r *screenshotRunner) cleanupTemporarySubtitleResources() {
	r.cleanupTemporarySubtitleFile()
	r.cleanupTemporarySubtitleFontDir()
}

// cleanupTemporarySubtitleFile 会在截图任务结束时清理提取出的临时字幕文件。
func (r *screenshotRunner) cleanupTemporarySubtitleFile() {
	if strings.TrimSpace(r.subtitleState.TempSubtitleFile) == "" {
		return
	}
	_ = os.Remove(r.subtitleState.TempSubtitleFile)
	r.subtitleState.TempSubtitleFile = ""
}

// cleanupTemporarySubtitleFontDir 会在截图任务结束时清理提取出的附件字体目录。
func (r *screenshotRunner) cleanupTemporarySubtitleFontDir() {
	if strings.TrimSpace(r.subtitleState.SubtitleFontDir) == "" {
		return
	}
	_ = os.RemoveAll(r.subtitleState.SubtitleFontDir)
	r.subtitleState.SubtitleFontDir = ""
}

func (r *screenshotRunner) subtitleFlow() *screenshotsubtitle.Runner {
	return screenshotsubtitle.NewRunner(screenshotsubtitle.RunnerConfig{
		Ctx:                      r.ctx,
		SourcePath:               r.sourcePath,
		DVDMediaInfoPath:         r.dvdMediaInfoPath,
		SubtitleMode:             r.subtitleMode,
		Settings:                 r.settings,
		Tools:                    r.tools,
		Media:                    &r.media,
		SubtitleState:            &r.subtitleState,
		Subtitle:                 &r.subtitle,
		Logf:                     r.logf,
		LogProgress:              r.logProgress,
		LogProgressPercent:       r.logProgressPercent,
		StartHeartbeat:           r.startProgressHeartbeat,
		EnsureDVDMediaInfo:       r.ensureDVDMediaInfoResult,
		IsSupportedBitmap:        r.isSupportedBitmapSubtitle,
		RunFFmpegSubtitleExtract: r.runFFmpegSubtitleExtract,
	})
}

func (r *screenshotRunner) ensureSubtitleIndex() []screenshotruntime.SubtitleSpan {
	return r.subtitleFlow().EnsureIndex()
}

// ensureDVDMediaInfoResult 在 DVD 场景下只探测一次 mediainfo 结果，并缓存供后续字幕与比例逻辑复用。
func (r *screenshotRunner) ensureDVDMediaInfoResult() (screenshotruntime.DVDMediaInfoResult, bool, error) {
	if r == nil || strings.TrimSpace(r.tools.MediaInfoBin) == "" {
		return screenshotruntime.DVDMediaInfoResult{}, false, nil
	}
	if !screenshotsource.LooksLikeDVDSource(r.sourcePath) {
		return screenshotruntime.DVDMediaInfoResult{}, false, nil
	}
	if r.subtitleState.HasDVDMediaInfoResult {
		return r.subtitleState.DVDMediaInfoResult, true, nil
	}

	result, err := screenshotdvdinfo.Probe(r.ctx, r.tools.MediaInfoBin, r.sourcePath, r.dvdMediaInfoPath)
	if err != nil {
		return screenshotruntime.DVDMediaInfoResult{}, false, err
	}
	r.subtitleState.DVDMediaInfoResult = result
	r.subtitleState.HasDVDMediaInfoResult = true
	return result, true, nil
}

// clearDir 删除目录下的所有内容，但保留目录本身。
func clearDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
