// Package media 提供 DVD MediaInfo 源文件和标题集推导辅助函数。

package media

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
