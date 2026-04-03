package screenshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"minfo/internal/media"
	"minfo/internal/system"
)

const (
	nativeDefaultSubDur   = 4.0
	nativeSubSnapEpsilon  = 0.50
	nativePlaylistScanMax = 6
	nativeOversizeBytes   = 10 * 1024 * 1024
)

var (
	nativeLangZHHans = []string{"简体", "简中", "chs", "zh-hans", "zh_hans", "zh-cn", "zh_cn"}
	nativeLangZHHant = []string{"繁体", "繁中", "cht", "big5", "zh-hant", "zh_hant", "zh-tw", "zh_tw"}
	nativeLangZH     = []string{"中文", "chinese", "zho", "chi", "zh"}
	nativeLangEN     = []string{"en", "eng", "english"}

	nativeClipIDPattern = regexp.MustCompile(`[0-9]{5}M2TS`)
)

type nativeVariantSettings struct {
	Ext            string
	ProbeSize      string
	Analyze        string
	CoarseBackText int
	CoarseBackPGS  int
	SearchBack     float64
	SearchForward  float64
	JPGQuality     int
}

type nativeSubtitleSelection struct {
	Mode          string
	File          string
	StreamIndex   int
	RelativeIndex int
	Lang          string
	Codec         string
	Title         string
}

type nativeSubtitleSpan struct {
	Start float64
	End   float64
}

type nativeSubtitleTrack struct {
	Index     int
	StreamID  string
	Codec     string
	Language  string
	Title     string
	Forced    int
	IsDefault int
	Tags      string
}

type nativeHelperTrack struct {
	PID        int    `json:"pid"`
	Lang       string `json:"lang"`
	CodingType int    `json:"coding_type"`
	CharCode   int    `json:"char_code"`
	SubpathID  int    `json:"subpath_id"`
}

type nativeBlurayHelperResult struct {
	Source string `json:"source"`
	Clip   struct {
		ClipID        string              `json:"clip_id"`
		PGStreamCount int                 `json:"pg_stream_count"`
		PGStreams     []nativeHelperTrack `json:"pg_streams"`
	} `json:"clip"`
}

type nativeBlurayProbeContext struct {
	Root     string
	Playlist string
	Clip     string
}

type nativeFFprobeStreamsPayload struct {
	Streams []struct {
		Index       int                    `json:"index"`
		ID          interface{}            `json:"id"`
		CodecName   string                 `json:"codec_name"`
		Tags        map[string]interface{} `json:"tags"`
		Disposition struct {
			Default int `json:"default"`
			Forced  int `json:"forced"`
		} `json:"disposition"`
	} `json:"streams"`
}

type nativeFFprobePacketsPayload struct {
	Packets []nativeFFprobePacket `json:"packets"`
}

type nativeFFprobePacket struct {
	PTSTime      string `json:"pts_time"`
	DurationTime string `json:"duration_time"`
	Size         string `json:"size"`
}

type nativeScreenshotRunner struct {
	ctx              context.Context
	sourcePath       string
	dvdMediaInfoPath string
	outputDir        string
	variant          string
	subtitleMode     string
	requested        []float64
	settings         nativeVariantSettings
	ffmpegBin        string
	ffprobeBin       string
	mediainfoBin     string
	bdsubBin         string
	logLines         []string
	logHandler       LogHandler

	blurayContext            nativeBlurayProbeContext
	subtitle                 nativeSubtitleSelection
	subtitleIndex            []nativeSubtitleSpan
	rejectedBitmapCandidates map[string]struct{}

	startOffset float64
	duration    float64
	videoWidth  int
	videoHeight int
	colorInfo   string
	colorChain  string
}

func runNativeScreenshotsWithLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (ScriptResult, error) {
	sourcePath, cleanup, err := media.ResolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return ScriptResult{}, err
	}
	defer cleanup()

	dvdMediaInfoPath, dvdMediaInfoCleanup, dvdMediaInfoErr := media.ResolveDVDMediaInfoSource(ctx, inputPath)
	if dvdMediaInfoErr == nil {
		defer dvdMediaInfoCleanup()
	} else {
		dvdMediaInfoPath = ""
	}

	timestamps, err := randomScreenshotTimestampsForSource(ctx, sourcePath, count)
	if err != nil {
		return ScriptResult{}, err
	}

	return runNativeScreenshotsFromSource(ctx, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode, timestamps, onLog)
}

func runNativeScreenshotsFromSource(ctx context.Context, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode string, timestamps []string, onLog LogHandler) (ScriptResult, error) {
	runner := &nativeScreenshotRunner{
		ctx:              ctx,
		sourcePath:       sourcePath,
		dvdMediaInfoPath: dvdMediaInfoPath,
		outputDir:        outputDir,
		variant:          NormalizeVariant(variant),
		subtitleMode:     NormalizeSubtitleMode(subtitleMode),
		settings:         nativeVariantSettingsFor(variant),
		subtitle: nativeSubtitleSelection{
			Mode: "none",
		},
		logHandler: onLog,
	}

	runner.logf("[信息] 已切换为 Go 截图引擎。")
	if nativeLooksLikeDVDSource(runner.dvdProbeSource()) {
		runner.logf("[信息] DVD 已选片段：VOB=%s | IFO=%s",
			nativeDisplayProbeValue(runner.dvdSelectedVOBPath()),
			nativeDisplayProbeValue(runner.dvdSelectedIFOPath()),
		)
	}

	if err := runner.init(timestamps); err != nil {
		return ScriptResult{Logs: runner.logs()}, err
	}

	files, err := runner.run()
	if err != nil {
		return ScriptResult{Logs: runner.logs()}, err
	}
	return ScriptResult{Files: files, Logs: runner.logs()}, nil
}

func nativeVariantSettingsFor(variant string) nativeVariantSettings {
	switch NormalizeVariant(variant) {
	case VariantJPG:
		return nativeVariantSettings{
			Ext:            ".jpg",
			ProbeSize:      "100M",
			Analyze:        "100M",
			CoarseBackText: 2,
			CoarseBackPGS:  8,
			SearchBack:     4,
			SearchForward:  8,
			JPGQuality:     2,
		}
	default:
		return nativeVariantSettings{
			Ext:            ".png",
			ProbeSize:      "150M",
			Analyze:        "150M",
			CoarseBackText: 3,
			CoarseBackPGS:  12,
			SearchBack:     6,
			SearchForward:  10,
			JPGQuality:     85,
		}
	}
}

func (r *nativeScreenshotRunner) init(timestamps []string) error {
	var err error

	r.ffmpegBin, err = system.ResolveBin("FFMPEG_BIN", "ffmpeg")
	if err != nil {
		return err
	}
	r.ffprobeBin, err = system.ResolveBin("FFPROBE_BIN", "ffprobe")
	if err != nil {
		return err
	}
	if bin, binErr := system.ResolveBin("MEDIAINFO_BIN", "mediainfo"); binErr == nil {
		r.mediainfoBin = bin
	}
	if path, lookErr := exec.LookPath("bdsub"); lookErr == nil {
		r.bdsubBin = path
	}

	r.requested, err = nativeParseRequestedTimestamps(timestamps)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(r.outputDir, 0o755); err != nil {
		return err
	}
	if err := nativeClearDir(r.outputDir); err != nil {
		return err
	}

	if r.subtitleMode != SubtitleModeOff {
		r.prepareBlurayProbeContext()
	}
	r.chooseSubtitle()

	r.startOffset = r.detectStartOffset()
	r.duration, err = probeMediaDuration(r.ctx, r.ffprobeBin, r.sourcePath)
	if err != nil {
		return err
	}
	r.videoWidth, r.videoHeight = r.detectVideoDimensions()

	if r.variant == VariantPNG {
		r.colorInfo = r.detectColorspace()
		r.colorChain = nativeBuildColorspaceChain(r.colorInfo)
		if r.colorInfo != "" {
			r.logf("[信息] 检测到色彩空间：%s", strings.TrimSuffix(r.colorInfo, "|"))
		} else {
			r.logf("[信息] 无法检测色彩空间，将使用标准转换")
		}
	}

	r.logf("[信息] 容器起始偏移：%.3fs | 影片总时长：%s", r.startOffset, nativeSecToHMS(r.duration))
	return nil
}

func (r *nativeScreenshotRunner) run() ([]string, error) {
	successCount := 0
	failures := make([]string, 0)
	usedNames := make(map[string]int, len(r.requested))
	usedSeconds := make(map[int]struct{}, len(r.requested))

	for _, requested := range r.requested {
		aligned := requested
		if r.subtitle.Mode != "none" {
			aligned = r.alignToSubtitle(requested)
		}
		aligned = r.clampToDuration(aligned)
		if candidate, adjusted, ok := r.resolveUniqueScreenshotSecond(requested, aligned, usedSeconds); ok {
			if adjusted {
				r.logf("[提示] 请求 %s 对齐后命中已使用秒，改用唯一秒 %s",
					nativeSecToHMSMS(requested),
					nativeSecToHMSMS(candidate),
				)
			}
			aligned = candidate
		} else {
			r.logf("[提示] 请求 %s 对齐后未找到新的唯一秒，跳过该截图。", nativeSecToHMSMS(requested))
			continue
		}

		outputName := nativeUniqueScreenshotName(aligned, r.settings.Ext, usedNames)
		outputPath := filepath.Join(r.outputDir, outputName)
		r.logf("[信息] 截图: 请求 %s → 对齐 %s → 输出 %s -> %s",
			nativeSecToHMSMS(requested),
			nativeSecToHMSMS(aligned),
			nativeSecToHMSMS(aligned),
			outputName,
		)

		if err := r.captureScreenshot(aligned, outputPath); err != nil {
			failures = append(failures, fmt.Sprintf("[失败] 文件: %s\n原因: %s", filepath.Base(outputPath), err.Error()))
			continue
		}
		usedSeconds[nativeScreenshotSecond(aligned)] = struct{}{}
		successCount++
	}

	r.logf("")
	r.logf("===== 任务完成 =====")
	r.logf("成功: %d 张 | 失败: %d 张", successCount, len(failures))

	if len(failures) > 0 {
		r.logf("")
		r.logf("===== 失败详情 =====")
		for _, item := range failures {
			r.logf("%s", item)
		}
	}

	files, err := listScreenshotFiles(r.outputDir)
	if err != nil {
		if successCount == 0 {
			return nil, errors.New("no screenshots were generated")
		}
		return nil, err
	}
	return files, nil
}

func (r *nativeScreenshotRunner) resolveUniqueScreenshotSecond(requested, aligned float64, usedSeconds map[int]struct{}) (float64, bool, bool) {
	aligned = r.clampToDuration(aligned)
	second := nativeScreenshotSecond(aligned)
	if _, exists := usedSeconds[second]; !exists {
		return aligned, false, true
	}

	if r.subtitle.Mode != "none" {
		if len(r.subtitleIndex) == 0 {
			r.subtitleIndex = r.buildSubtitleIndex()
		}
		for _, candidate := range r.uniqueAlignedCandidatesFromSubtitleIndex(requested) {
			candidate = r.clampToDuration(candidate)
			if _, exists := usedSeconds[nativeScreenshotSecond(candidate)]; exists {
				continue
			}
			return candidate, true, true
		}
	}

	return 0, false, false
}

func (r *nativeScreenshotRunner) uniqueAlignedCandidatesFromSubtitleIndex(requested float64) []float64 {
	if len(r.subtitleIndex) == 0 {
		return nil
	}

	type secondCandidate struct {
		value    float64
		distance float64
		second   int
	}

	candidates := make([]secondCandidate, 0, len(r.subtitleIndex))
	seen := make(map[int]struct{}, len(r.subtitleIndex))
	for _, span := range r.subtitleIndex {
		startSecond := nativeScreenshotSecond(span.Start)
		endSecond := nativeScreenshotSecond(span.End)
		for second := startSecond; second <= endSecond; second++ {
			secondStart := math.Max(span.Start, float64(second))
			secondEnd := math.Min(span.End, float64(second)+0.999)
			if secondEnd < secondStart {
				continue
			}
			candidate := secondStart + (secondEnd-secondStart)/2
			secondKey := nativeScreenshotSecond(candidate)
			if _, exists := seen[secondKey]; exists {
				continue
			}
			seen[secondKey] = struct{}{}
			candidates = append(candidates, secondCandidate{
				value:    candidate,
				distance: math.Abs(candidate - requested),
				second:   secondKey,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			if candidates[i].second == candidates[j].second {
				return candidates[i].value < candidates[j].value
			}
			return candidates[i].second < candidates[j].second
		}
		return candidates[i].distance < candidates[j].distance
	})

	values := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.value)
	}
	return values
}

func (r *nativeScreenshotRunner) chooseSubtitle() {
	r.subtitle = nativeSubtitleSelection{Mode: "none", RelativeIndex: -1, StreamIndex: -1}

	if r.subtitleMode == SubtitleModeOff {
		r.logf("[信息] 已禁用字幕挂载与字幕对齐，将直接按时间点截图。")
		return
	}

	if selection, ok := r.findExternalSubtitle(); ok {
		r.subtitle = selection
		r.logSelectedSubtitleSummary()
		r.logSubtitleFallback("外挂")
		return
	}

	if selection, ok := r.pickInternalSubtitle(); ok {
		r.subtitle = selection
		r.logSelectedSubtitleSummary()
		r.logSubtitleFallback("内挂")
		return
	}

	r.logf("[提示] 未找到可用字幕，将仅截图视频画面。")
}

func (r *nativeScreenshotRunner) findExternalSubtitle() (nativeSubtitleSelection, bool) {
	dir := filepath.Dir(r.sourcePath)
	base := strings.TrimSuffix(filepath.Base(r.sourcePath), filepath.Ext(r.sourcePath))

	candidates := make([]string, 0)
	for _, ext := range []string{"ass", "ssa", "srt"} {
		for _, token := range append(append(append([]string{}, nativeLangZHHans...), nativeLangZHHant...), nativeLangZH...) {
			candidates = append(candidates,
				filepath.Join(dir, base+"."+token+"."+ext),
				filepath.Join(dir, base+"-"+token+"."+ext),
				filepath.Join(dir, base+"_"+token+"."+ext),
			)
		}
		for _, token := range nativeLangEN {
			candidates = append(candidates,
				filepath.Join(dir, base+"."+token+"."+ext),
				filepath.Join(dir, base+"-"+token+"."+ext),
				filepath.Join(dir, base+"_"+token+"."+ext),
			)
		}
	}

	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			lowerName := strings.ToLower(entry.Name())
			if !strings.Contains(lowerName, strings.ToLower(base)) {
				continue
			}
			if strings.HasSuffix(lowerName, ".ass") || strings.HasSuffix(lowerName, ".ssa") || strings.HasSuffix(lowerName, ".srt") {
				candidates = append(candidates, filepath.Join(dir, entry.Name()))
			}
		}
	}

	bestPath := ""
	bestLang := ""
	bestScore := -1
	seen := map[string]struct{}{}

	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}

		langClass := nativeClassifySubtitleLanguage(filepath.Base(candidate))
		if langClass == "" {
			continue
		}
		score := nativeSubtitleLanguageScore(langClass)
		if score > bestScore {
			bestScore = score
			bestPath = candidate
			bestLang = langClass
		}
	}

	if bestPath == "" {
		return nativeSubtitleSelection{}, false
	}

	r.logf("[信息] 选择外挂字幕：%s （语言：%s，字幕格式：%s）", bestPath, bestLang, nativeSubtitleFormatLabel(nativeSubtitleCodecFromPath(bestPath)))
	return nativeSubtitleSelection{
		Mode:          "external",
		File:          bestPath,
		Lang:          bestLang,
		Codec:         nativeSubtitleCodecFromPath(bestPath),
		RelativeIndex: -1,
		StreamIndex:   -1,
	}, true
}

func (r *nativeScreenshotRunner) pickInternalSubtitle() (nativeSubtitleSelection, bool) {
	helperTracks := make([]nativeHelperTrack, 0)
	helperResult := nativeBlurayHelperResult{}
	blurayTracks := make([]nativeSubtitleTrack, 0)
	blurayMode := "none"
	dvdMediaInfoTracks := make([]nativeDVDMediaInfoTrack, 0)
	dvdMediaInfoResult := nativeDVDMediaInfoResult{}
	currentPlaylist := r.blurayContext.Playlist

	if nativeLooksLikeDVDSource(r.dvdProbeSource()) {
		if result, ok := r.probeDVDMediaInfo(); ok {
			dvdMediaInfoResult = result
			dvdMediaInfoTracks = result.Tracks
			r.logf("[信息] DVD 选轨改用 mediainfo 字幕元数据：IFO=%s | VOB=%s | 字幕条数=%d",
				nativeDisplayProbeValue(result.ProbePath),
				nativeDisplayProbeValue(result.SelectedVOBPath),
				len(result.Tracks),
			)
		}
	}

	rawTracks, err := r.probeSubtitleTracks(r.subtitleProbeSource())
	if err != nil || len(rawTracks) == 0 {
		if len(dvdMediaInfoTracks) > 0 {
			r.logf("[提示] ffprobe 未从 %s 识别到字幕流，但 mediainfo 已识别到 %d 条 DVD 字幕元数据。",
				r.subtitleProbeSource(),
				len(dvdMediaInfoTracks),
			)
		}
		return nativeSubtitleSelection{}, false
	}

	if r.blurayContext.Root != "" && r.blurayContext.Playlist != "" {
		if result, tracks, ok := r.probeBlurayHelper(r.blurayContext.Playlist); ok {
			helperResult = result
			helperTracks = tracks
			if !nativeHelperTracksHaveClassifiedLang(helperTracks) {
				for _, playlist := range r.listBlurayPlaylistsRanked() {
					if playlist == currentPlaylist {
						continue
					}
					result, altTracks, ok := r.probeBlurayHelper(playlist)
					if !ok || len(altTracks) == 0 {
						continue
					}
					if nativeHelperTracksHaveClassifiedLang(altTracks) {
						r.blurayContext.Playlist = playlist
						helperResult = result
						helperTracks = altTracks
						r.logf("[信息] 首选 playlist %s 未识别出中英字幕语言，改用候选 playlist %s。", currentPlaylist, playlist)
						break
					}
				}
			}
			blurayMode = "helper"
			r.logf("[信息] 原盘选轨改用 bdsub（BDInfo-style MPLS/CLPI）字幕元数据：%s / playlist %s / clip %s",
				r.blurayContext.Root,
				r.blurayContext.Playlist,
				r.blurayContext.Clip,
			)
		}

		if blurayMode == "none" {
			if result, ok := r.probeBlurayFFprobe(r.blurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
				blurayTracks = result
				if !nativeTracksHaveClassifiedLang(blurayTracks) {
					for _, playlist := range r.listBlurayPlaylistsRanked() {
						if playlist == currentPlaylist {
							continue
						}
						altTracks, ok := r.probeBlurayFFprobe(playlist)
						if !ok || len(altTracks) != len(rawTracks) || len(altTracks) == 0 {
							continue
						}
						if nativeTracksHaveClassifiedLang(altTracks) {
							r.blurayContext.Playlist = playlist
							blurayTracks = altTracks
							r.logf("[信息] 首选 playlist %s 未识别出中英字幕语言，改用候选 playlist %s。", currentPlaylist, playlist)
							break
						}
					}
				}
				blurayMode = "ffprobe"
				r.logf("[信息] 原盘选轨回退到 ffprobe bluray playlist 字幕元数据：bluray:%s -playlist %s", r.blurayContext.Root, r.blurayContext.Playlist)
			}
		}
	}

	r.logInternalSubtitleTracks(rawTracks, helperTracks, helperResult, blurayTracks, blurayMode, dvdMediaInfoTracks, dvdMediaInfoResult)

	best := nativeSubtitleTrack{}
	bestLangClass := ""
	bestScore := -1
	bestPID := math.MaxInt

	fallback := nativeSubtitleTrack{}
	fallbackScore := -1

	other := nativeSubtitleTrack{}
	otherScore := -1

	helperLangByPID := map[int]string{}
	for _, item := range helperTracks {
		helperLangByPID[item.PID] = strings.ToLower(strings.TrimSpace(item.Lang))
	}
	dvdTrackByStreamID := nativeResolveDVDMediaInfoTracks(rawTracks, dvdMediaInfoTracks)

	for index, track := range rawTracks {
		langForPick := track.Language
		titleForPick := track.Title
		pidValue, pidOK := nativeNormalizeStreamPID(track.StreamID)

		switch blurayMode {
		case "helper":
			if pid, ok := nativeNormalizeStreamPID(track.StreamID); ok {
				if lang := helperLangByPID[pid]; lang != "" {
					langForPick = lang
				} else if len(helperTracks) == len(rawTracks) && index < len(helperTracks) && helperTracks[index].Lang != "" {
					langForPick = strings.ToLower(strings.TrimSpace(helperTracks[index].Lang))
				}
			}
		case "ffprobe":
			if index < len(blurayTracks) {
				if blurayTracks[index].Language != "" && blurayTracks[index].Language != "unknown" {
					langForPick = blurayTracks[index].Language
				}
				if blurayTracks[index].Title != "" {
					titleForPick = blurayTracks[index].Title
				}
			}
		}

		dispositionScore := nativeSubtitleDispositionScore(track.Forced, track.IsDefault)
		if pidOK {
			if meta, ok := dvdTrackByStreamID[pidValue]; ok {
				if strings.TrimSpace(meta.Language) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Language))
				}
				dispositionScore += 5
				if strings.TrimSpace(meta.Title) != "" {
					titleForPick = strings.TrimSpace(meta.Title)
				}
			}
		}
		langClass := nativeClassifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))

		if langClass != "" {
			score := nativeSubtitleLanguageScore(langClass) + dispositionScore
			if score > bestScore || (score == bestScore && pidOK && pidValue < bestPID) {
				best = track
				best.Language = langForPick
				best.Title = titleForPick
				bestLangClass = langClass
				bestScore = score
				if pidOK {
					bestPID = pidValue
				}
			}
			continue
		}

		if track.IsDefault == 1 && dispositionScore > fallbackScore {
			fallback = track
			fallback.Language = langForPick
			fallback.Title = titleForPick
			fallbackScore = dispositionScore
		}
		if dispositionScore > otherScore {
			other = track
			other.Language = langForPick
			other.Title = titleForPick
			otherScore = dispositionScore
		}
	}

	if bestScore < 0 {
		if fallbackScore >= 0 {
			best = fallback
			bestLangClass = "default"
		} else if otherScore >= 0 {
			best = other
			bestLangClass = "other"
		} else {
			return nativeSubtitleSelection{}, false
		}
	}

	relativeIndex, err := r.resolveRelativeSubtitleIndex(r.subtitleProbeSource(), best.Index)
	if err != nil {
		relativeIndex = 0
	}

	r.logf("[信息] 选择内挂字幕：流索引 %d / 字幕序号 %d （语言：%s，title：%s，default=%d，forced=%d，字幕格式：%s，codec：%s）",
		best.Index,
		relativeIndex,
		bestLangClass,
		nativeDisplayProbeValue(best.Title),
		best.IsDefault,
		best.Forced,
		nativeSubtitleFormatLabel(best.Codec),
		best.Codec,
	)

	return nativeSubtitleSelection{
		Mode:          "internal",
		StreamIndex:   best.Index,
		RelativeIndex: relativeIndex,
		Lang:          bestLangClass,
		Codec:         best.Codec,
		Title:         best.Title,
	}, true
}

func (r *nativeScreenshotRunner) prepareBlurayProbeContext() {
	clip := strings.TrimSuffix(filepath.Base(r.sourcePath), filepath.Ext(r.sourcePath))
	if len(clip) != 5 || !nativeAllDigits(clip) {
		return
	}
	root, ok := nativeFindBlurayRootFromVideo(r.sourcePath)
	if !ok {
		return
	}
	playlists := nativeListBlurayPlaylistsRanked(root, clip)
	if len(playlists) == 0 {
		return
	}

	r.blurayContext = nativeBlurayProbeContext{
		Root:     root,
		Playlist: playlists[0],
		Clip:     clip,
	}
	r.logf("[信息] 原盘字幕语言探测优先使用 bluray:%s -playlist %s （来源：本地 MPLS 评分，clip：%s）",
		root,
		playlists[0],
		clip,
	)
}

func (r *nativeScreenshotRunner) listBlurayPlaylistsRanked() []string {
	if r.blurayContext.Root == "" || r.blurayContext.Clip == "" {
		return nil
	}
	playlists := nativeListBlurayPlaylistsRanked(r.blurayContext.Root, r.blurayContext.Clip)
	if len(playlists) > nativePlaylistScanMax+1 {
		playlists = playlists[:nativePlaylistScanMax+1]
	}
	return playlists
}

func (r *nativeScreenshotRunner) probeBlurayHelper(playlist string) (nativeBlurayHelperResult, []nativeHelperTrack, bool) {
	if r.bdsubBin == "" || r.blurayContext.Root == "" || r.blurayContext.Clip == "" {
		return nativeBlurayHelperResult{}, nil, false
	}

	stdout, stderr, err := system.RunCommand(r.ctx, r.bdsubBin, r.blurayContext.Root, "--playlist", playlist, "--clip", r.blurayContext.Clip)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message != "" {
			r.logf("[提示] bdsub 失败：%s", message)
		}
		return nativeBlurayHelperResult{}, nil, false
	}

	var result nativeBlurayHelperResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		r.logf("[提示] bdsub 输出无法解析为预期 JSON，回退 ffprobe bluray playlist 探测。")
		return nativeBlurayHelperResult{}, nil, false
	}
	if len(result.Clip.PGStreams) == 0 {
		r.logf("[提示] bdsub 返回 0 条可用 PG 流（source=%s，clip=%s，pg_stream_count=%d），回退 ffprobe bluray playlist 探测。",
			nativeDisplayProbeValue(result.Source),
			nativeDisplayProbeValue(result.Clip.ClipID),
			result.Clip.PGStreamCount,
		)
		return nativeBlurayHelperResult{}, nil, false
	}

	r.logf("[信息] 已调用 bdsub：%s / playlist %s / clip %s", r.blurayContext.Root, playlist, r.blurayContext.Clip)
	return result, result.Clip.PGStreams, true
}

func (r *nativeScreenshotRunner) probeBlurayFFprobe(playlist string) ([]nativeSubtitleTrack, bool) {
	if r.blurayContext.Root == "" {
		return nil, false
	}
	tracks, err := r.probeSubtitleTracks("bluray:"+r.blurayContext.Root, "-playlist", playlist)
	if err != nil || len(tracks) == 0 {
		return nil, false
	}
	return tracks, true
}

func (r *nativeScreenshotRunner) probeDVDMediaInfo() (nativeDVDMediaInfoResult, bool) {
	if strings.TrimSpace(r.mediainfoBin) == "" {
		return nativeDVDMediaInfoResult{}, false
	}

	result, err := probeDVDMediaInfo(r.ctx, r.mediainfoBin, r.dvdSelectedIFOPath(), r.dvdSelectedVOBPath())
	if err != nil {
		r.logf("[提示] mediainfo(DVD) 失败：%s", err.Error())
		return nativeDVDMediaInfoResult{}, false
	}
	if len(result.Tracks) == 0 {
		r.logf("[提示] mediainfo(DVD) 未返回字幕元数据。")
		return nativeDVDMediaInfoResult{}, false
	}

	r.logf("[信息] 已调用 mediainfo(DVD)：IFO=%s | VOB=%s",
		nativeDisplayProbeValue(result.ProbePath),
		nativeDisplayProbeValue(result.SelectedVOBPath),
	)
	if strings.TrimSpace(result.LanguageFallbackPath) != "" {
		r.logf("[信息] mediainfo(DVD) 语言回退：IFO 缺语言，已从 BUP 补齐：%s", result.LanguageFallbackPath)
	}
	return result, true
}

func (r *nativeScreenshotRunner) logInternalSubtitleTracks(raw []nativeSubtitleTrack, helper []nativeHelperTrack, helperResult nativeBlurayHelperResult, bluray []nativeSubtitleTrack, blurayMode string, dvdMediaInfo []nativeDVDMediaInfoTrack, dvdMediaInfoResult nativeDVDMediaInfoResult) {
	if len(raw) == 0 {
		return
	}

	helperLangByPID := map[int]nativeHelperTrack{}
	for _, item := range helper {
		helperLangByPID[item.PID] = item
	}
	dvdTrackByStreamID := nativeResolveDVDMediaInfoTracks(raw, dvdMediaInfo)

	r.logf("[信息] 可用内挂字幕轨（共 %d 条）：", len(raw))
	for index, track := range raw {
		langForPick := track.Language
		titleForPick := track.Title
		tagDetail := ""
		pidDetail := ""

		if pid, ok := nativeNormalizeStreamPID(track.StreamID); ok {
			pidDetail = fmt.Sprintf(" | PID=%s", nativeFormatStreamPID(pid))
			if blurayMode == "helper" {
				if meta, ok := helperLangByPID[pid]; ok {
					if strings.TrimSpace(meta.Lang) != "" {
						langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
					}
					tagDetail = fmt.Sprintf("bdsub: coding_type=%d, char_code=%d, subpath_id=%d", meta.CodingType, meta.CharCode, meta.SubpathID)
				}
			}
			if meta, ok := dvdTrackByStreamID[pid]; ok {
				if strings.TrimSpace(meta.Language) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Language))
				}
				if strings.TrimSpace(meta.Title) != "" {
					titleForPick = strings.TrimSpace(meta.Title)
				}
				tagDetail = fmt.Sprintf("mediainfo: id=%s, format=%s, source=%s",
					nativeDisplayProbeValue(meta.ID),
					nativeDisplayProbeValue(meta.Format),
					nativeDisplayProbeValue(meta.Source),
				)
			}
		}
		if blurayMode == "ffprobe" && index < len(bluray) {
			if bluray[index].Language != "" && bluray[index].Language != "unknown" {
				langForPick = bluray[index].Language
			}
			if bluray[index].Title != "" {
				titleForPick = bluray[index].Title
			}
			if bluray[index].Tags != "" {
				tagDetail = bluray[index].Tags
			}
		}

		langClass := nativeClassifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass == "" {
			langClass = "未识别"
		}

		r.logf("[字幕] 流索引 %d / 字幕序号 %d%s | 格式：%s | 语言：%s | title：%s | default=%d | forced=%d | codec=%s | 分类=%s",
			track.Index,
			index,
			pidDetail,
			nativeSubtitleFormatLabel(track.Codec),
			nativeDisplayProbeValue(langForPick),
			nativeDisplayProbeValue(titleForPick),
			track.IsDefault,
			track.Forced,
			track.Codec,
			langClass,
		)

		if langClass == "未识别" {
			details := make([]string, 0, 3)
			if track.Tags != "" {
				details = append(details, "ffprobe(file) tags: "+track.Tags)
			}
			if tagDetail != "" {
				details = append(details, tagDetail)
			}
			if len(details) > 0 {
				r.logf("[字幕] 流索引 %d 标签：%s", track.Index, strings.Join(details, " | "))
			}
		}
	}

	if blurayMode == "helper" && helperResult.Source != "" {
		r.logf("[信息] bdsub 来源：%s / clip=%s / pg_stream_count=%d",
			helperResult.Source,
			nativeDisplayProbeValue(helperResult.Clip.ClipID),
			helperResult.Clip.PGStreamCount,
		)
	}
	if len(dvdMediaInfo) > 0 {
		r.logf("[信息] mediainfo(DVD) 来源：IFO=%s | VOB=%s | subtitle_count=%d / duration=%s",
			nativeDisplayProbeValue(dvdMediaInfoResult.ProbePath),
			nativeDisplayProbeValue(dvdMediaInfoResult.SelectedVOBPath),
			len(dvdMediaInfo),
			nativeSecToHMS(dvdMediaInfoResult.Duration),
		)
	}
}

func (r *nativeScreenshotRunner) resolveRelativeSubtitleIndex(input string, streamIndex int) (int, error) {
	stdout, stderr, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "s",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		input,
	)
	if err != nil {
		return 0, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	lines := strings.Split(stdout, "\n")
	relative := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		value, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		if value == streamIndex {
			return relative, nil
		}
		relative++
	}
	return 0, errors.New("subtitle stream not found in ffprobe select_streams output")
}

func (r *nativeScreenshotRunner) probeSubtitleTracks(input string, extraArgs ...string) ([]nativeSubtitleTrack, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
	}
	args = append(args, extraArgs...)
	args = append(args,
		"-select_streams", "s",
		"-show_entries", "stream=index,id,codec_name:stream_tags:stream_disposition=default,forced",
		"-of", "json",
		input,
	)

	stdout, stderr, err := system.RunCommand(r.ctx, r.ffprobeBin, args...)
	if err != nil {
		return nil, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var payload nativeFFprobeStreamsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}

	tracks := make([]nativeSubtitleTrack, 0, len(payload.Streams))
	for _, stream := range payload.Streams {
		tracks = append(tracks, nativeSubtitleTrack{
			Index:     stream.Index,
			StreamID:  nativeJSONString(stream.ID),
			Codec:     strings.ToLower(strings.TrimSpace(stream.CodecName)),
			Language:  strings.ToLower(strings.TrimSpace(nativeFirstSubtitleLanguage(stream.Tags))),
			Title:     strings.TrimSpace(nativeFirstSubtitleTitle(stream.Tags)),
			Forced:    stream.Disposition.Forced,
			IsDefault: stream.Disposition.Default,
			Tags:      nativeSubtitleTagsSummary(stream.Tags),
		})
	}
	return tracks, nil
}

func (r *nativeScreenshotRunner) detectStartOffset() float64 {
	return r.detectStartOffsetForInput(r.sourcePath)
}

func (r *nativeScreenshotRunner) detectStartOffsetForInput(input string) float64 {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := nativeFirstFloatLine(stdout); ok {
			return value
		}
	}

	stdout, _, err = system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-show_entries", "format=start_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		input,
	)
	if err == nil {
		if value, ok := nativeFirstFloatLine(stdout); ok {
			return value
		}
	}
	return 0
}

func (r *nativeScreenshotRunner) detectVideoDimensions() (int, int) {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		r.sourcePath,
	)
	if err != nil {
		return 0, 0
	}

	value := strings.TrimSpace(strings.SplitN(stdout, "\n", 2)[0])
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, err1 := strconv.Atoi(parts[0])
	height, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0
	}
	return width, height
}

func (r *nativeScreenshotRunner) detectColorspace() string {
	stdout, _, err := system.RunCommand(r.ctx, r.ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=color_space,color_primaries,color_transfer",
		"-of", "default=noprint_wrappers=1",
		r.sourcePath,
	)
	if err != nil {
		return ""
	}

	lines := make([]string, 0, 3)
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "color_space=") || strings.HasPrefix(line, "color_primaries=") || strings.HasPrefix(line, "color_transfer=") {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "|") + "|"
}

func nativeBuildColorspaceChain(info string) string {
	switch {
	case strings.Contains(info, "bt2020") && (strings.Contains(info, "smpte2084") || strings.Contains(info, "arib-std-b67")):
		return "format=yuv420p10le,zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709:t=bt709,tonemap=hable:desat=0,format=rgb24"
	case strings.Contains(info, "bt2020"):
		return "format=rgb24,scale=in_color_matrix=bt2020:out_color_matrix=bt709"
	default:
		return "format=rgb24"
	}
}

func nativeBuildDisplayAspectFilter() string {
	// DVD/VOB and other anamorphic sources often use non-square pixels.
	// Still-image formats do not reliably preserve SAR, so we expand to
	// square pixels before writing PNG/JPG.
	return "scale='trunc(iw*sar/2)*2:ih',setsar=1"
}

func nativeJoinFilters(parts ...string) string {
	filters := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filters = append(filters, part)
	}
	return strings.Join(filters, ",")
}

func nativeBitmapPacketMinSize(codec string) int {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "hdmv_pgs_subtitle", "pgssub":
		return 1500
	case "dvd_subtitle", "vobsub", "dvb_subtitle", "xsub":
		return 1
	default:
		return 1
	}
}

func (r *nativeScreenshotRunner) alignToSubtitle(requested float64) float64 {
	if r.subtitle.Mode == "none" {
		return requested
	}

	if candidate, ok := r.snapWindow(requested); ok {
		candidate = r.clampToDuration(candidate)
		if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
			if confirmed, ok := r.acceptBitmapSubtitleCandidate("就近/扩窗字幕", candidate); ok {
				r.logf("[对齐] 请求 %s → 就近/扩窗字幕 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(confirmed))
				return confirmed
			}
		} else {
			r.logf("[对齐] 请求 %s → 就近/扩窗字幕 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(candidate))
			return candidate
		}
	}

	if candidate, ok := r.snapExpandedWindow(requested); ok {
		candidate = r.clampToDuration(candidate)
		if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
			if confirmed, ok := r.acceptBitmapSubtitleCandidate("扩窗字幕", candidate); ok {
				r.logf("[对齐] 请求 %s → 扩窗字幕 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(confirmed))
				return confirmed
			}
		} else {
			r.logf("[对齐] 请求 %s → 扩窗字幕 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(candidate))
			return candidate
		}
	}

	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		if candidate, ok := r.findNearestBitmapSubtitle(requested); ok && math.Abs(candidate-requested) <= 1200 && candidate >= 0 {
			if confirmed, ok := r.acceptBitmapSubtitleCandidate("渐进扩窗", candidate); ok {
				r.logf("[对齐] 请求 %s → 渐进扩窗 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(confirmed))
				return confirmed
			}
		}
	}

	if len(r.subtitleIndex) == 0 {
		r.subtitleIndex = r.buildSubtitleIndex()
	}
	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		if candidate, ok := r.findNearestVisibleBitmapIndexedCandidate(requested); ok {
			candidate = r.clampToDuration(candidate)
			if nativeFloatDiffGT(candidate, requested) {
				r.logf("[对齐] 请求 %s → 全片位图索引 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(candidate))
			} else {
				r.logf("[提示] 周边扩窗未命中，沿用全片位图索引对齐到原时间点附近：%s", nativeSecToHMSMS(candidate))
			}
			return candidate
		}
		r.logf("[提示] 周边及全片均未找到可见字幕事件，按原时间点截图：%s", nativeSecToHMSMS(requested))
		return requested
	}
	if candidate, ok := nativeSnapFromIndex(requested, r.subtitleIndex, nativeSubSnapEpsilon); ok {
		candidate = r.clampToDuration(candidate)
		if nativeFloatDiffGT(candidate, requested) {
			r.logf("[对齐] 请求 %s → 全片索引 %s", nativeSecToHMSMS(requested), nativeSecToHMSMS(candidate))
		} else {
			r.logf("[提示] 周边及全片均未找到字幕事件，按原时间点截图：%s", nativeSecToHMSMS(requested))
		}
		return candidate
	}

	r.logf("[提示] 周边及全片均未找到字幕事件，按原时间点截图：%s", nativeSecToHMSMS(requested))
	return requested
}

func (r *nativeScreenshotRunner) snapWindow(requested float64) (float64, bool) {
	var spans []nativeSubtitleSpan
	var err error

	switch {
	case r.subtitle.Mode == "internal" && r.isBitmapSubtitle():
		absoluteStart := math.Max(requested+r.startOffset-r.settings.SearchBack, 0)
		spans, err = r.probeBitmapSpans(absoluteStart, r.settings.SearchBack+r.settings.SearchForward)
	case r.subtitle.Mode == "internal":
		absoluteStart := math.Max(requested+r.startOffset-r.settings.SearchBack, 0)
		spans, err = r.probeInternalTextSpans(absoluteStart, r.settings.SearchBack+r.settings.SearchForward)
	default:
		start := math.Max(requested-r.settings.SearchBack, 0)
		spans, err = r.probeExternalTextSpans(start, r.settings.SearchBack+r.settings.SearchForward)
	}
	if err != nil {
		r.logf("[提示] 字幕窗口探测失败：%s", err.Error())
		return 0, false
	}
	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		return nativeSnapFromBitmapSpans(requested, spans, nativeSubSnapEpsilon)
	}
	return nativeSnapFromSpans(requested, spans, nativeSubSnapEpsilon)
}

func (r *nativeScreenshotRunner) snapExpandedWindow(requested float64) (float64, bool) {
	var spans []nativeSubtitleSpan
	var err error

	switch {
	case r.subtitle.Mode == "internal" && r.isBitmapSubtitle():
		absoluteStart := math.Max(requested+r.startOffset-60, 0)
		spans, err = r.probeBitmapSpans(absoluteStart, 120)
	case r.subtitle.Mode == "internal":
		absoluteStart := math.Max(requested+r.startOffset-60, 0)
		spans, err = r.probeInternalTextSpans(absoluteStart, 120)
	default:
		start := math.Max(requested-60, 0)
		spans, err = r.probeExternalTextSpans(start, 120)
	}
	if err != nil {
		r.logf("[提示] 字幕扩窗探测失败：%s", err.Error())
		return 0, false
	}
	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		return nativeSnapFromBitmapSpans(requested, spans, nativeSubSnapEpsilon)
	}
	return nativeSnapFromSpans(requested, spans, nativeSubSnapEpsilon)
}

func (r *nativeScreenshotRunner) findNearestBitmapSubtitle(requested float64) (float64, bool) {
	for _, span := range []float64{60, 120, 240, 480, 900} {
		absoluteStart := math.Max(requested+r.startOffset-span, 0)
		spans, err := r.probeBitmapSpans(absoluteStart, span+span)
		if err != nil || len(spans) == 0 {
			continue
		}

		best := -1.0
		bestSpan := nativeSubtitleSpan{}
		bestDistance := math.MaxFloat64
		for _, item := range spans {
			mid := item.Start + (item.End-item.Start)/2
			distance := math.Abs(mid - requested)
			if distance < bestDistance {
				best = mid
				bestSpan = item
				bestDistance = distance
			}
		}
		if best >= 0 {
			return r.clampToDuration(nativeBitmapSnapPoint(bestSpan, nativeSubSnapEpsilon)), true
		}
	}
	return requested, false
}

func (r *nativeScreenshotRunner) acceptBitmapSubtitleCandidate(label string, candidate float64) (float64, bool) {
	candidate = r.clampToDuration(candidate)
	key := nativeBitmapCandidateKey(candidate)
	if _, rejected := r.rejectedBitmapCandidates[key]; rejected {
		return 0, false
	}

	visible, err := r.bitmapSubtitleVisibleAt(candidate)
	if err != nil {
		r.logf("[提示] %s候选可视性验证失败，沿用该时间点：%s | 原因：%s",
			label,
			nativeSecToHMSMS(candidate),
			err.Error(),
		)
		return candidate, true
	}
	if !visible {
		if r.rejectedBitmapCandidates == nil {
			r.rejectedBitmapCandidates = make(map[string]struct{})
		}
		r.rejectedBitmapCandidates[key] = struct{}{}
		r.logf("[提示] %s候选未实际渲染出字幕，继续搜索：%s",
			label,
			nativeSecToHMSMS(candidate),
		)
		return 0, false
	}
	return candidate, true
}

func (r *nativeScreenshotRunner) findNearestVisibleBitmapIndexedCandidate(requested float64) (float64, bool) {
	if len(r.subtitleIndex) == 0 {
		return 0, false
	}

	spans := append([]nativeSubtitleSpan(nil), r.subtitleIndex...)
	sort.Slice(spans, func(i, j int) bool {
		left := math.Abs(nativeBitmapSnapPoint(spans[i], nativeSubSnapEpsilon) - requested)
		right := math.Abs(nativeBitmapSnapPoint(spans[j], nativeSubSnapEpsilon) - requested)
		if left == right {
			return spans[i].Start < spans[j].Start
		}
		return left < right
	})

	limit := len(spans)
	if limit > 8 {
		limit = 8
	}
	for _, span := range spans[:limit] {
		candidate, ok := r.acceptBitmapSubtitleCandidate("全片位图索引", nativeBitmapSnapPoint(span, nativeSubSnapEpsilon))
		if ok {
			return candidate, true
		}
	}
	return 0, false
}

func (r *nativeScreenshotRunner) buildSubtitleIndex() []nativeSubtitleSpan {
	if r.subtitle.Mode == "none" {
		return nil
	}

	var spans []nativeSubtitleSpan
	var err error

	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		spans, err = r.probeBitmapSpans(-1, 0)
	} else if r.subtitle.Mode == "internal" {
		spans, err = r.probeInternalTextSpans(-1, 0)
	} else {
		spans, err = r.probeExternalTextSpans(-1, 0)
	}
	if err != nil {
		r.logf("[提示] 建立字幕索引失败：%s", err.Error())
		return nil
	}
	if len(spans) == 0 {
		return nil
	}

	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		spans = nativeMergeNearbySubtitleSpans(spans, 0.75)
		r.logf("[信息] 已建立字幕索引（位图字幕，共 %d 段）。", len(spans))
		return spans
	}

	r.logf("[信息] 已建立字幕索引（文字字幕）。")
	return spans
}

func (r *nativeScreenshotRunner) probeBitmapSpans(startAbs, duration float64) ([]nativeSubtitleSpan, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", nativeReadInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time,size",
		"-of", "json",
		r.sourcePath,
	)
	return r.probePacketSpans(args, true, true)
}

func (r *nativeScreenshotRunner) probeInternalTextSpans(startAbs, duration float64) ([]nativeSubtitleSpan, error) {
	args := []string{
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-v", "error",
		"-select_streams", fmt.Sprintf("s:%d", r.subtitle.RelativeIndex),
	}
	if startAbs >= 0 {
		args = append(args, "-read_intervals", nativeReadInterval(startAbs, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "json",
		r.sourcePath,
	)
	return r.probePacketSpans(args, true, false)
}

func (r *nativeScreenshotRunner) probeExternalTextSpans(start, duration float64) ([]nativeSubtitleSpan, error) {
	args := []string{"-v", "error"}
	if start >= 0 {
		args = append(args, "-read_intervals", nativeReadInterval(start, duration))
	}
	args = append(args,
		"-show_packets",
		"-show_entries", "packet=pts_time,duration_time",
		"-of", "json",
		r.subtitle.File,
	)
	return r.probePacketSpans(args, false, false)
}

func (r *nativeScreenshotRunner) probePacketSpans(args []string, internal bool, bitmap bool) ([]nativeSubtitleSpan, error) {
	stdout, stderr, err := system.RunCommand(r.ctx, r.ffprobeBin, args...)
	if err != nil {
		return nil, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	if strings.TrimSpace(stdout) == "" {
		return nil, nil
	}

	var payload nativeFFprobePacketsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}

	spans := make([]nativeSubtitleSpan, 0, len(payload.Packets))
	bitmapMinSize := nativeBitmapPacketMinSize(r.subtitle.Codec)
	for _, packet := range payload.Packets {
		pts, ok := nativeParseFloatString(packet.PTSTime)
		if !ok {
			continue
		}
		durationValue, ok := nativeParseFloatString(packet.DurationTime)
		if !ok {
			durationValue = nativeDefaultSubDur
		}
		if bitmap {
			sizeValue, ok := nativeParseIntString(packet.Size)
			if !ok || sizeValue < bitmapMinSize {
				continue
			}
		}

		start := pts
		end := pts + durationValue
		if internal {
			start -= r.startOffset
			end -= r.startOffset
		}
		if end < 0 {
			continue
		}
		if start < 0 {
			start = 0
		}
		spans = append(spans, nativeSubtitleSpan{Start: start, End: end})
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})
	if bitmap {
		return nativeMergeNearbySubtitleSpans(spans, 0.75), nil
	}
	return spans, nil
}

func (r *nativeScreenshotRunner) captureScreenshot(aligned float64, path string) error {
	if err := r.capturePrimary(aligned, path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= nativeOversizeBytes {
		return nil
	}

	sizeMB := float64(info.Size()) / 1024.0 / 1024.0
	if r.variant == VariantJPG {
		r.logf("[提示] %s 大小 %.2fMB，重拍降低质量...", filepath.Base(path), sizeMB)
	} else {
		r.logf("[提示] %s 大小 %.2fMB，重拍并映射到 SDR...", filepath.Base(path), sizeMB)
	}

	tempPath := path + ".tmp" + r.settings.Ext
	if err := r.captureReencoded(aligned, tempPath); err != nil {
		_ = os.Remove(tempPath)
		r.logf("[警告] 重拍失败，保留原始截图：%s", err.Error())
		return nil
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func (r *nativeScreenshotRunner) bitmapSubtitleVisibleAt(aligned float64) (bool, error) {
	if !r.isBitmapSubtitle() || r.subtitle.Mode != "internal" {
		return false, nil
	}

	baseFrame, err := r.captureBitmapProbeFrame(r.sourcePath, aligned, false)
	if err != nil {
		return false, err
	}
	subFrame, err := r.captureBitmapProbeFrame(r.sourcePath, aligned, true)
	if err != nil {
		return false, err
	}
	return baseFrame != subFrame, nil
}

func (r *nativeScreenshotRunner) captureBitmapProbeFrame(inputPath string, localTime float64, withSubtitle bool) (string, error) {
	coarseBack := r.settings.CoarseBackPGS
	coarseSecond := int(math.Max(math.Floor(localTime)-float64(coarseBack), 0))
	fineSecond := localTime - float64(coarseSecond)
	coarseHMS := nativeFormatScriptTimestamp(coarseSecond)

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", inputPath,
		"-ss", nativeFormatFloat(fineSecond),
		"-frames:v", "1",
		"-f", "rawvideo",
		"-pix_fmt", "gray",
	}

	if withSubtitle {
		filterComplex := fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10),%s,format=gray[out]",
			r.subtitle.RelativeIndex,
			nativeBuildDisplayAspectFilter(),
		)
		args = append(args,
			"-filter_complex", filterComplex,
			"-map", "[out]",
			"-",
		)
	} else {
		filterChain := nativeJoinFilters(nativeBuildDisplayAspectFilter(), "format=gray")
		args = append(args,
			"-map", "0:v:0",
			"-vf", filterChain,
			"-",
		)
	}

	stdout, stderr, err := system.RunCommand(r.ctx, r.ffmpegBin, args...)
	if err != nil {
		return "", fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return stdout, nil
}

func (r *nativeScreenshotRunner) capturePrimary(aligned float64, path string) error {
	if r.subtitle.Mode == "external" {
		if _, err := os.Stat(r.subtitle.File); err != nil {
			return fmt.Errorf("subtitle file not found before render: %w", err)
		}
	}

	coarseBack := r.settings.CoarseBackText
	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		coarseBack = r.settings.CoarseBackPGS
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := nativeFormatScriptTimestamp(coarseSecond)

	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		filterComplex := nativeJoinFilters(
			fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
			nativeBuildDisplayAspectFilter(),
		)
		args := []string{
			"-v", "error",
			"-fflags", "+genpts",
			"-ss", coarseHMS,
			"-probesize", r.settings.ProbeSize,
			"-analyzeduration", r.settings.Analyze,
			"-i", r.sourcePath,
			"-ss", nativeFormatFloat(fineSecond),
			"-filter_complex", filterComplex,
			"-frames:v", "1",
			"-y",
		}
		args = append(args, r.primaryOutputArgs()...)
		args = append(args, path)
		return r.runFFmpeg(args)
	}

	frameSelect := fmt.Sprintf("setpts=PTS-STARTPTS,select='gte(t,%s)'", nativeFormatFloat(fineSecond))
	filterChain := nativeJoinFilters(frameSelect, nativeBuildDisplayAspectFilter())

	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = nativeJoinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", nativeFormatFloat(aligned)),
			subFilter,
			nativeBuildDisplayAspectFilter(),
		)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-y",
		"-frames:v", "1",
		"-vf", filterChain,
	}
	args = append(args, r.primaryOutputArgs()...)
	args = append(args, path)
	return r.runFFmpeg(args)
}

func (r *nativeScreenshotRunner) captureReencoded(aligned float64, path string) error {
	if r.variant == VariantJPG {
		return r.captureJPGReencoded(aligned, path)
	}
	return r.capturePNGReencoded(aligned, path)
}

func (r *nativeScreenshotRunner) capturePNGReencoded(aligned float64, path string) error {
	coarseBack := r.settings.CoarseBackText
	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		coarseBack = r.settings.CoarseBackPGS
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := nativeFormatScriptTimestamp(coarseSecond)

	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		filterComplex := nativeJoinFilters(
			fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
			r.colorChain,
			nativeBuildDisplayAspectFilter(),
		)
		args := []string{
			"-v", "error",
			"-fflags", "+genpts",
			"-ss", coarseHMS,
			"-probesize", r.settings.ProbeSize,
			"-analyzeduration", r.settings.Analyze,
			"-i", r.sourcePath,
			"-ss", nativeFormatFloat(fineSecond),
			"-filter_complex", filterComplex,
			"-frames:v", "1",
			"-y",
			"-c:v", "png",
			"-compression_level", "9",
			"-pred", "mixed",
			path,
		}
		return r.runFFmpeg(args)
	}

	frameSelect := fmt.Sprintf("setpts=PTS-STARTPTS,select='gte(t,%s)'", nativeFormatFloat(fineSecond))
	filterChain := nativeJoinFilters(frameSelect, r.colorChain, nativeBuildDisplayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = nativeJoinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", nativeFormatFloat(aligned)),
			subFilter,
			r.colorChain,
			nativeBuildDisplayAspectFilter(),
		)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
		"-c:v", "png",
		"-compression_level", "9",
		"-pred", "mixed",
		path,
	}
	return r.runFFmpeg(args)
}

func (r *nativeScreenshotRunner) captureJPGReencoded(aligned float64, path string) error {
	coarseBack := r.settings.CoarseBackText
	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		coarseBack = r.settings.CoarseBackPGS
	}

	coarseSecond := int(math.Max(math.Floor(aligned)-float64(coarseBack), 0))
	fineSecond := aligned - float64(coarseSecond)
	coarseHMS := nativeFormatScriptTimestamp(coarseSecond)

	quality := nativeFallbackJPGQScale(r.settings.JPGQuality)

	if r.subtitle.Mode == "internal" && r.isBitmapSubtitle() {
		filterComplex := nativeJoinFilters(
			fmt.Sprintf("[0:v:0][0:s:%d]overlay=(W-w)/2:(H-h-10)", r.subtitle.RelativeIndex),
			nativeBuildDisplayAspectFilter(),
		)
		args := []string{
			"-v", "error",
			"-fflags", "+genpts",
			"-ss", coarseHMS,
			"-probesize", r.settings.ProbeSize,
			"-analyzeduration", r.settings.Analyze,
			"-i", r.sourcePath,
			"-ss", nativeFormatFloat(fineSecond),
			"-filter_complex", filterComplex,
			"-frames:v", "1",
			"-y",
			"-c:v", "mjpeg",
			"-q:v", strconv.Itoa(quality),
			path,
		}
		return r.runFFmpeg(args)
	}

	frameSelect := fmt.Sprintf("setpts=PTS-STARTPTS,select='gte(t,%s)'", nativeFormatFloat(fineSecond))
	filterChain := nativeJoinFilters(frameSelect, nativeBuildDisplayAspectFilter())
	if subFilter := r.buildTextSubtitleFilter(); subFilter != "" {
		filterChain = nativeJoinFilters(
			frameSelect,
			fmt.Sprintf("setpts=PTS-STARTPTS+%s/TB", nativeFormatFloat(aligned)),
			subFilter,
			nativeBuildDisplayAspectFilter(),
		)
	}

	args := []string{
		"-v", "error",
		"-fflags", "+genpts",
		"-ss", coarseHMS,
		"-probesize", r.settings.ProbeSize,
		"-analyzeduration", r.settings.Analyze,
		"-i", r.sourcePath,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-y",
		"-vf", filterChain,
		"-c:v", "mjpeg",
		"-q:v", strconv.Itoa(quality),
		path,
	}
	return r.runFFmpeg(args)
}

func (r *nativeScreenshotRunner) primaryOutputArgs() []string {
	if r.variant == VariantJPG {
		return []string{"-c:v", "mjpeg", "-q:v", strconv.Itoa(nativeClampJPGQScale(r.settings.JPGQuality))}
	}
	return []string{"-c:v", "png", "-compression_level", "9", "-pred", "mixed"}
}

func (r *nativeScreenshotRunner) runFFmpeg(args []string) error {
	stdout, stderr, err := system.RunCommand(r.ctx, r.ffmpegBin, args...)
	if err != nil {
		return fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}
	return nil
}

func (r *nativeScreenshotRunner) buildTextSubtitleFilter() string {
	if r.subtitle.Mode == "none" {
		return ""
	}

	sizePart := ""
	if r.videoWidth > 0 && r.videoHeight > 0 {
		sizePart = fmt.Sprintf(":original_size=%dx%d", r.videoWidth, r.videoHeight)
	}

	switch r.subtitle.Mode {
	case "external":
		return fmt.Sprintf("subtitles='%s'%s", nativeEscapeFilterValue(r.subtitle.File), sizePart)
	case "internal":
		return fmt.Sprintf("subtitles='%s'%s:si=%d", nativeEscapeFilterValue(r.sourcePath), sizePart, r.subtitle.RelativeIndex)
	default:
		return ""
	}
}

func (r *nativeScreenshotRunner) isBitmapSubtitle() bool {
	switch strings.ToLower(strings.TrimSpace(r.subtitle.Codec)) {
	case "hdmv_pgs_subtitle", "pgssub", "dvd_subtitle", "dvb_subtitle", "xsub", "vobsub":
		return true
	default:
		return false
	}
}

func (r *nativeScreenshotRunner) clampToDuration(value float64) float64 {
	if value < 0 {
		return 0
	}
	if r.duration > 0 && value > r.duration {
		return r.duration
	}
	return value
}

func (r *nativeScreenshotRunner) logSubtitleFallback(modeLabel string) {
	switch r.subtitle.Lang {
	case "zh-Hant":
		r.logf("[提示] 未找到简体中文字幕，改用繁体%s字幕。", modeLabel)
	case "zh":
		r.logf("[提示] 检测到中文字幕，但未明确识别简繁体，使用中文%s字幕。", modeLabel)
	case "en":
		r.logf("[提示] 未找到中文字幕，改用英文%s字幕。", modeLabel)
	case "other":
		r.logf("[提示] 未找到简体/繁体/英文字幕，改用其他%s字幕。", modeLabel)
	case "default":
		r.logf("[提示] 未找到简体/繁体/英文字幕，改用默认%s字幕。", modeLabel)
	}
}

func (r *nativeScreenshotRunner) logs() string {
	return strings.TrimSpace(strings.Join(r.logLines, "\n"))
}

func (r *nativeScreenshotRunner) logf(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	r.logLines = append(r.logLines, line)
	if r.logHandler != nil {
		r.logHandler(line)
	}
}

func (r *nativeScreenshotRunner) logSelectedSubtitleSummary() {
	if r.subtitle.Mode == "none" {
		return
	}

	source := "外挂"
	render := "直接使用外挂文件"
	if r.subtitle.Mode == "internal" {
		source = "内封"
		render = "直接使用内封轨道"
	}

	r.logf("[字幕格式] 来源：%s | 格式：%s | 渲染：%s", source, nativeSubtitleFormatLabel(r.subtitle.Codec), render)
}

func (r *nativeScreenshotRunner) subtitleProbeSource() string {
	return r.sourcePath
}

func (r *nativeScreenshotRunner) dvdMediaInfoSource() string {
	if strings.TrimSpace(r.dvdMediaInfoPath) != "" {
		return r.dvdMediaInfoPath
	}
	return r.dvdProbeSource()
}

func (r *nativeScreenshotRunner) dvdSelectedIFOPath() string {
	resolved, ok := nativeDVDMediaInfoIFOPath(r.dvdProbeSource())
	if ok {
		return resolved
	}
	resolved, ok = nativeDVDMediaInfoIFOPath(r.dvdMediaInfoSource())
	if ok {
		return resolved
	}
	return r.dvdMediaInfoSource()
}

func (r *nativeScreenshotRunner) dvdSelectedVOBPath() string {
	resolved, ok := nativeDVDMediaInfoTitleVOBPath(r.dvdProbeSource())
	if ok {
		return resolved
	}
	resolved, ok = nativeDVDMediaInfoTitleVOBPath(r.dvdMediaInfoSource())
	if ok {
		return resolved
	}
	return ""
}

func (r *nativeScreenshotRunner) dvdProbeSource() string {
	return r.sourcePath
}

func nativeSubtitleCodecFromPath(path string) string {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".ass":
		return "ass"
	case ".ssa":
		return "ssa"
	case ".srt":
		return "subrip"
	default:
		return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(filepath.Ext(path))), ".")
	}
}

func nativeSubtitleFormatLabel(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "subrip", "srt":
		return "SRT/SubRip"
	case "ass":
		return "ASS"
	case "ssa":
		return "SSA"
	case "hdmv_pgs_subtitle", "pgssub":
		return "PGS"
	case "dvd_subtitle":
		return "DVD Subtitle"
	case "dvb_subtitle":
		return "DVB Subtitle"
	case "xsub":
		return "XSub"
	case "vobsub":
		return "VobSub"
	case "":
		return "未知"
	default:
		return strings.ToUpper(codec)
	}
}

func nativeParseRequestedTimestamps(values []string) ([]float64, error) {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		parsed, err := nativeParseClockTimestamp(value)
		if err != nil {
			return nil, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

func nativeParseClockTimestamp(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, err
	}

	return float64(hours*3600 + minutes*60 + seconds), nil
}

func nativeReadInterval(start, duration float64) string {
	return fmt.Sprintf("%s%%+%s", nativeFormatFloat(start), nativeFormatFloat(duration))
}

func nativeFormatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func nativeFormatScriptTimestamp(totalSeconds int) string {
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func nativeSecToHMS(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	return nativeFormatScriptTimestamp(total)
}

func nativeSecToFilenameStamp(seconds float64) string {
	total := int(math.Floor(seconds))
	if total < 0 {
		total = 0
	}
	hours := total / 3600
	minutes := (total % 3600) / 60
	remain := total % 60
	return fmt.Sprintf("%02dh%02dm%02ds", hours, minutes, remain)
}

func nativeSecToHMSMS(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := int(seconds / 3600)
	minutes := int(math.Mod(seconds, 3600) / 60)
	remain := seconds - float64(hours*3600+minutes*60)
	return fmt.Sprintf("%02d:%02d:%06.3f", hours, minutes, remain)
}

func nativeSnapFromSpans(target float64, spans []nativeSubtitleSpan, epsilon float64) (float64, bool) {
	for _, span := range spans {
		if target >= span.Start && target <= span.End {
			return nativeClampInsideSpan(target, span, epsilon), true
		}
		if span.Start >= target {
			return nativeClampInsideSpan(span.Start+epsilon, span, epsilon), true
		}
	}
	return 0, false
}

func nativeSnapFromBitmapSpans(target float64, spans []nativeSubtitleSpan, epsilon float64) (float64, bool) {
	for _, span := range spans {
		if target >= span.Start && target <= span.End {
			return nativeBitmapSnapPoint(span, epsilon), true
		}
		if span.Start >= target {
			return nativeBitmapSnapPoint(span, epsilon), true
		}
	}
	return 0, false
}

func nativeSnapNearestBitmapSpan(target float64, spans []nativeSubtitleSpan, epsilon float64) (float64, bool) {
	if len(spans) == 0 {
		return target, false
	}

	bestDistance := math.MaxFloat64
	bestSpan := nativeSubtitleSpan{}
	found := false
	for _, span := range spans {
		mid := span.Start + (span.End-span.Start)/2
		distance := math.Abs(mid - target)
		if !found || distance < bestDistance {
			bestDistance = distance
			bestSpan = span
			found = true
		}
	}
	if !found {
		return target, false
	}
	return nativeBitmapSnapPoint(bestSpan, epsilon), true
}

func nativeSnapFromIndex(target float64, spans []nativeSubtitleSpan, epsilon float64) (float64, bool) {
	if len(spans) == 0 {
		return target, false
	}

	bestAfterIndex := -1
	lastBeforeIndex := -1
	for index, span := range spans {
		if target >= span.Start && target <= span.End {
			return nativeClampInsideSpan(target, span, epsilon), true
		}
		if bestAfterIndex == -1 && span.Start >= target {
			bestAfterIndex = index
		}
		if span.Start <= target {
			lastBeforeIndex = index
		}
	}

	if bestAfterIndex >= 0 {
		return nativeClampInsideSpan(spans[bestAfterIndex].Start+epsilon, spans[bestAfterIndex], epsilon), true
	}
	if lastBeforeIndex >= 0 {
		span := spans[lastBeforeIndex]
		return nativeClampInsideSpan(span.End-epsilon, span, epsilon), true
	}
	return target, false
}

func nativeClampInsideSpan(value float64, span nativeSubtitleSpan, epsilon float64) float64 {
	if span.End <= span.Start {
		return span.Start
	}

	minValue := span.Start + epsilon
	maxValue := span.End - epsilon
	if maxValue < minValue {
		mid := span.Start + (span.End-span.Start)/2
		return mid
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func nativeBitmapSnapPoint(span nativeSubtitleSpan, epsilon float64) float64 {
	return nativeClampInsideSpan(span.Start+(span.End-span.Start)/2, span, epsilon)
}

func nativeBitmapCandidateKey(value float64) string {
	return strconv.FormatInt(int64(math.Round(value*1000)), 10)
}

func nativeClampJPGQScale(value int) int {
	if value < 2 {
		return 2
	}
	if value > 31 {
		return 31
	}
	return value
}

func nativeFallbackJPGQScale(value int) int {
	value = nativeClampJPGQScale(value)
	value += 2
	if value > 6 {
		return 6
	}
	return value
}

func nativeMergeNearbySubtitleSpans(spans []nativeSubtitleSpan, maxGap float64) []nativeSubtitleSpan {
	if len(spans) <= 1 {
		return spans
	}
	if maxGap < 0 {
		maxGap = 0
	}

	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Start == spans[j].Start {
			return spans[i].End < spans[j].End
		}
		return spans[i].Start < spans[j].Start
	})

	merged := make([]nativeSubtitleSpan, 0, len(spans))
	current := spans[0]
	for _, span := range spans[1:] {
		if span.Start <= current.End+maxGap {
			if span.End > current.End {
				current.End = span.End
			}
			continue
		}
		merged = append(merged, current)
		current = span
	}
	merged = append(merged, current)
	return merged
}

func nativeTracksHaveClassifiedLang(tracks []nativeSubtitleTrack) bool {
	for _, track := range tracks {
		if nativeClassifySubtitleLanguage(strings.TrimSpace(track.Language+" "+track.Title)) != "" {
			return true
		}
	}
	return false
}

func nativeHelperTracksHaveClassifiedLang(tracks []nativeHelperTrack) bool {
	for _, track := range tracks {
		if nativeClassifySubtitleLanguage(track.Lang) != "" {
			return true
		}
	}
	return false
}

func nativeLooksLikeDVDSource(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	base := strings.ToLower(filepath.Base(lower))
	parent := filepath.Base(filepath.Dir(path))
	if strings.Contains(lower, "/video_ts/") || strings.EqualFold(parent, "VIDEO_TS") {
		return true
	}
	if strings.HasSuffix(base, ".ifo") || strings.HasSuffix(base, ".vob") || strings.HasSuffix(base, ".bup") {
		return strings.EqualFold(base, "video_ts.ifo") ||
			strings.EqualFold(base, "video_ts.vob") ||
			strings.EqualFold(base, "video_ts.bup") ||
			strings.HasPrefix(base, "vts_")
	}
	return false
}

func nativeClassifySubtitleLanguage(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return ""
	}

	if nativeContainsAnyToken(value, nativeLangZHHans) {
		return "zh-Hans"
	}
	if nativeContainsAnyToken(value, nativeLangZHHant) {
		return "zh-Hant"
	}
	if nativeContainsAnyToken(value, nativeLangZH) {
		return "zh"
	}
	if nativeContainsAnyToken(value, nativeLangEN) {
		return "en"
	}
	return ""
}

func nativeContainsAnyToken(haystack string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(haystack, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

func nativeSubtitleLanguageScore(lang string) int {
	switch lang {
	case "zh-Hans":
		return 400
	case "zh-Hant":
		return 300
	case "zh":
		return 250
	case "en":
		return 200
	default:
		return 0
	}
}

func nativeSubtitleDispositionScore(forced, isDefault int) int {
	switch {
	case forced == 0 && isDefault == 1:
		return 40
	case forced == 0 && isDefault == 0:
		return 30
	case forced == 1 && isDefault == 1:
		return 20
	default:
		return 10
	}
}

func nativeFirstSubtitleLanguage(tags map[string]interface{}) string {
	for _, key := range []string{"language", "lang"} {
		if value := nativeLookupTag(tags, key); value != "" {
			return value
		}
	}
	for _, prefix := range []string{"language-", "language_", "lang-", "lang_"} {
		if value := nativeLookupTagPrefix(tags, prefix); value != "" {
			return value
		}
	}
	return ""
}

func nativeFirstSubtitleTitle(tags map[string]interface{}) string {
	for _, key := range []string{"title", "name", "handler_name"} {
		if value := nativeLookupTag(tags, key); value != "" {
			return value
		}
	}
	for _, prefix := range []string{"title-", "title_", "name-", "name_", "handler_name-", "handler_name_"} {
		if value := nativeLookupTagPrefix(tags, prefix); value != "" {
			return value
		}
	}
	return ""
}

func nativeLookupTag(tags map[string]interface{}, wanted string) string {
	for key, value := range tags {
		if strings.EqualFold(strings.TrimSpace(key), wanted) {
			return strings.TrimSpace(nativeJSONString(value))
		}
	}
	return ""
}

func nativeLookupTagPrefix(tags map[string]interface{}, prefix string) string {
	for key, value := range tags {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), prefix) {
			return strings.TrimSpace(nativeJSONString(value))
		}
	}
	return ""
}

func nativeSubtitleTagsSummary(tags map[string]interface{}) string {
	if len(tags) == 0 {
		return ""
	}

	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(nativeJSONString(tags[key]))
		if value == "" {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

func nativeJSONString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(typed)
	}
}

func nativeDisplayProbeValue(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "", "unknown", "und", "undefined", "null", "n/a", "na":
		return "无"
	default:
		return strings.TrimSpace(value)
	}
}

func nativeUniqueScreenshotName(aligned float64, ext string, used map[string]int) string {
	base := nativeSecToFilenameStamp(aligned)
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base + ext
	}
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

func nativeScreenshotSecond(value float64) int {
	second := int(math.Floor(value))
	if second < 0 {
		return 0
	}
	return second
}

func nativeNormalizeStreamPID(raw string) (int, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	value = strings.TrimPrefix(value, "0x")
	if strings.HasPrefix(strings.TrimSpace(raw), "0x") || strings.HasPrefix(strings.TrimSpace(raw), "0X") {
		parsed, err := strconv.ParseInt(value, 16, 64)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	}
	if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return parsed, true
	}
	return 0, false
}

func nativeFormatStreamPID(value int) string {
	return fmt.Sprintf("0x%04X", value)
}

func nativeFindBlurayRootFromVideo(videoPath string) (string, bool) {
	current := filepath.Dir(videoPath)
	for {
		if current == "/" || current == "." || current == "" {
			return "", false
		}
		if info, err := os.Stat(filepath.Join(current, "BDMV", "STREAM")); err == nil && info.IsDir() {
			return current, true
		}
		if strings.EqualFold(filepath.Base(current), "BDMV") {
			if info, err := os.Stat(filepath.Join(current, "STREAM")); err == nil && info.IsDir() {
				return filepath.Dir(current), true
			}
		}
		next := filepath.Dir(current)
		if next == current {
			return "", false
		}
		current = next
	}
}

type nativePlaylistScore struct {
	Name      string
	Contains  bool
	TotalSize int64
	ClipCount int
	FileSize  int64
}

func nativeListBlurayPlaylistsRanked(root, clip string) []string {
	playlistDir := filepath.Join(root, "BDMV", "PLAYLIST")
	streamDir := filepath.Join(root, "BDMV", "STREAM")

	playlistEntries, err := os.ReadDir(playlistDir)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(streamDir); err != nil || !info.IsDir() {
		return nil
	}

	scores := make([]nativePlaylistScore, 0)
	for _, entry := range playlistEntries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".mpls") {
			continue
		}

		path := filepath.Join(playlistDir, entry.Name())
		clipIDs := nativeExtractMPLSClipIDs(path)
		if len(clipIDs) == 0 {
			continue
		}

		totalSize := int64(0)
		contains := false
		for _, clipID := range clipIDs {
			if clipID == clip {
				contains = true
			}
			if info, err := os.Stat(filepath.Join(streamDir, clipID+".m2ts")); err == nil {
				totalSize += info.Size()
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		scores = append(scores, nativePlaylistScore{
			Name:      strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Contains:  contains,
			TotalSize: totalSize,
			ClipCount: len(clipIDs),
			FileSize:  info.Size(),
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Contains != scores[j].Contains {
			return scores[i].Contains
		}
		if scores[i].TotalSize != scores[j].TotalSize {
			return scores[i].TotalSize > scores[j].TotalSize
		}
		if scores[i].ClipCount != scores[j].ClipCount {
			return scores[i].ClipCount > scores[j].ClipCount
		}
		if scores[i].FileSize != scores[j].FileSize {
			return scores[i].FileSize > scores[j].FileSize
		}
		return scores[i].Name < scores[j].Name
	})

	playlists := make([]string, 0, len(scores))
	for _, score := range scores {
		playlists = append(playlists, score.Name)
	}
	return playlists
}

func nativeExtractMPLSClipIDs(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	matches := nativeClipIDPattern.FindAllString(string(data), -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		clipID := strings.TrimSuffix(match, "M2TS")
		if _, ok := seen[clipID]; ok {
			continue
		}
		seen[clipID] = struct{}{}
		ids = append(ids, clipID)
	}
	return ids
}

func nativeFirstFloatLine(output string) (float64, bool) {
	for _, line := range strings.Split(output, "\n") {
		if value, ok := nativeParseFloatString(line); ok {
			return value, true
		}
	}
	return 0, false
}

func nativeParseFloatString(value string) (float64, bool) {
	text := strings.TrimSpace(value)
	if text == "" || text == "N/A" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}
	return parsed, true
}

func nativeParseIntString(value string) (int, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func nativeFloatDiffGT(a, b float64) bool {
	return math.Abs(a-b) > 0.0005
}

func nativeEscapeFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return value
}

func nativeClearDir(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(path, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func nativeAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, item := range value {
		if item < '0' || item > '9' {
			return false
		}
	}
	return true
}
