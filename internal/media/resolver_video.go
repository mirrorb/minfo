// Package media 提供视频文件搜索和截图源回退选择辅助函数。

package media

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// isVideoFile 判断路径是否属于当前支持的视频扩展名。
func isVideoFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m2ts", ".mts", ".mkv", ".mp4", ".m4v", ".mov", ".avi", ".wmv", ".flv", ".mpg", ".mpeg", ".m2v", ".ts", ".vob", ".webm":
		return true
	default:
		return false
	}
}

// findVideoFile 优先在当前目录选择体积最大的直接子视频文件；找不到时递归回退。
func findVideoFile(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}

	var bestPath string
	var bestSize int64
	for _, entry := range entries {
		if entry.IsDir() || !isVideoFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			bestPath = filepath.Join(root, entry.Name())
		}
	}
	if bestPath != "" {
		return bestPath, nil
	}
	return findLargestVideoFile(root)
}

// findVideoCandidates 在目录树中找出体积最大的若干视频文件，供 MediaInfo 依次重试。
func findVideoCandidates(root string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 1
	}

	items := make([]videoCandidate, 0, 16)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isVideoFile(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		items = append(items, videoCandidate{path: path, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w under directory: %s", errNoVideo, root)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].size != items[j].size {
			return items[i].size > items[j].size
		}
		return items[i].path < items[j].path
	})
	if limit > len(items) {
		limit = len(items)
	}

	results := make([]string, 0, limit)
	for index := 0; index < limit; index++ {
		results = append(results, items[index].path)
	}
	return results, nil
}

// resolveScreenshotSourceFromRoot 从目录根路径推断截图流程应该使用的视频文件。
func resolveScreenshotSourceFromRoot(root string) (string, error) {
	if bdmvRoot, ok := resolveBDMVRoot(root); ok {
		return findLargestM2TS(bdmvRoot)
	}
	if dvdRoot, ok := resolveDVDVideoRoot(root); ok {
		if titleVOB, err := findMainDVDTitleSetFirstVOB(dvdRoot); err == nil {
			return titleVOB, nil
		}
		return findVideoFile(dvdRoot)
	}
	return findVideoFile(root)
}
