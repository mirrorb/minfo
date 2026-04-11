// Package media 负责把输入路径解析成截图、BDInfo 和 MediaInfo 所需的实际源。

package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var errNoISO = errors.New("no iso found")
var errISOFound = errors.New("iso found")
var errNoVideo = errors.New("no video files found")

const mediaInfoCandidateLimit = 5

// MediaInfoCandidateLimit 限制 MediaInfo 自动重试时最多返回的候选媒体数量。
const MediaInfoCandidateLimit = mediaInfoCandidateLimit

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

// resolveDVDMediaInfoFileFromRoot 从 DVD 目录中挑选最适合交给 MediaInfo 的 IFO 或 VOB 文件。
func resolveDVDMediaInfoFileFromRoot(root string) (string, error) {
	dvdRoot, ok := resolveDVDVideoRoot(root)
	if !ok {
		return "", errors.New("VIDEO_TS folder not found")
	}

	titleVOB, err := findMainDVDTitleSetFirstVOB(dvdRoot)
	if err == nil {
		ifoPath := dvdControlIFOPathFromTitleVOB(titleVOB)
		if ifoPath != "" {
			if info, statErr := os.Stat(ifoPath); statErr == nil && !info.IsDir() {
				return ifoPath, nil
			}
		}
		return titleVOB, nil
	}

	videoTSIFO := filepath.Join(dvdRoot, "VIDEO_TS.IFO")
	if info, statErr := os.Stat(videoTSIFO); statErr == nil && !info.IsDir() {
		return videoTSIFO, nil
	}
	return "", err
}

// findMainDVDTitleSetFirstVOB 选择主标题集对应的首个 VOB 文件。
func findMainDVDTitleSetFirstVOB(videoTSDir string) (string, error) {
	entries, err := os.ReadDir(videoTSDir)
	if err != nil {
		return "", err
	}

	type dvdTitleVOB struct {
		titleSet int
		part     int
		size     int64
		path     string
	}

	items := make([]dvdTitleVOB, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isDVDTitleVOBName(entry.Name()) {
			continue
		}
		titleSet, part, ok := parseDVDTitleVOBName(entry.Name())
		if !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		items = append(items, dvdTitleVOB{
			titleSet: titleSet,
			part:     part,
			size:     info.Size(),
			path:     filepath.Join(videoTSDir, entry.Name()),
		})
	}
	if len(items) == 0 {
		return "", errors.New("no DVD title VOB files found under VIDEO_TS")
	}

	titleSetSizes := make(map[int]int64, len(items))
	for _, item := range items {
		titleSetSizes[item.titleSet] += item.size
	}

	sort.Slice(items, func(i, j int) bool {
		leftTotal := titleSetSizes[items[i].titleSet]
		rightTotal := titleSetSizes[items[j].titleSet]
		if leftTotal != rightTotal {
			return leftTotal > rightTotal
		}
		if items[i].titleSet != items[j].titleSet {
			return items[i].titleSet < items[j].titleSet
		}
		if items[i].part != items[j].part {
			return items[i].part < items[j].part
		}
		return items[i].path < items[j].path
	})

	mainTitleSet := items[0].titleSet
	best := items[0]
	for _, item := range items[1:] {
		if item.titleSet != mainTitleSet {
			break
		}
		if item.part < best.part || (item.part == best.part && item.path < best.path) {
			best = item
		}
	}
	return best.path, nil
}

// dvdControlIFOPathFromTitleVOB 根据标题 VOB 路径推导对应的控制 IFO 路径。
func dvdControlIFOPathFromTitleVOB(path string) string {
	base := filepath.Base(path)
	if len(base) < len("VTS_00_1.VOB") {
		return ""
	}
	if !isDVDTitleVOBName(base) {
		return ""
	}
	return filepath.Join(filepath.Dir(path), base[:7]+"0.IFO")
}

// dvdTitleVOBPathFromControlFile 根据 DVD 控制文件路径推导对应的首个标题 VOB。
func dvdTitleVOBPathFromControlFile(path string) string {
	base := strings.ToUpper(filepath.Base(path))
	if len(base) < len("VTS_00_0.IFO") {
		return ""
	}
	if !isDVDTitleControlFileName(base) {
		return ""
	}

	vobPath := filepath.Join(filepath.Dir(path), base[:7]+"1.VOB")
	info, err := os.Stat(vobPath)
	if err != nil || info.IsDir() {
		return ""
	}
	return vobPath
}

// isDVDTitleControlFileName 会判断DVD标题Control文件名称是否满足当前条件。
func isDVDTitleControlFileName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	return len(name) == len("VTS_00_0.IFO") &&
		strings.HasPrefix(name, "VTS_") &&
		name[6] == '_' &&
		name[7] == '0' &&
		name[8] == '.' &&
		name[4] >= '0' && name[4] <= '9' &&
		name[5] >= '0' && name[5] <= '9' &&
		(strings.HasSuffix(name, ".IFO") || strings.HasSuffix(name, ".BUP"))
}

// isDVDTitleVOBName 会判断DVD标题VOB名称是否满足当前条件。
func isDVDTitleVOBName(name string) bool {
	name = strings.ToUpper(strings.TrimSpace(name))
	return len(name) == len("VTS_00_1.VOB") &&
		strings.HasPrefix(name, "VTS_") &&
		name[6] == '_' &&
		name[8] == '.' &&
		name[7] >= '1' && name[7] <= '9' &&
		name[4] >= '0' && name[4] <= '9' &&
		name[5] >= '0' && name[5] <= '9' &&
		strings.HasSuffix(name, ".VOB")
}

// parseDVDTitleVOBName 会解析DVD标题VOB名称，并把原始输入转换成结构化结果。
func parseDVDTitleVOBName(name string) (int, int, bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if !isDVDTitleVOBName(name) {
		return 0, 0, false
	}

	titleSet := int(name[4]-'0')*10 + int(name[5]-'0')
	part := int(name[7] - '0')
	return titleSet, part, true
}

// findLargestVideoFile 递归查找目录树中体积最大的受支持视频文件。
func findLargestVideoFile(root string) (string, error) {
	var largestPath string
	var largestSize int64
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
		if info.Size() > largestSize {
			largestSize = info.Size()
			largestPath = path
		}
		return nil
	}); err != nil {
		return "", err
	}
	if largestPath == "" {
		return "", fmt.Errorf("%w under directory: %s", errNoVideo, root)
	}
	return largestPath, nil
}
