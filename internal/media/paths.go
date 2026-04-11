// Package media 提供媒体根目录解析和路径联想逻辑。

package media

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// SuggestedPath 表示路径联想结果中的一个候选项。
type SuggestedPath struct {
	Path     string
	IsDir    bool
	Size     int64
	Duration string
}

// SuggestPaths 根据前缀在媒体根目录或 ISO 虚拟目录中生成路径联想结果。
func SuggestPaths(roots []string, prefix string, limit int) ([]SuggestedPath, string, error) {
	if len(roots) == 0 {
		return nil, "", errors.New("no MEDIA_ROOT configured")
	}

	if isVirtualISOPath(prefix) {
		return suggestVirtualISOPaths(roots, prefix, limit)
	}

	if prefix == "" {
		if len(roots) == 1 {
			items, err := listDir(roots[0], "", limit)
			return items, roots[0], err
		}
		items := make([]SuggestedPath, 0, len(roots))
		for _, root := range roots {
			items = append(items, SuggestedPath{
				Path:  withDirSuffix(root),
				IsDir: true,
			})
		}
		return items, "", nil
	}

	cleaned := filepath.Clean(prefix)
	selectedRoot := ""
	var absPrefix string
	if filepath.IsAbs(cleaned) {
		var ok bool
		absPrefix = cleaned
		selectedRoot, ok = findContainingRoot(roots, absPrefix)
		if !ok {
			return nil, "", errors.New("path is outside MEDIA_ROOTS")
		}
	} else {
		if len(roots) != 1 {
			return nil, "", errors.New("relative path requires a single MEDIA_ROOT")
		}
		selectedRoot = roots[0]
		absPrefix = filepath.Join(selectedRoot, cleaned)
	}

	sep := string(filepath.Separator)
	if strings.HasSuffix(prefix, sep) || strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, "\\") {
		if !isSubpath(selectedRoot, absPrefix) {
			return nil, "", errors.New("path is outside MEDIA_ROOTS")
		}
		items, err := listDir(absPrefix, "", limit)
		return items, selectedRoot, err
	}

	dir := filepath.Dir(absPrefix)
	base := filepath.Base(absPrefix)
	if !isSubpath(selectedRoot, dir) {
		return nil, "", errors.New("path is outside MEDIA_ROOTS")
	}
	items, err := listDir(dir, base, limit)
	return items, selectedRoot, err
}

// ResolveRoots 过滤无效或重复的媒体根目录，并返回排序后的可用列表。
func ResolveRoots(roots []string) ([]string, error) {
	resolved := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		resolved = append(resolved, root)
	}
	if len(resolved) == 0 {
		return nil, errors.New("no MEDIA_ROOT configured")
	}
	sort.Strings(resolved)
	return resolved, nil
}

// findContainingRoot 返回包含给定绝对路径的媒体根目录。
func findContainingRoot(roots []string, path string) (string, bool) {
	for _, root := range roots {
		if isSubpath(root, path) {
			return root, true
		}
	}
	return "", false
}

// withDirSuffix 为目录路径补上平台相关的尾部分隔符。
func withDirSuffix(path string) string {
	if strings.HasSuffix(path, string(filepath.Separator)) {
		return path
	}
	return path + string(filepath.Separator)
}

// suggestVirtualISOPaths 在挂载后的 ISO 目录中执行路径联想，并把结果重新映射回虚拟 ISO 路径。
func suggestVirtualISOPaths(roots []string, prefix string, limit int) ([]SuggestedPath, string, error) {
	isoPath, inner, ok := parseVirtualISOPath(prefix)
	if !ok {
		return nil, "", errors.New("invalid ISO browser path")
	}

	selectedRoot, ok := findContainingRoot(roots, isoPath)
	if !ok {
		return nil, "", errors.New("path is outside MEDIA_ROOTS")
	}

	mountDir, cleanup, err := mountISO(context.Background(), isoPath)
	if err != nil {
		return nil, "", err
	}
	defer cleanup()

	dirInner := inner
	base := ""
	if !hasDirectorySuffix(prefix) {
		dirInner = path.Dir(inner)
		base = path.Base(inner)
		if dirInner == "." {
			dirInner = "/"
		}
		if base == "." || base == "/" {
			base = ""
		}
	}

	dirOnDisk := mountDir
	if dirInner != "/" {
		dirOnDisk = filepath.Join(mountDir, filepath.FromSlash(strings.TrimPrefix(dirInner, "/")))
	}
	dirOnDisk = filepath.Clean(dirOnDisk)
	if !isSubpath(mountDir, dirOnDisk) {
		return nil, "", errors.New("path is outside mounted ISO")
	}

	entries, err := os.ReadDir(dirOnDisk)
	if err != nil {
		return nil, "", err
	}

	items := make([]SuggestedPath, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if base != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}

		entryInner := path.Join(dirInner, name)
		if !strings.HasPrefix(entryInner, "/") {
			entryInner = "/" + entryInner
		}
		item := SuggestedPath{
			Path:  buildVirtualISOPath(isoPath, entryInner, entry.IsDir()),
			IsDir: entry.IsDir(),
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return nil, "", err
			}
			item.Size = info.Size()
			item.Duration = readSuggestedPathDuration(filepath.Join(dirOnDisk, name))
		}
		items = append(items, item)
		if limit > 0 && len(items) >= limit {
			break
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})
	return items, selectedRoot, nil
}

// listDir 会列出目录，并按当前规则返回排序后的结果列表。
func listDir(dir, base string, limit int) ([]SuggestedPath, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	items := make([]SuggestedPath, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if base != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(base)) {
			continue
		}
		full := filepath.Join(dir, name)
		item := SuggestedPath{
			Path:  full,
			IsDir: entry.IsDir(),
		}
		if entry.IsDir() {
			item.Path = withDirSuffix(full)
		} else {
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			item.Size = info.Size()
			item.Duration = readSuggestedPathDuration(full)
		}
		items = append(items, item)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})
	return items, nil
}

// readSuggestedPathDuration 在候选路径为 MPLS 文件时返回格式化时长。
func readSuggestedPathDuration(path string) string {
	if !isMPLSFile(path) {
		return ""
	}
	duration, err := readMPLSDuration(path)
	if err != nil {
		return ""
	}
	return formatMPLSDuration(duration)
}

// hasDirectorySuffix 会判断DirectorySuffix是否已经存在或具备。
func hasDirectorySuffix(value string) bool {
	return strings.HasSuffix(value, string(filepath.Separator)) || strings.HasSuffix(value, "/") || strings.HasSuffix(value, "\\")
}

// isSubpath 会判断Subpath是否满足当前条件。
func isSubpath(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
