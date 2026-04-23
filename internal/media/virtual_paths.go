// Package media 处理媒体输入路径和虚拟 ISO 路径的解析与构造。

package media

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const virtualISOPrefix = "ISO:"

// ResolveInputPath 解析用户输入的媒体路径；它支持普通路径和 ISO 虚拟路径。
func ResolveInputPath(ctx context.Context, input string) (string, func(), error) {
	cleaned := strings.TrimSpace(strings.Trim(input, "\""))
	if cleaned == "" {
		return "", func() {}, fmt.Errorf("missing path")
	}

	if isVirtualISOPath(cleaned) {
		return resolveVirtualISOPath(ctx, cleaned)
	}

	cleaned = filepath.Clean(cleaned)
	if _, err := os.Stat(cleaned); err != nil {
		return "", func() {}, fmt.Errorf("path not found: %v", err)
	}
	return cleaned, func() {}, nil
}

// isVirtualISOPath 会判断虚拟 ISO 路径是否满足当前条件。
func isVirtualISOPath(input string) bool {
	_, _, ok := parseVirtualISOPath(input)
	return ok
}

// parseVirtualISOPath 解析形如 ISO:/path/to.iso!/BDMV 的虚拟 ISO 路径。
func parseVirtualISOPath(input string) (string, string, bool) {
	if !strings.HasPrefix(input, virtualISOPrefix) {
		return "", "", false
	}

	rest := strings.TrimPrefix(input, virtualISOPrefix)
	if rest == "" {
		return "", "", false
	}

	var isoPath string
	inner := "/"
	if strings.HasSuffix(rest, "!") {
		isoPath = rest[:len(rest)-1]
	} else {
		bang := strings.Index(rest, "!/")
		if bang < 0 {
			return "", "", false
		}
		isoPath = rest[:bang]
		inner = rest[bang+1:]
	}

	isoPath = filepath.Clean(strings.TrimSpace(isoPath))
	if isoPath == "." || isoPath == "" || !isISOFile(isoPath) {
		return "", "", false
	}

	inner = strings.ReplaceAll(strings.TrimSpace(inner), "\\", "/")
	if inner == "" {
		inner = "/"
	}
	if !strings.HasPrefix(inner, "/") {
		inner = "/" + inner
	}
	inner = path.Clean(inner)
	if inner == "." {
		inner = "/"
	}
	if !strings.HasPrefix(inner, "/") {
		return "", "", false
	}

	return isoPath, inner, true
}

// buildVirtualISOPath 根据 ISO 文件路径和内部路径重新拼出虚拟 ISO 路径。
func buildVirtualISOPath(isoPath, inner string, isDir bool) string {
	cleanISO := filepath.Clean(isoPath)
	cleanInner := strings.ReplaceAll(strings.TrimSpace(inner), "\\", "/")
	if cleanInner == "" {
		cleanInner = "/"
	}
	if !strings.HasPrefix(cleanInner, "/") {
		cleanInner = "/" + cleanInner
	}
	cleanInner = path.Clean(cleanInner)
	if cleanInner == "." {
		cleanInner = "/"
	}

	result := virtualISOPrefix + cleanISO + "!"
	if cleanInner != "/" {
		result += cleanInner
	}
	if isDir && !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result
}

// resolveVirtualISOPath 挂载 ISO 并把虚拟路径解析成容器内可访问的实际路径。
func resolveVirtualISOPath(ctx context.Context, input string) (string, func(), error) {
	isoPath, inner, ok := parseVirtualISOPath(input)
	if !ok {
		return "", func() {}, fmt.Errorf("invalid ISO browser path")
	}
	if _, err := os.Stat(isoPath); err != nil {
		return "", func() {}, fmt.Errorf("path not found: %v", err)
	}

	mountDir, cleanup, err := mountISO(ctx, isoPath)
	if err != nil {
		return "", func() {}, err
	}

	target := mountDir
	if inner != "/" {
		target = filepath.Join(mountDir, filepath.FromSlash(strings.TrimPrefix(inner, "/")))
	}
	target = filepath.Clean(target)
	if !isSubpath(mountDir, target) {
		cleanup()
		return "", func() {}, fmt.Errorf("path is outside mounted ISO")
	}
	if _, err := os.Stat(target); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("path not found: %v", err)
	}

	return target, cleanup, nil
}
