// Package screenshot 提供截图压缩与重编码辅助逻辑。

package screenshot

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"minfo/internal/system"
)

// compressOversizedPNGIfNeeded 会在 PNG 截图过大时依次尝试无损和有损压缩。
func (r *screenshotRunner) compressOversizedPNGIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= oversizeBytes {
		return
	}

	if err := r.compressOxiPNG(path); err != nil {
		r.logf("[警告] %s oxipng 压缩失败，保留当前截图：%s", filepath.Base(path), err.Error())
		return
	}

	afterInfo, err := os.Stat(path)
	if err != nil {
		return
	}
	afterMB := float64(afterInfo.Size()) / 1024.0 / 1024.0
	if afterInfo.Size() <= oversizeBytes {
		r.logf("[信息] %s 经 oxipng 压缩后大小 %.2fMB。", filepath.Base(path), afterMB)
		return
	}

	r.logf("[提示] %s 经 oxipng 压缩后仍为 %.2fMB，继续使用 pngquant 压缩...", filepath.Base(path), afterMB)
	if err := r.compressPNGQuant(path); err != nil {
		r.logf("[警告] %s pngquant 压缩失败，保留 oxipng 结果：%s", filepath.Base(path), err.Error())
		return
	}
	r.logf("[警告] %s 已使用 pngquant 有损压缩。若介意画质损失，可切换 JPG 重新生成。", filepath.Base(path))

	finalInfo, err := os.Stat(path)
	if err != nil {
		return
	}

	finalMB := float64(finalInfo.Size()) / 1024.0 / 1024.0
	if finalInfo.Size() <= oversizeBytes {
		r.logf("[信息] %s 经 oxipng + pngquant 压缩后大小 %.2fMB。", filepath.Base(path), finalMB)
		return
	}

	r.logf("[警告] %s 经 oxipng + pngquant 压缩后仍为 %.2fMB，图床上传可能跳过该文件。", filepath.Base(path), finalMB)
}

// compressOxiPNG 会调用 oxipng 对 PNG 截图执行无损压缩。
func (r *screenshotRunner) compressOxiPNG(path string) error {
	if strings.TrimSpace(r.tools.OxiPNGBin) == "" {
		return fmt.Errorf("%s not found", system.OxiPNGBinaryPath)
	}

	args := buildOxiPNGCompressionArgs(path)
	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.OxiPNGBin, args...)
	if err != nil {
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return nil
}

// compressPNGQuant 会调用 pngquant 生成替换式的有损压缩 PNG。
func (r *screenshotRunner) compressPNGQuant(path string) error {
	if strings.TrimSpace(r.tools.PNGQuantBin) == "" {
		return fmt.Errorf("%s not found", system.PNGQuantBinaryPath)
	}

	compressedPath := path + ".quant.png"
	_ = os.Remove(compressedPath)

	args := buildPNGQuantCompressionArgs(path, compressedPath)
	stdout, stderr, err := system.RunCommand(r.ctx, r.tools.PNGQuantBin, args...)
	if err != nil {
		_ = os.Remove(compressedPath)
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	if err := os.Rename(compressedPath, path); err != nil {
		_ = os.Remove(compressedPath)
		return err
	}
	r.markLossyPNG(path)
	return nil
}

// buildOxiPNGCompressionArgs 会构造 oxipng 压缩当前截图所需的参数列表。
func buildOxiPNGCompressionArgs(path string) []string {
	return []string{
		"-o", "max",
		"--strip", "safe",
		path,
	}
}

// buildPNGQuantCompressionArgs 会构造 pngquant 输出到目标文件的参数列表。
func buildPNGQuantCompressionArgs(inputPath, outputPath string) []string {
	return []string{
		"256",
		"--force",
		"--output", outputPath,
		"--speed", "1",
		"--strip",
		"--",
		inputPath,
	}
}

// markLossyPNG 会记录当前截图已经经过有损 PNG 压缩。
func (r *screenshotRunner) markLossyPNG(path string) {
	if r == nil {
		return
	}
	if r.lossyPNGFiles == nil {
		r.lossyPNGFiles = make(map[string]struct{})
	}
	name := filepath.Base(strings.TrimSpace(path))
	if name == "" {
		return
	}
	r.lossyPNGFiles[name] = struct{}{}
}

// lossyPNGFileList 会返回按文件名排序的有损 PNG 清单。
func (r *screenshotRunner) lossyPNGFileList() []string {
	if r == nil || len(r.lossyPNGFiles) == 0 {
		return nil
	}
	files := make([]string, 0, len(r.lossyPNGFiles))
	for name := range r.lossyPNGFiles {
		files = append(files, name)
	}
	sort.Strings(files)
	return files
}

// clampJPGQScale 将 JPG qscale 限制在 ffmpeg 可接受的范围内。
func clampJPGQScale(value int) int {
	if value < 1 {
		return 1
	}
	if value > 31 {
		return 31
	}
	return value
}

// fallbackJPGQScale 为超大 JPG 重拍场景选择更保守的 qscale。
func fallbackJPGQScale(value int) int {
	value = clampJPGQScale(value)
	value += 2
	if value > 6 {
		return 6
	}
	return value
}

// jpgReencodeOutputArgs 返回 JPG 重拍流程使用的输出编码参数。
func jpgReencodeOutputArgs(quality int) []string {
	return []string{
		"-c:v", "mjpeg",
		"-q:v", fmt.Sprintf("%d", quality),
	}
}
