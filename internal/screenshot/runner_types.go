// Package screenshot 定义截图运行器的核心状态结构。

package screenshot

import (
	"context"

	screenshotruntime "minfo/internal/screenshot/runtime"
)

type screenshotRunner struct {
	ctx              context.Context
	sourcePath       string
	dvdMediaInfoPath string
	outputDir        string
	variant          string
	subtitleMode     string
	hdrProcessor     string
	requested        []float64
	settings         screenshotruntime.VariantSettings
	tools            screenshotruntime.Toolchain
	logger           screenshotruntime.Logger
	lossyPNGFiles    map[string]struct{}
	media            screenshotruntime.MediaState
	render           screenshotruntime.RenderState
	subtitleState    screenshotruntime.SubtitleState

	subtitle screenshotruntime.SubtitleSelection

	activeShot screenshotruntime.ActiveShot
}
