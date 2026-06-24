// Package pixhost 负责 Pixhost 上传批次的日志、结果和可上传文件整理。

package pixhost

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// uploadBatch 维护单次图床上传批次的日志、直链和图片结果。
type uploadBatch struct {
	onLog        LogHandler
	onItem       UploadItemHandler
	logLines     []string
	links        []string
	items        []UploadedImage
	lossySet     map[string]struct{}
	lossyIndexes []int
}

// newUploadBatch 会为一次 Pixhost 上传请求创建新的批次状态容器。
func newUploadBatch(lossyFiles []string, onLog LogHandler, onItem UploadItemHandler) *uploadBatch {
	lossySet := make(map[string]struct{}, len(lossyFiles))
	for _, name := range lossyFiles {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		lossySet[name] = struct{}{}
	}

	return &uploadBatch{
		onLog:        onLog,
		onItem:       onItem,
		logLines:     make([]string, 0),
		links:        make([]string, 0),
		items:        make([]UploadedImage, 0),
		lossySet:     lossySet,
		lossyIndexes: make([]int, 0, len(lossySet)),
	}
}

// logs 会返回当前批次累计的完整上传日志文本。
func (b *uploadBatch) logs() string {
	if b == nil {
		return ""
	}
	return strings.Join(b.logLines, "\n")
}

// appendLog 会追加一条上传日志，并在存在实时回调时同步推送。
func (b *uploadBatch) appendLog(format string, args ...any) {
	if b == nil {
		return
	}
	line := fmt.Sprintf(format, args...)
	b.logLines = append(b.logLines, line)
	if b.onLog != nil {
		b.onLog(line)
	}
}

// recordFailure 会记录单张图片上传失败的日志。
func (b *uploadBatch) recordFailure(imagePath string, err error) {
	if b == nil {
		return
	}
	b.appendLog("上传失败: %s (%s)", filepath.Base(imagePath), err.Error())
}

// recordSuccess 会记录单张图片上传成功后的直链、缩略图、元数据和实时回调。
func (b *uploadBatch) recordSuccess(imagePath, directURL, thumbnailURL string) {
	if b == nil {
		return
	}
	if _, ok := b.lossySet[filepath.Base(imagePath)]; ok {
		b.lossyIndexes = append(b.lossyIndexes, len(b.links))
	}

	b.links = append(b.links, directURL)
	item := buildUploadedImage(imagePath, directURL, thumbnailURL)
	b.items = append(b.items, item)
	if b.onItem != nil {
		b.onItem(item)
	}
	b.appendLog("已上传并校准域名: %s", filepath.Base(imagePath))
}

// finalize 会输出批次摘要，并生成最终返回给调用方的整理结果。
func (b *uploadBatch) finalize(total int) (Result, error) {
	if b == nil {
		return Result{}, errors.New("pixhost upload batch is nil")
	}

	b.appendLog("")
	b.appendLog("处理完成! 成功: %d/%d", len(b.links), total)
	if len(b.links) == 0 {
		return Result{Logs: b.logs(), Items: b.items, LossyIndexes: b.lossyIndexes}, errors.New("pixhost upload completed but returned no links")
	}

	output := strings.Join(extractDirectLinks(strings.Join(b.links, "\n")), "\n")
	return Result{
		Output:       output,
		Logs:         b.logs(),
		Items:        b.items,
		LossyIndexes: b.lossyIndexes,
	}, nil
}

// buildUploadedImage 会根据本地文件、原图直链和缩略图构建一条可返回给前端的图片记录。
func buildUploadedImage(imagePath, directURL, thumbnailURL string) UploadedImage {
	item := UploadedImage{
		URL:          strings.TrimSpace(directURL),
		ThumbnailURL: strings.TrimSpace(thumbnailURL),
		Filename:     filepath.Base(imagePath),
	}

	info, err := os.Stat(imagePath)
	if err == nil && !info.IsDir() && info.Size() > 0 {
		item.Size = info.Size()
	}
	item.Width, item.Height = readImageDimensions(imagePath)
	return item
}

// readImageDimensions 从本地原图读取尺寸；失败时保持零值，由前端隐藏该元数据。
func readImageDimensions(path string) (int, int) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0
	}
	return config.Width, config.Height
}

// collectUploadableImages 从路径列表中筛出可上传的图片，并按文件名排序。
func collectUploadableImages(paths []string, maxUploadBytes int64) []string {
	candidates := make([]string, 0, len(paths))
	for _, path := range paths {
		if !isUploadableImage(path, maxUploadBytes) {
			continue
		}
		candidates = append(candidates, path)
	}
	sort.Strings(candidates)
	return candidates
}

// isUploadableImage 检查文件是否存在、尺寸合理且 MIME 类型为图片。
func isUploadableImage(path string, maxUploadBytes int64) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if info.Size() <= 0 || (maxUploadBytes > 0 && info.Size() > maxUploadBytes) {
		return false
	}

	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}

	contentType := http.DetectContentType(header[:n])
	return strings.HasPrefix(contentType, "image/")
}
