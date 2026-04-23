// Package media 提供媒体源解析阶段共用的目录根路径识别辅助函数。

package media

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveBDInfoRoot 返回路径对应的蓝光根目录；它接受光盘根目录、BDMV/PLAYLIST/STREAM 目录。
func resolveBDInfoRoot(path string) (string, bool) {
	base := filepath.Base(path)
	if strings.EqualFold(base, "BDMV") {
		return filepath.Dir(path), true
	}
	if strings.EqualFold(base, "PLAYLIST") {
		return filepath.Dir(filepath.Dir(path)), true
	}
	if strings.EqualFold(base, "STREAM") {
		return filepath.Dir(filepath.Dir(path)), true
	}
	bdmv := filepath.Join(path, "BDMV")
	if info, err := os.Stat(bdmv); err == nil && info.IsDir() {
		return path, true
	}
	return "", false
}

// resolveBDMVRoot 返回可用于查找 M2TS 的 BDMV 目录。
func resolveBDMVRoot(path string) (string, bool) {
	base := filepath.Base(path)
	if strings.EqualFold(base, "BDMV") || strings.EqualFold(base, "STREAM") {
		return path, true
	}
	bdmv := filepath.Join(path, "BDMV")
	if info, err := os.Stat(bdmv); err == nil && info.IsDir() {
		return bdmv, true
	}
	return "", false
}

// resolveDVDVideoRoot 返回路径对应的 VIDEO_TS 目录。
func resolveDVDVideoRoot(path string) (string, bool) {
	base := filepath.Base(path)
	if strings.EqualFold(base, "VIDEO_TS") {
		return path, true
	}
	videoTS := filepath.Join(path, "VIDEO_TS")
	if info, err := os.Stat(videoTS); err == nil && info.IsDir() {
		return videoTS, true
	}
	return "", false
}

// resolveDVDFileScreenshotSource 把单个 DVD 控制文件或 VOB 文件转换成截图流程应该使用的视频源。
func resolveDVDFileScreenshotSource(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", false
	}

	info, err := os.Stat(cleaned)
	if err != nil || info.IsDir() {
		return "", false
	}

	base := strings.ToUpper(filepath.Base(cleaned))
	switch strings.ToUpper(filepath.Ext(cleaned)) {
	case ".VOB":
		if strings.EqualFold(base, "VIDEO_TS.VOB") {
			if sourcePath, err := resolveScreenshotSourceFromRoot(filepath.Dir(cleaned)); err == nil {
				return sourcePath, true
			}
		}
		if isDVDTitleVOBName(base) {
			return cleaned, true
		}
	case ".IFO", ".BUP":
		if strings.EqualFold(base, "VIDEO_TS.IFO") || strings.EqualFold(base, "VIDEO_TS.BUP") {
			if sourcePath, err := resolveScreenshotSourceFromRoot(filepath.Dir(cleaned)); err == nil {
				return sourcePath, true
			}
			return "", false
		}
		if controlVOB := dvdTitleVOBPathFromControlFile(cleaned); controlVOB != "" {
			return controlVOB, true
		}
	}

	return "", false
}
