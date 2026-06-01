// Package pixhost 提供截图上传到 Pixhost 的独立子模块。

package pixhost

import screenshotruntime "minfo/internal/screenshot/runtime"

// UploadedImage 表示一次图床上传后返回的单张图片结果。
type UploadedImage struct {
	URL      string
	Filename string
	Size     int64
}

// UploadItemHandler 处理图床上传过程中单张已完成图片的实时回调。
type UploadItemHandler func(item UploadedImage)

// LogHandler 处理上传流程产生的单行实时日志。
type LogHandler = screenshotruntime.LineHandler

// UploadOptions 表示图床上传时的可选运行参数。
type UploadOptions struct {
	ProxyURL string
}

// Result 表示一次 Pixhost 上传批次返回的直链、日志和图片结果。
type Result struct {
	Output       string
	Logs         string
	Items        []UploadedImage
	LossyIndexes []int
}
