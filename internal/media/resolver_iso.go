// Package media 提供 ISO 识别、挂载后解析和目录内 ISO 搜索辅助函数。

package media

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// isISOFile 判断路径是否以 .iso 结尾。
func isISOFile(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".iso")
}

// findISOInDir 在目录树中查找第一个 ISO 文件。
func findISOInDir(root string) (string, error) {
	var isoPath string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isISOFile(path) {
			isoPath = path
			return errISOFound
		}
		return nil
	})
	if err != nil && !errors.Is(err, errISOFound) {
		return "", err
	}
	if isoPath == "" {
		return "", errNoISO
	}
	return isoPath, nil
}

// findLargestM2TS 在 BDMV 或 BDMV/STREAM 下返回体积最大的 M2TS 文件。
func findLargestM2TS(root string) (string, error) {
	searchRoot := root
	stream := filepath.Join(root, "STREAM")
	if info, err := os.Stat(stream); err == nil && info.IsDir() {
		searchRoot = stream
	}

	var largestPath string
	var largestSize int64
	err := filepath.WalkDir(searchRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".m2ts") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > largestSize {
			largestSize = info.Size()
			largestPath = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if largestPath == "" {
		return "", errors.New("no m2ts files found under BDMV")
	}
	return largestPath, nil
}

// resolveVideoFromMountedISO 挂载 ISO 并在其中选择截图流程要使用的视频文件。
func resolveVideoFromMountedISO(ctx context.Context, isoPath string) (string, func(), error) {
	mountDir, cleanup, err := mountISO(ctx, isoPath)
	if err != nil {
		return "", func() {}, err
	}
	videoPath, err := resolveScreenshotSourceFromRoot(mountDir)
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return videoPath, cleanup, nil
}

// resolveBDInfoFromMountedISO 挂载 ISO 并返回其中可供 BDInfo 扫描的蓝光根目录。
func resolveBDInfoFromMountedISO(ctx context.Context, isoPath string) (BDInfoSource, func(), error) {
	mountDir, cleanup, err := mountISO(ctx, isoPath)
	if err != nil {
		return BDInfoSource{}, func() {}, err
	}
	root, ok := resolveBDInfoRoot(mountDir)
	if !ok {
		cleanup()
		return BDInfoSource{}, func() {}, errors.New("BDMV folder not found in ISO")
	}
	return BDInfoSource{Path: root}, cleanup, nil
}
