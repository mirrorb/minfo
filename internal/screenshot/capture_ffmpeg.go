// Package screenshot 负责截图流程中的 FFmpeg 执行与回退处理。

package screenshot

import (
	"fmt"
	"strings"
	"time"

	"minfo/internal/system"
)

// runFFmpeg 会执行FFmpeg，并把结果和错误状态返回给调用方。
func (r *screenshotRunner) runFFmpeg(args []string, localWindowSeconds float64) error {
	_, _, err := r.runFFmpegLive(args, "渲染", normalizeRenderProgressWindow(localWindowSeconds), r.ffmpegRenderProgressDetail)
	return err
}

// runRenderWithLibplaceboFallback 会在 libplacebo 渲染崩溃时自动切回兼容链后重试。
func (r *screenshotRunner) runRenderWithLibplaceboFallback(render func() error) error {
	if render == nil {
		return nil
	}

	err := render()
	if err == nil {
		return nil
	}
	if !r.applyLibplaceboRenderFallback(err) {
		return err
	}
	return render()
}

// runFFmpegSubtitleExtract 会执行带实时进度的字幕提取 FFmpeg 命令。
func (r *screenshotRunner) runFFmpegSubtitleExtract(args []string) (string, string, error) {
	return r.runFFmpegLive(args, "字幕", 0, r.ffmpegSubtitleProgressDetail)
}

// runFFmpegLive 会执行带实时进度回调的 FFmpeg 命令，并收集完整输出。
func (r *screenshotRunner) runFFmpegLive(args []string, stage string, localWindowSeconds float64, detailBuilder func(*ffmpegRealtimeState) string) (string, string, error) {
	extraArgs := 4
	if stage == "渲染" && r.usesLibplaceboColorspace() {
		extraArgs += 4
	}
	ffmpegArgs := make([]string, 0, len(args)+extraArgs)
	ffmpegArgs = append(ffmpegArgs, "-progress", "pipe:1", "-nostats")
	if stage == "渲染" && r.usesLibplaceboColorspace() {
		ffmpegArgs = append(ffmpegArgs,
			"-init_hw_device", "vulkan=minfo:llvmpipe",
			"-filter_hw_device", "minfo",
		)
	}
	ffmpegArgs = append(ffmpegArgs, args...)
	r.logf("[ffmpeg][%s] 执行命令: %s", stage, system.FormatCommandForLog(r.ctx, r.tools.FFmpegBin, ffmpegArgs...))

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

	stdout, stderr, err := system.RunCommandLive(r.ctx, r.tools.FFmpegBin, func(stream, line string) {
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

// usesLibplaceboColorspace 会判断当前渲染链是否正在使用 libplacebo。
func (r *screenshotRunner) usesLibplaceboColorspace() bool {
	return r != nil && strings.Contains(r.render.ColorChain, "libplacebo=")
}

// applyLibplaceboRenderFallback 会在识别到崩溃特征时切换到兼容色彩链。
func (r *screenshotRunner) applyLibplaceboRenderFallback(err error) bool {
	if r == nil || err == nil || !r.usesLibplaceboColorspace() {
		return false
	}
	if !isLibplaceboRenderCrashMessage(err.Error()) {
		return false
	}

	originalReady := r.tools.LibplaceboReady
	r.tools.LibplaceboReady = false
	fallbackChain := buildColorspaceChain(r.render.ColorInfo, r.effectiveHDRProcessor())
	if strings.TrimSpace(fallbackChain) == "" {
		r.tools.LibplaceboReady = originalReady
		return false
	}
	r.render.ColorChain = fallbackChain
	r.logf("[提示] libplacebo/Vulkan 渲染失败，自动回退到兼容色彩链后重试当前截图。")
	return true
}

// isLibplaceboRenderCrashMessage 会判断错误文本是否命中已知的 libplacebo 崩溃特征。
func isLibplaceboRenderCrashMessage(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "llvm error:") && strings.Contains(lower, "cannot select:") {
		return true
	}
	if strings.Contains(lower, "in function: cs_co_variant.resume") {
		return true
	}
	if strings.Contains(lower, "assertion failed:") && strings.Contains(lower, "pl_alloc.c") {
		return true
	}
	if strings.Contains(lower, "signal: segmentation fault") {
		return true
	}
	if strings.Contains(lower, "segmentation fault (core dumped)") {
		return true
	}
	return false
}

// usesZscaleColorspace 会判断当前渲染链是否正在使用 zscale / tonemap 兼容链。
func (r *screenshotRunner) usesZscaleColorspace() bool {
	return r != nil && (strings.Contains(r.render.ColorChain, "zscale=") || strings.Contains(r.render.ColorChain, "tonemap="))
}

// renderCoarseBack 会根据字幕类型返回渲染阶段使用的回溯秒数。
func (r *screenshotRunner) renderCoarseBack() int {
	if r == nil {
		return 1
	}
	if r.subtitle.Mode == "internal" && r.isSupportedBitmapSubtitle() {
		if r.subtitleState.BitmapRenderBackOverride > 0 {
			return r.subtitleState.BitmapRenderBackOverride
		}
		if r.settings.RenderBackPGS > 0 {
			return r.settings.RenderBackPGS
		}
		return r.settings.CoarseBackPGS
	}
	if r.settings.RenderBackText > 0 {
		return r.settings.RenderBackText
	}
	return r.settings.CoarseBackText
}
