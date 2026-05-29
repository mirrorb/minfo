// Package screenshot 定义截图服务对外暴露的常量、结果模型和回调类型。

package screenshot

import (
	screenshotpixhost "minfo/internal/screenshot/pixhost"
	screenshotruntime "minfo/internal/screenshot/runtime"
)

const (
	defaultScreenshotCount = 4
	minScreenshotCount     = 1
	maxScreenshotCount     = 10
)

const (
	ModeZip   = "zip"
	ModeLinks = "links"

	VariantPNG = "png"
	VariantJPG = "jpg"

	SubtitleModeAuto = "auto"
	SubtitleModeOff  = "off"

	HDRProcessorLibplacebo = "libplacebo"
	HDRProcessorZscale     = "zscale"
)

// ScreenshotsResult 表示一次截图流程返回的文件列表和日志。
type ScreenshotsResult struct {
	Files           []string
	Logs            string
	LossyPNGFiles   []string
	LossyPNGIndexes []int
}

// UploadedImage 表示一次图床上传后返回的单张图片结果。
type UploadedImage = screenshotpixhost.UploadedImage

// UploadItemHandler 处理图床上传过程中单张已完成图片的实时回调。
type UploadItemHandler = screenshotpixhost.UploadItemHandler

// UploadResult 表示一次截图上传流程返回的直链文本和日志。
type UploadResult struct {
	Output          string
	Logs            string
	Items           []UploadedImage
	LossyPNGFiles   []string
	LossyPNGIndexes []int
}

// LogHandler 处理截图流程产生的单行实时日志。
type LogHandler = screenshotruntime.LineHandler
