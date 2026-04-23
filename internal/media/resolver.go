// Package media 负责把输入路径解析成截图、BDInfo 和 MediaInfo 所需的实际源。

package media

import (
	"context"
	"errors"
	"os"
)

var errNoISO = errors.New("no iso found")
var errISOFound = errors.New("iso found")
var errNoVideo = errors.New("no video files found")

const mediaInfoCandidateLimit = 5

// MediaInfoCandidateLimit 限制 MediaInfo 自动重试时最多返回的候选媒体数量。
const MediaInfoCandidateLimit = mediaInfoCandidateLimit

// videoCandidate 表示一次视频候选搜索中记录的路径与文件大小。
type videoCandidate struct {
	path string
	size int64
}

// BDInfoSource 表示 BDInfo 实际扫描路径以及可选的指定 playlist。
type BDInfoSource struct {
	Path     string
	Playlist string
}

// ResolveScreenshotSource 把输入路径解析成可直接交给截图流程的实际视频文件，并返回可能需要的清理函数。
func ResolveScreenshotSource(ctx context.Context, input string) (string, func(), error) {
	info, err := os.Stat(input)
	if err != nil {
		return "", func() {}, err
	}
	if !info.IsDir() {
		if isISOFile(input) {
			return resolveVideoFromMountedISO(ctx, input)
		}
		if sourcePath, ok := resolveDVDFileScreenshotSource(input); ok {
			return sourcePath, func() {}, nil
		}
		return input, func() {}, nil
	}

	if bdmvRoot, ok := resolveBDMVRoot(input); ok {
		m2ts, err := findLargestM2TS(bdmvRoot)
		if err != nil {
			return "", func() {}, err
		}
		return m2ts, func() {}, nil
	}

	isoPath, err := findISOInDir(input)
	if err == nil {
		return resolveVideoFromMountedISO(ctx, isoPath)
	}
	if !errors.Is(err, errNoISO) {
		return "", func() {}, err
	}

	videoPath, err := resolveScreenshotSourceFromRoot(input)
	if err != nil {
		return "", func() {}, err
	}
	return videoPath, func() {}, nil
}

// ResolveMediaInfoCandidates 根据输入路径返回适合 MediaInfo 重试的候选文件列表。
func ResolveMediaInfoCandidates(ctx context.Context, input string, limit int) ([]string, func(), error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, func() {}, err
	}
	if !info.IsDir() {
		return []string{input}, func() {}, nil
	}

	if dvdRoot, ok := resolveDVDVideoRoot(input); ok {
		target, err := resolveDVDMediaInfoFileFromRoot(dvdRoot)
		if err != nil {
			return nil, func() {}, err
		}
		return []string{target}, func() {}, nil
	}

	isoPath, err := findISOInDir(input)
	if err == nil {
		return []string{isoPath}, func() {}, nil
	}
	if !errors.Is(err, errNoISO) {
		return nil, func() {}, err
	}

	candidates, err := findVideoCandidates(input, limit)
	if err != nil {
		return nil, func() {}, err
	}
	return candidates, func() {}, nil
}

// ResolveDVDMediaInfoSource 把输入路径解析成 DVD MediaInfo 应该探测的文件或 ISO 路径。
func ResolveDVDMediaInfoSource(ctx context.Context, input string) (string, func(), error) {
	info, err := os.Stat(input)
	if err != nil {
		return "", func() {}, err
	}

	if !info.IsDir() {
		if isISOFile(input) {
			return input, func() {}, nil
		}
		return input, func() {}, nil
	}

	if dvdRoot, ok := resolveDVDVideoRoot(input); ok {
		target, err := resolveDVDMediaInfoFileFromRoot(dvdRoot)
		if err != nil {
			return "", func() {}, err
		}
		return target, func() {}, nil
	}

	isoPath, err := findISOInDir(input)
	if err == nil {
		return isoPath, func() {}, nil
	}
	if !errors.Is(err, errNoISO) {
		return "", func() {}, err
	}

	return "", func() {}, errors.New("path does not contain DVD VIDEO_TS content")
}

// ResolveBDInfoSource 把输入路径解析成 BDInfo 可以扫描的目录，并在需要时附带指定 playlist。
func ResolveBDInfoSource(ctx context.Context, input string) (BDInfoSource, func(), error) {
	info, err := os.Stat(input)
	if err != nil {
		return BDInfoSource{}, func() {}, err
	}
	if !info.IsDir() {
		if isISOFile(input) {
			return resolveBDInfoFromMountedISO(ctx, input)
		}
		if root, playlist, ok := resolveBDInfoPlaylistSelection(input); ok {
			return BDInfoSource{Path: root, Playlist: playlist}, func() {}, nil
		}
		return BDInfoSource{}, func() {}, errors.New("path must be a folder containing BDMV or ISO, or a MPLS file under BDMV/PLAYLIST")
	}

	if bdmvRoot, ok := resolveBDInfoRoot(input); ok {
		return BDInfoSource{Path: bdmvRoot}, func() {}, nil
	}

	isoPath, err := findISOInDir(input)
	if err == nil {
		return resolveBDInfoFromMountedISO(ctx, isoPath)
	}
	if !errors.Is(err, errNoISO) {
		return BDInfoSource{}, func() {}, err
	}

	return BDInfoSource{}, func() {}, errors.New("path does not contain BDMV or BDISO content")
}
