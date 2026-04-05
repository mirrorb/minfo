// Package screenshot 提供 DVD MediaInfo 字幕元数据解析逻辑。

package screenshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"minfo/internal/system"
)

type dvdMediaInfoTrack struct {
	StreamID int
	ID       string
	Format   string
	Language string
	Title    string
	Source   string
}

type dvdMediaInfoResult struct {
	Duration             float64
	Tracks               []dvdMediaInfoTrack
	ProbePath            string
	SelectedVOBPath      string
	LanguageFallbackPath string
}

type mediaInfoPayload struct {
	Media struct {
		Track []map[string]interface{} `json:"track"`
	} `json:"media"`
}

var mediaInfoHexIDPattern = regexp.MustCompile(`(?i)\(0x([0-9a-f]+)\)`)

// probeDVDMediaInfo 使用 mediainfo 提取 DVD 字幕元数据；必要时会从 BUP 结果补齐缺失语言。
func probeDVDMediaInfo(ctx context.Context, mediainfoBin, path, probePath string) (dvdMediaInfoResult, error) {
	if strings.TrimSpace(mediainfoBin) == "" {
		return dvdMediaInfoResult{}, fmt.Errorf("mediainfo not available")
	}

	selectedVOBPath := resolveDVDMediaInfoVOBPath(path, probePath)
	primaryPath := resolveDVDMediaInfoProbePath(path, probePath)
	result, err := probeDVDMediaInfoOnce(ctx, mediainfoBin, primaryPath)
	if err != nil {
		return dvdMediaInfoResult{}, err
	}
	result.ProbePath = primaryPath
	result.SelectedVOBPath = selectedVOBPath

	if dvdMediaInfoNeedsLanguageFallback(result) {
		if bupPath, ok := dvdMediaInfoBUPPath(primaryPath); ok {
			fallback, fallbackErr := probeDVDMediaInfoOnce(ctx, mediainfoBin, bupPath)
			if fallbackErr == nil {
				merged, used := mergeDVDMediaInfoLanguageFallback(result, fallback)
				if used {
					merged.ProbePath = result.ProbePath
					merged.SelectedVOBPath = result.SelectedVOBPath
					merged.LanguageFallbackPath = bupPath
					result = merged
				}
			}
		}
	}
	return result, nil
}

// probeDVDMediaInfoOnce 运行一次 mediainfo JSON 探测，并解析出字幕轨和时长信息。
func probeDVDMediaInfoOnce(ctx context.Context, mediainfoBin, path string) (dvdMediaInfoResult, error) {
	stdout, stderr, err := system.RunCommand(ctx, mediainfoBin, "--Output=JSON", path)
	if err != nil {
		return dvdMediaInfoResult{}, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return dvdMediaInfoResult{}, fmt.Errorf("mediainfo returned empty output")
	}

	payloads, err := decodeMediaInfoPayloads([]byte(stdout))
	if err != nil {
		return dvdMediaInfoResult{}, err
	}

	result := dvdMediaInfoResult{
		Tracks: make([]dvdMediaInfoTrack, 0, 8),
	}

	for _, payload := range payloads {
		for _, track := range payload.Media.Track {
			trackType := strings.TrimSpace(mediaInfoTrackString(track, "@type"))
			switch strings.ToLower(trackType) {
			case "general":
				if value, ok := parseMediaInfoTrackDuration(mediaInfoTrackString(track, "Duration")); ok && value > 0 && value > result.Duration {
					result.Duration = value
				}
			case "text":
				streamID, _ := parseMediaInfoStreamID(mediaInfoTrackString(track, "ID"))
				result.Tracks = append(result.Tracks, dvdMediaInfoTrack{
					StreamID: streamID,
					ID:       mediaInfoTrackString(track, "ID"),
					Format:   mediaInfoTrackString(track, "Format", "Format/String"),
					Language: mediaInfoTrackString(track, "Language", "Language/String"),
					Title:    mediaInfoTrackString(track, "Title", "Title/String"),
					Source:   mediaInfoTrackString(track, "Source"),
				})
			}
		}
	}

	sort.Slice(result.Tracks, func(i, j int) bool {
		if result.Tracks[i].StreamID != result.Tracks[j].StreamID {
			return result.Tracks[i].StreamID < result.Tracks[j].StreamID
		}
		return result.Tracks[i].ID < result.Tracks[j].ID
	})
	return result, nil
}

// resolveDVDMediaInfoProbePath 选择实际交给 mediainfo 的探测路径；优先使用能映射到 IFO 的路径。
func resolveDVDMediaInfoProbePath(path, probePath string) string {
	for _, candidate := range []string{probePath, path} {
		if resolved, ok := dvdMediaInfoIFOPath(candidate); ok {
			return resolved
		}
	}
	return strings.TrimSpace(path)
}

// resolveDVDMediaInfoVOBPath 返回与当前探测路径对应的标题 VOB 路径。
func resolveDVDMediaInfoVOBPath(path, probePath string) string {
	for _, candidate := range []string{probePath, path} {
		if resolved, ok := dvdMediaInfoTitleVOBPath(candidate); ok {
			return resolved
		}
	}
	return ""
}

// dvdMediaInfoIFOPath 把 DVD 相关路径归一化为对应的 IFO 文件路径。
func dvdMediaInfoIFOPath(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", false
	}

	upperBase := strings.ToUpper(filepath.Base(cleaned))
	switch filepath.Ext(upperBase) {
	case ".IFO":
		if fileExists(cleaned) {
			return cleaned, true
		}
	case ".BUP":
		ifoPath := strings.TrimSuffix(cleaned, filepath.Ext(cleaned)) + ".IFO"
		if fileExists(ifoPath) {
			return ifoPath, true
		}
	case ".VOB":
		if strings.EqualFold(upperBase, "VIDEO_TS.VOB") {
			ifoPath := filepath.Join(filepath.Dir(cleaned), "VIDEO_TS.IFO")
			if fileExists(ifoPath) {
				return ifoPath, true
			}
			return "", false
		}
		if len(upperBase) == len("VTS_00_1.VOB") &&
			strings.HasPrefix(upperBase, "VTS_") &&
			upperBase[6] == '_' &&
			upperBase[8:] == ".VOB" &&
			upperBase[7] >= '1' && upperBase[7] <= '9' {
			ifoPath := filepath.Join(filepath.Dir(cleaned), upperBase[:7]+"0.IFO")
			if fileExists(ifoPath) {
				return ifoPath, true
			}
		}
	}
	return "", false
}

// dvdMediaInfoTitleVOBPath 把 DVD 相关路径归一化为对应的首个标题 VOB 路径。
func dvdMediaInfoTitleVOBPath(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", false
	}

	upperBase := strings.ToUpper(filepath.Base(cleaned))
	switch filepath.Ext(upperBase) {
	case ".VOB":
		if fileExists(cleaned) && looksLikeDVDSource(cleaned) {
			return cleaned, true
		}
	case ".IFO", ".BUP":
		if strings.EqualFold(upperBase, "VIDEO_TS.IFO") || strings.EqualFold(upperBase, "VIDEO_TS.BUP") {
			return "", false
		}
		if len(upperBase) == len("VTS_00_0.IFO") &&
			strings.HasPrefix(upperBase, "VTS_") &&
			upperBase[6] == '_' &&
			upperBase[7] == '0' {
			vobPath := filepath.Join(filepath.Dir(cleaned), upperBase[:7]+"1.VOB")
			if fileExists(vobPath) {
				return vobPath, true
			}
		}
	}
	return "", false
}

// dvdMediaInfoBUPPath 根据 IFO 路径返回对应的 BUP 路径。
func dvdMediaInfoBUPPath(path string) (string, bool) {
	cleaned := strings.TrimSpace(path)
	if !strings.EqualFold(filepath.Ext(cleaned), ".ifo") {
		return "", false
	}
	bupPath := strings.TrimSuffix(cleaned, filepath.Ext(cleaned)) + ".BUP"
	if !fileExists(bupPath) {
		return "", false
	}
	return bupPath, true
}

// dvdMediaInfoNeedsLanguageFallback 判断结果中是否仍有字幕轨缺少可用语言信息。
func dvdMediaInfoNeedsLanguageFallback(result dvdMediaInfoResult) bool {
	if len(result.Tracks) == 0 {
		return false
	}
	for _, track := range result.Tracks {
		if !dvdMediaInfoHasLanguage(track.Language) {
			return true
		}
	}
	return false
}

// mergeDVDMediaInfoLanguageFallback 会合并DVD媒体Info语言回退，并保留后续流程仍然需要的有效信息。
func mergeDVDMediaInfoLanguageFallback(primary, fallback dvdMediaInfoResult) (dvdMediaInfoResult, bool) {
	if len(primary.Tracks) == 0 || len(fallback.Tracks) == 0 {
		return primary, false
	}

	merged := primary
	merged.Tracks = append([]dvdMediaInfoTrack(nil), primary.Tracks...)

	fallbackByStreamID := make(map[int][]dvdMediaInfoTrack, len(fallback.Tracks))
	fallbackIndexByStreamID := make(map[int][]int, len(fallback.Tracks))
	fallbackOrdered := make([]dvdMediaInfoTrack, 0, len(fallback.Tracks))
	for _, track := range fallback.Tracks {
		if !dvdMediaInfoHasLanguage(track.Language) {
			continue
		}
		fallbackOrdered = append(fallbackOrdered, track)
		orderedIndex := len(fallbackOrdered) - 1
		if track.StreamID > 0 {
			fallbackByStreamID[track.StreamID] = append(fallbackByStreamID[track.StreamID], track)
			fallbackIndexByStreamID[track.StreamID] = append(fallbackIndexByStreamID[track.StreamID], orderedIndex)
		}
	}
	if len(fallbackOrdered) == 0 {
		return primary, false
	}

	usedOrdered := make([]bool, len(fallbackOrdered))
	used := false
	for index := range merged.Tracks {
		if dvdMediaInfoHasLanguage(merged.Tracks[index].Language) {
			continue
		}

		if mergedTrack, ok := fillDVDMediaInfoLanguageByStreamID(merged.Tracks[index], fallbackByStreamID, fallbackIndexByStreamID, usedOrdered); ok {
			merged.Tracks[index] = mergedTrack
			used = true
			continue
		}

		for fallbackIndex, candidate := range fallbackOrdered {
			if usedOrdered[fallbackIndex] {
				continue
			}
			merged.Tracks[index] = mergeDVDMediaInfoTrack(merged.Tracks[index], candidate)
			usedOrdered[fallbackIndex] = true
			used = true
			break
		}
	}

	if merged.Duration <= 0 && fallback.Duration > 0 {
		merged.Duration = fallback.Duration
	}
	return merged, used
}

// fillDVDMediaInfoLanguageByStreamID 尝试按 StreamID 从回退结果中补齐单条字幕轨的语言信息。
func fillDVDMediaInfoLanguageByStreamID(track dvdMediaInfoTrack, fallbackByStreamID map[int][]dvdMediaInfoTrack, fallbackIndexByStreamID map[int][]int, usedOrdered []bool) (dvdMediaInfoTrack, bool) {
	if track.StreamID <= 0 {
		return track, false
	}
	candidates := fallbackByStreamID[track.StreamID]
	candidateIndexes := fallbackIndexByStreamID[track.StreamID]
	if len(candidates) == 0 || len(candidateIndexes) == 0 {
		return track, false
	}
	for index, candidate := range candidates {
		if index >= len(candidateIndexes) {
			break
		}
		orderedIndex := candidateIndexes[index]
		if orderedIndex < 0 || orderedIndex >= len(usedOrdered) || usedOrdered[orderedIndex] {
			continue
		}
		usedOrdered[orderedIndex] = true
		return mergeDVDMediaInfoTrack(track, candidate), true
	}
	return track, false
}

// mergeDVDMediaInfoTrack 会合并DVD媒体Info轨道，并保留后续流程仍然需要的有效信息。
func mergeDVDMediaInfoTrack(track, fallback dvdMediaInfoTrack) dvdMediaInfoTrack {
	if !dvdMediaInfoHasLanguage(track.Language) && dvdMediaInfoHasLanguage(fallback.Language) {
		track.Language = fallback.Language
		track.Source = fallback.Source
	}
	if strings.TrimSpace(track.Title) == "" && strings.TrimSpace(fallback.Title) != "" {
		track.Title = strings.TrimSpace(fallback.Title)
	}
	if strings.TrimSpace(track.Format) == "" && strings.TrimSpace(fallback.Format) != "" {
		track.Format = strings.TrimSpace(fallback.Format)
	}
	return track
}

// dvdMediaInfoHasLanguage 判断语言字段是否包含有效值。
func dvdMediaInfoHasLanguage(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "unknown", "und", "undefined", "null", "n/a", "na":
		return false
	default:
		return true
	}
}

// fileExists 判断路径是否存在且为普通文件。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// decodeMediaInfoPayloads 兼容 mediainfo 输出的单对象或对象数组 JSON 结构。
func decodeMediaInfoPayloads(data []byte) ([]mediaInfoPayload, error) {
	var single mediaInfoPayload
	if err := json.Unmarshal(data, &single); err == nil {
		return []mediaInfoPayload{single}, nil
	}

	var multiple []mediaInfoPayload
	if err := json.Unmarshal(data, &multiple); err == nil {
		return multiple, nil
	}

	return nil, fmt.Errorf("unsupported mediainfo JSON shape")
}

// mediaInfoTrackString 按候选键顺序读取轨道字段，并返回第一个非空字符串值。
func mediaInfoTrackString(track map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(jsonString(track[key]))
		if value != "" {
			return value
		}
	}
	return ""
}

// parseMediaInfoStreamID 会解析媒体Info流ID，并把原始输入转换成结构化结果。
func parseMediaInfoStreamID(raw string) (int, bool) {
	matches := mediaInfoHexIDPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		if len(last) >= 2 {
			value, err := strconv.ParseInt(last[1], 16, 64)
			if err == nil {
				return int(value), true
			}
		}
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if value, err := strconv.Atoi(raw); err == nil {
		return value, true
	}
	return 0, false
}

// parseMediaInfoTrackDuration 会解析媒体Info轨道时长，并把原始输入转换成结构化结果。
func parseMediaInfoTrackDuration(raw string) (float64, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed / 1000.0, true
}

// resolveDVDMediaInfoTracks 将 MediaInfo 的 DVD 字幕轨尽量映射回 ffprobe 的原始字幕 PID。
func resolveDVDMediaInfoTracks(raw []subtitleTrack, tracks []dvdMediaInfoTrack) map[int]dvdMediaInfoTrack {
	resolved := make(map[int]dvdMediaInfoTrack, len(tracks))
	if len(raw) == 0 || len(tracks) == 0 {
		return resolved
	}

	rawPIDSet := make(map[int]struct{}, len(raw))
	for _, track := range raw {
		pid, ok := normalizeStreamPID(track.StreamID)
		if !ok {
			continue
		}
		rawPIDSet[pid] = struct{}{}
	}

	exactMatched := false
	for _, item := range tracks {
		if item.StreamID <= 0 {
			continue
		}
		if _, ok := rawPIDSet[item.StreamID]; ok {
			resolved[item.StreamID] = item
			exactMatched = true
		}
	}
	if exactMatched {
		return resolved
	}

	type rawTrackPID struct {
		pid int
	}
	rawPIDs := make([]rawTrackPID, 0, len(raw))
	for _, track := range raw {
		if strings.ToLower(strings.TrimSpace(track.Codec)) != "dvd_subtitle" {
			continue
		}
		pid, ok := normalizeStreamPID(track.StreamID)
		if !ok {
			continue
		}
		rawPIDs = append(rawPIDs, rawTrackPID{pid: pid})
	}
	sort.Slice(rawPIDs, func(i, j int) bool {
		return rawPIDs[i].pid < rawPIDs[j].pid
	})

	mediaInfoCopy := append([]dvdMediaInfoTrack(nil), tracks...)
	sort.Slice(mediaInfoCopy, func(i, j int) bool {
		if mediaInfoCopy[i].StreamID != mediaInfoCopy[j].StreamID {
			return mediaInfoCopy[i].StreamID < mediaInfoCopy[j].StreamID
		}
		return mediaInfoCopy[i].ID < mediaInfoCopy[j].ID
	})

	limit := len(mediaInfoCopy)
	if len(rawPIDs) < limit {
		limit = len(rawPIDs)
	}
	for index := 0; index < limit; index++ {
		resolved[rawPIDs[index].pid] = mediaInfoCopy[index]
	}
	return resolved
}
