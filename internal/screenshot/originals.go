package screenshot

import (
	"fmt"
	"path/filepath"
	"strings"

	screenshotdelivery "minfo/internal/screenshot/delivery"
)

func prepareOriginalPNGDownloads(result ScreenshotsResult, onLog LogHandler) (map[string]string, string) {
	lossy := make(map[string]bool, len(result.LossyPNGFiles))
	for _, name := range result.LossyPNGFiles {
		lossy[name] = true
	}
	urls := make(map[string]string, len(lossy))
	var logs []string
	for _, path := range result.Files {
		name := filepath.Base(path)
		if !lossy[name] {
			continue
		}
		token, err := screenshotdelivery.SaveOriginalPNG(screenshotdelivery.OriginalPNGPath(path))
		var message string
		if err != nil {
			message = fmt.Sprintf("[警告] %s 无损原图缓存失败：%s", name, err)
		} else {
			urls[name] = "/api/screenshot-original?token=" + token
			message = fmt.Sprintf("[信息] %s 无损原图已保留，可在 30 分钟内下载。", name)
		}
		logs = append(logs, message)
		if onLog != nil {
			onLog(message)
		}
	}
	return urls, strings.Join(logs, "\n")
}
