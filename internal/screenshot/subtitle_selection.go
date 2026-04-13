// Package screenshot 实现外挂、内挂和原盘字幕选择流程。

package screenshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"minfo/internal/system"
)

// chooseSubtitle 会在当前截图上下文中确定最终使用的字幕来源，并把结果写回运行器状态。
func (r *screenshotRunner) chooseSubtitle() error {
	r.subtitle = subtitleSelection{Mode: "none", RelativeIndex: -1, StreamIndex: -1}

	if r.subtitleMode == SubtitleModeOff {
		r.logf("[信息] 已禁用字幕挂载与字幕对齐，将直接按时间点截图。")
		return nil
	}

	if selection, ok, err := r.findExternalSubtitle(); err != nil {
		return err
	} else if ok {
		r.subtitle = selection
		r.logSubtitleFallback("外挂")
		return nil
	}

	if selection, ok, err := r.pickInternalSubtitle(); err != nil {
		return err
	} else if ok {
		r.subtitle = selection
		r.logSubtitleFallback("内挂")
		return nil
	}

	r.logf("[提示] 未找到可用字幕，将仅截图视频画面。")
	return nil
}

// prepareTextSubtitleRenderSource 会为内挂文字字幕准备更稳定的渲染来源。
func (r *screenshotRunner) prepareTextSubtitleRenderSource() error {
	if r.subtitle.Mode != "internal" {
		return nil
	}
	if r.isSupportedBitmapSubtitle() {
		return nil
	}
	if !isSupportedTextSubtitleCodec(r.subtitle.Codec) {
		return fmt.Errorf("unsupported text subtitle codec: %s", subtitleFormatLabel(r.subtitle.Codec))
	}
	if !r.shouldExtractInternalTextSubtitle() {
		return nil
	}

	pattern, extractionArgs, extractedCodec, logMessage := internalTextSubtitleExtractionPlan(r.subtitle.Codec)
	r.logProgress("字幕", 3, 3, "正在提取内挂文字字幕。")
	r.logf("%s", logMessage)

	tempFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}

	stdout, stderr, err := r.runFFmpegSubtitleExtract([]string{
		"-v", "error",
		"-i", r.sourcePath,
		"-map", fmt.Sprintf("0:s:%d", r.subtitle.RelativeIndex),
		"-c:s", extractionArgs,
		"-y", tempPath,
	})
	if err != nil {
		_ = os.Remove(tempPath)
		if message := strings.TrimSpace(system.BestErrorMessage(err, stderr, stdout)); message != "" {
			normalized := strings.ReplaceAll(message, "\r\n", "\n")
			normalized = strings.ReplaceAll(normalized, "\r", "\n")
			for _, line := range strings.Split(normalized, "\n") {
				if strings.TrimSpace(line) == "" {
					continue
				}
				r.logf("[错误] 提取失败详情: %s", line)
			}
		}
		return fmt.Errorf("failed to extract internal text subtitle: %w", err)
	}

	r.tempSubtitleFile = tempPath
	r.subtitle.Mode = "external"
	r.subtitle.File = tempPath
	r.subtitle.Codec = extractedCodec
	r.subtitle.StreamIndex = -1
	r.subtitle.RelativeIndex = -1
	r.subtitle.ExtractedText = true
	r.logf("[信息] 已提取内挂文本字幕供截图使用：%s", tempPath)
	return nil
}

func (r *screenshotRunner) shouldExtractInternalTextSubtitle() bool {
	if r == nil {
		return false
	}
	if r.subtitle.Mode != "internal" {
		return false
	}
	if r.isSupportedBitmapSubtitle() {
		return false
	}
	return true
}

func internalTextSubtitleExtractionPlan(codec string) (pattern string, extractionCodecArg string, extractedCodec string, logMessage string) {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "ass":
		return "minfo-sub-*.ass", "copy", "ass", "[信息] 内挂 ASS 字幕将提取为原始 ASS 文件，保留样式后参与截图渲染。"
	case "ssa":
		return "minfo-sub-*.ssa", "copy", "ssa", "[信息] 内挂 SSA 字幕将提取为原始 SSA 文件，保留样式后参与截图渲染。"
	default:
		return "minfo-sub-*.srt", "srt", "subrip", "[信息] 内挂文字字幕将先提取为临时字幕文件，再参与截图渲染。"
	}
}

// findExternalSubtitle 会在视频附近查找语言优先级最高的外挂字幕文件。
func (r *screenshotRunner) findExternalSubtitle() (subtitleSelection, bool, error) {
	dir := filepath.Dir(r.sourcePath)
	base := strings.TrimSuffix(filepath.Base(r.sourcePath), filepath.Ext(r.sourcePath))

	candidates := make([]string, 0)
	for _, ext := range []string{"ass", "ssa", "srt", "vtt", "webvtt", "ttml", "dfxp", "smi", "sami", "stl", "sbv", "lrc"} {
		for _, token := range append(append(append([]string{}, langZHHansTokens...), langZHHantTokens...), langZHTokens...) {
			candidates = append(candidates,
				filepath.Join(dir, base+"."+token+"."+ext),
				filepath.Join(dir, base+"-"+token+"."+ext),
				filepath.Join(dir, base+"_"+token+"."+ext),
			)
		}
		for _, token := range langENTokens {
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
			if isKnownTextSubtitleExtension(filepath.Ext(lowerName)) {
				candidates = append(candidates, filepath.Join(dir, entry.Name()))
			}
		}
	}

	bestPath := ""
	bestLang := ""
	bestScore := -1
	seen := map[string]struct{}{}
	firstUnsupportedPath := ""

	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}

		codec := subtitleCodecFromPath(candidate)
		if !isSupportedTextSubtitleCodec(codec) {
			if firstUnsupportedPath == "" {
				firstUnsupportedPath = candidate
			}
			continue
		}

		langClass := classifySubtitleLanguage(filepath.Base(candidate))
		if langClass == "" {
			continue
		}
		score := subtitleLanguageScore(langClass)
		if score > bestScore {
			bestScore = score
			bestPath = candidate
			bestLang = langClass
		}
	}

	if bestPath == "" {
		if firstUnsupportedPath != "" {
			return subtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec: %s", subtitleFormatLabel(subtitleCodecFromPath(firstUnsupportedPath)))
		}
		return subtitleSelection{}, false, nil
	}

	r.logf("[信息] 选择外挂字幕：%s （语言：%s，字幕格式：%s）", bestPath, bestLang, subtitleFormatLabel(subtitleCodecFromPath(bestPath)))
	return subtitleSelection{
		Mode:          "external",
		File:          bestPath,
		Lang:          bestLang,
		Codec:         subtitleCodecFromPath(bestPath),
		RelativeIndex: -1,
		StreamIndex:   -1,
	}, true, nil
}

// pickInternalSubtitle 会综合语言、默认标记、PID 和原盘补充信息选择最合适的内挂字幕轨。
func (r *screenshotRunner) pickInternalSubtitle() (subtitleSelection, bool, error) {
	helperTracks := make([]blurayHelperTrack, 0)
	helperResult := blurayHelperResult{}
	blurayTracks := make([]subtitleTrack, 0)
	blurayMode := "none"
	dvdMediaInfoTracks := make([]dvdMediaInfoTrack, 0)
	dvdMediaInfoResult := dvdMediaInfoResult{}
	currentPlaylist := r.blurayContext.Playlist

	if looksLikeDVDSource(r.dvdProbeSource()) {
		r.logProgress("字幕", 1, 3, "正在读取 DVD 字幕元数据。")
		if result, ok := r.probeDVDMediaInfo(); ok {
			dvdMediaInfoResult = result
			dvdMediaInfoTracks = result.Tracks
			r.logf("[信息] DVD 选轨改用 mediainfo 字幕元数据：IFO=%s | VOB=%s | 字幕条数=%d",
				displayProbeValue(result.ProbePath),
				displayProbeValue(result.SelectedVOBPath),
				len(result.Tracks),
			)
		}
	}

	r.logProgress("字幕", 1, 3, "正在探测内挂字幕轨。")
	rawTracks, err := r.probeSubtitleTracks(r.subtitleProbeSource())
	if err != nil || len(rawTracks) == 0 {
		if len(dvdMediaInfoTracks) > 0 {
			r.logf("[提示] ffprobe 未从 %s 识别到字幕流，但 mediainfo 已识别到 %d 条 DVD 字幕元数据。",
				r.subtitleProbeSource(),
				len(dvdMediaInfoTracks),
			)
		}
		return subtitleSelection{}, false, nil
	}

	if r.blurayContext.Root != "" && r.blurayContext.Playlist != "" {
		r.logProgress("字幕", 2, 3, fmt.Sprintf("正在补充蓝光字幕元数据：playlist %s。", r.blurayContext.Playlist))
		if result, tracks, ok := r.probeBlurayHelper(r.blurayContext.Playlist, ""); ok {
			helperResult = result
			helperTracks = tracks
			if !helperTracksHaveClassifiedLang(helperTracks) {
				for _, playlist := range r.listBlurayPlaylistsRanked() {
					if playlist == currentPlaylist {
						continue
					}
					result, altTracks, ok := r.probeBlurayHelper(playlist, "")
					if !ok || len(altTracks) == 0 {
						continue
					}
					if helperTracksHaveClassifiedLang(altTracks) {
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
			if blurayHelperNeedsFFprobe(rawTracks, helperTracks) {
				r.logProgress("字幕", 2, 3, fmt.Sprintf("正在用 ffprobe 补充蓝光字幕元数据：playlist %s。", r.blurayContext.Playlist))
				if result, ok := r.probeBlurayFFprobe(r.blurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
					blurayTracks = result
					blurayMode = "helper+ffprobe"
					r.logf("[信息] bdsub 字幕元数据不足，继续调用 ffprobe bluray playlist 补充：bluray:%s -playlist %s",
						r.blurayContext.Root,
						r.blurayContext.Playlist,
					)
				} else {
					r.logf("[提示] bdsub 字幕元数据不足，但 ffprobe bluray playlist 未能补充更多字幕信息。")
				}
			}
			if blurayHelperNeedsPayloadScan(rawTracks, helperResult, helperTracks, blurayTracks, blurayMode) {
				r.logProgress("字幕", 2, 3, fmt.Sprintf("正在补充蓝光字幕 payload 元数据：playlist %s。", r.blurayContext.Playlist))
				r.logf("[信息] 检测到同语言 PGS 候选，开始补充 payload_bytes 用于热路径密度排序：playlist %s / clip %s",
					r.blurayContext.Playlist,
					r.blurayContext.Clip,
				)
				if result, tracks, ok := r.probeBlurayHelper(r.blurayContext.Playlist, "payload"); ok {
					helperResult = result
					helperTracks = tracks
				} else {
					r.logf("[提示] bdsub payload_bytes 补充失败，将继续按无密度数据的规则选轨。")
				}
			}
		}

		if blurayMode == "none" {
			r.logProgress("字幕", 2, 3, fmt.Sprintf("正在用 ffprobe 探测蓝光字幕：playlist %s。", r.blurayContext.Playlist))
			if result, ok := r.probeBlurayFFprobe(r.blurayContext.Playlist); ok && len(result) == len(rawTracks) && len(result) > 0 {
				blurayTracks = result
				if !tracksHaveClassifiedLang(blurayTracks) {
					for _, playlist := range r.listBlurayPlaylistsRanked() {
						if playlist == currentPlaylist {
							continue
						}
						altTracks, ok := r.probeBlurayFFprobe(playlist)
						if !ok || len(altTracks) != len(rawTracks) || len(altTracks) == 0 {
							continue
						}
						if tracksHaveClassifiedLang(altTracks) {
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

	best := subtitleTrack{}
	bestLangClass := ""
	bestRank := preferredSubtitleRank{LangScore: -1, DispositionScore: -1}
	unsupportedBitmapDetails := make([]string, 0)
	unsupportedTextDetails := make([]string, 0)

	fallback := subtitleTrack{}
	fallbackScore := -1

	other := subtitleTrack{}
	otherScore := -1

	helperTrackByPID := map[int]blurayHelperTrack{}
	for _, item := range helperTracks {
		helperTrackByPID[item.PID] = item
	}
	dvdTrackByStreamID := resolveDVDMediaInfoTracks(rawTracks, dvdMediaInfoTracks)

	for index, track := range rawTracks {
		langForPick := track.Language
		titleForPick := track.Title
		pidValue, pidOK := normalizeStreamPID(track.StreamID)
		helperMeta := blurayHelperTrack{}
		helperMetaOK := false

		switch blurayMode {
		case "helper", "helper+ffprobe":
			if pidOK {
				if meta, ok := helperTrackByPID[pidValue]; ok {
					helperMeta = meta
					helperMetaOK = true
					if strings.TrimSpace(meta.Lang) != "" {
						langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
					}
				}
			}
			if !helperMetaOK && len(helperTracks) == len(rawTracks) && index < len(helperTracks) {
				helperMeta = helperTracks[index]
				helperMetaOK = true
				if strings.TrimSpace(helperMeta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(helperMeta.Lang))
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
		if (blurayMode == "ffprobe" || blurayMode == "helper+ffprobe") && index < len(blurayTracks) {
			needsSupplement := blurayMode == "ffprobe" || subtitleNeedsBluraySupplement(langForPick, titleForPick)
			if needsSupplement {
				if blurayTracks[index].Language != "" && blurayTracks[index].Language != "unknown" {
					langForPick = blurayTracks[index].Language
				}
				if blurayTracks[index].Title != "" {
					titleForPick = blurayTracks[index].Title
				}
			} else if strings.TrimSpace(titleForPick) == "" && blurayTracks[index].Title != "" {
				titleForPick = blurayTracks[index].Title
			}
		}

		dispositionScore := subtitleDispositionScore(track.Forced, track.IsDefault)
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

		if isUnsupportedBitmapSubtitleCodec(track.Codec) {
			unsupportedBitmapDetails = append(unsupportedBitmapDetails, fmt.Sprintf("流索引 %d(codec=%s)", track.Index, track.Codec))
			continue
		}
		if !isSupportedTextSubtitleCodec(track.Codec) && bitmapSubtitleKindFromCodec(track.Codec) == bitmapSubtitleNone {
			unsupportedTextDetails = append(unsupportedTextDetails, fmt.Sprintf("流索引 %d(codec=%s)", track.Index, track.Codec))
			continue
		}

		langClass := classifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))

		if langClass != "" {
			rank := preferredSubtitleRank{
				LangClass:        langClass,
				LangScore:        subtitleLanguageScore(langClass),
				DispositionScore: dispositionScore,
				PID:              pidValue,
				PIDOK:            pidOK,
				BitmapKind:       bitmapSubtitleKindFromCodec(track.Codec),
				PayloadBytes:     helperMeta.PayloadBytes,
				UsePayloadBytes:  blurayHelperHasPayloadBytes(helperResult) && helperMetaOK && bitmapSubtitleKindFromCodec(track.Codec) == bitmapSubtitlePGS,
				Bitrate:          helperMeta.Bitrate,
				UseBitrate:       helperResult.BitrateScanned && helperMetaOK && bitmapSubtitleKindFromCodec(track.Codec) == bitmapSubtitlePGS,
			}
			if preferPreferredSubtitleRank(rank, bestRank) {
				best = track
				best.Language = langForPick
				best.Title = titleForPick
				bestLangClass = langClass
				bestRank = rank
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

	if len(unsupportedBitmapDetails) > 0 {
		r.logf("[提示] 位图字幕目前仅支持 PGS 和 DVD Subtitle，已跳过暂不支持的位图字幕：%s", strings.Join(unsupportedBitmapDetails, ", "))
	}
	if len(unsupportedTextDetails) > 0 {
		r.logf("[提示] 已发现暂不支持的文本字幕类型：%s", strings.Join(unsupportedTextDetails, ", "))
	}

	if bestRank.LangScore >= 0 && !isSupportedTextSubtitleCodec(best.Codec) && bitmapSubtitleKindFromCodec(best.Codec) == bitmapSubtitleNone {
		return subtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec: %s", subtitleFormatLabel(best.Codec))
	}

	if bestRank.LangScore < 0 {
		if fallbackScore >= 0 {
			best = fallback
			bestLangClass = "default"
		} else if otherScore >= 0 {
			best = other
			bestLangClass = "other"
		} else if len(unsupportedTextDetails) > 0 {
			return subtitleSelection{}, false, fmt.Errorf("unsupported text subtitle codec, only ASS/SSA/SubRip are supported")
		} else {
			return subtitleSelection{}, false, nil
		}
	}

	relativeIndex, err := r.resolveRelativeSubtitleIndex(r.subtitleProbeSource(), best.Index)
	if err != nil {
		relativeIndex = 0
	}

	densitySuffix := ""
	if bestRank.UsePayloadBytes {
		densitySuffix = fmt.Sprintf("，payload_bytes=%d", bestRank.PayloadBytes)
	}
	if bestRank.UseBitrate {
		densitySuffix += fmt.Sprintf("，bitrate=%d", bestRank.Bitrate)
	}

	r.logf("[信息] 选择内挂字幕：流索引 %d / 字幕序号 %d （语言：%s，title：%s，default=%d，forced=%d，字幕格式：%s，codec：%s%s）",
		best.Index,
		relativeIndex,
		bestLangClass,
		displayProbeValue(best.Title),
		best.IsDefault,
		best.Forced,
		subtitleFormatLabel(best.Codec),
		best.Codec,
		densitySuffix,
	)

	return subtitleSelection{
		Mode:          "internal",
		StreamIndex:   best.Index,
		RelativeIndex: relativeIndex,
		Lang:          bestLangClass,
		Codec:         best.Codec,
		Title:         best.Title,
	}, true, nil
}

// prepareBlurayProbeContext 预先推导蓝光根目录、playlist 和 clip，供后续 bdsub 或 ffprobe 探测复用。
func (r *screenshotRunner) prepareBlurayProbeContext() {
	clip := strings.TrimSuffix(filepath.Base(r.sourcePath), filepath.Ext(r.sourcePath))
	if len(clip) != 5 || !allDigits(clip) {
		return
	}
	root, ok := findBlurayRootFromVideo(r.sourcePath)
	if !ok {
		return
	}
	playlists := listBlurayPlaylistsRanked(root, clip)
	if len(playlists) == 0 {
		return
	}

	r.blurayContext = blurayProbeContext{
		Root:     root,
		Playlist: playlists[0],
		Clip:     clip,
	}
	r.logf("[信息] 原盘字幕语言探测优先使用 bdsub（playlist 上下文：bluray:%s -playlist %s，来源：本地 MPLS 评分，clip：%s）",
		root,
		playlists[0],
		clip,
	)
}

// listBlurayPlaylistsRanked 会列出蓝光PlaylistsRanked，并按当前规则返回排序后的结果列表。
func (r *screenshotRunner) listBlurayPlaylistsRanked() []string {
	if r.blurayContext.Root == "" || r.blurayContext.Clip == "" {
		return nil
	}
	playlists := listBlurayPlaylistsRanked(r.blurayContext.Root, r.blurayContext.Clip)
	if len(playlists) > playlistScanMax+1 {
		playlists = playlists[:playlistScanMax+1]
	}
	return playlists
}

// probeBlurayHelper 调用 bdsub 探测当前蓝光 playlist 的字幕元数据；必要时再补充 payload_bytes 或 exact bitrate。
func (r *screenshotRunner) probeBlurayHelper(playlist string, scanMode string) (blurayHelperResult, []blurayHelperTrack, bool) {
	if r.bdsubBin == "" || r.blurayContext.Root == "" || r.blurayContext.Clip == "" {
		return blurayHelperResult{}, nil, false
	}

	args := []string{r.blurayContext.Root, "--playlist", playlist, "--clip", r.blurayContext.Clip}
	switch scanMode {
	case "payload":
		args = append(args, "--scan-payload")
	case "bitrate":
		args = append(args, "--scan-bitrate")
	}
	stdout, stderr, err := system.RunCommandLive(r.ctx, r.bdsubBin, func(stream, line string) {
		if stream != "stderr" {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		r.logf("[bdsub] %s", line)
	}, args...)
	if err != nil {
		message := strings.TrimSpace(stderr)
		if message != "" {
			r.logf("[提示] bdsub 失败：%s", message)
		}
		return blurayHelperResult{}, nil, false
	}

	var result blurayHelperResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		r.logf("[提示] bdsub 输出无法解析为预期 JSON，回退 ffprobe bluray playlist 探测。")
		return blurayHelperResult{}, nil, false
	}
	if len(result.Clip.PGStreams) == 0 {
		r.logf("[提示] bdsub 返回 0 条可用 PG 流（source=%s，clip=%s，pg_stream_count=%d），回退 ffprobe bluray playlist 探测。",
			displayProbeValue(result.Source),
			displayProbeValue(result.Clip.ClipID),
			result.Clip.PGStreamCount,
		)
		return blurayHelperResult{}, nil, false
	}

	switch scanMode {
	case "payload":
		r.logf("[信息] 已调用 bdsub（metadata+payload）：%s / playlist %s / clip %s", r.blurayContext.Root, playlist, r.blurayContext.Clip)
	case "bitrate":
		r.logf("[信息] 已调用 bdsub（metadata+bitrate）：%s / playlist %s / clip %s", r.blurayContext.Root, playlist, r.blurayContext.Clip)
	default:
		r.logf("[信息] 已调用 bdsub（metadata-only）：%s / playlist %s / clip %s", r.blurayContext.Root, playlist, r.blurayContext.Clip)
	}
	return result, result.Clip.PGStreams, true
}

// probeBlurayFFprobe 使用 bluray: 输入和 playlist 参数补充字幕轨元数据。
func (r *screenshotRunner) probeBlurayFFprobe(playlist string) ([]subtitleTrack, bool) {
	if r.blurayContext.Root == "" {
		return nil, false
	}
	tracks, err := r.probeSubtitleTracks("bluray:"+r.blurayContext.Root, "-playlist", playlist)
	if err != nil || len(tracks) == 0 {
		return nil, false
	}
	return tracks, true
}

// probeDVDMediaInfo 在可用时加载 DVD 的 MediaInfo 字幕元数据。
func (r *screenshotRunner) probeDVDMediaInfo() (dvdMediaInfoResult, bool) {
	if strings.TrimSpace(r.mediainfoBin) == "" {
		return dvdMediaInfoResult{}, false
	}

	result, ok, err := r.ensureDVDMediaInfoResult()
	if !ok {
		if err != nil {
			r.logf("[提示] mediainfo(DVD) 失败：%s", err.Error())
		}
		return dvdMediaInfoResult{}, false
	}
	if len(result.Tracks) == 0 {
		r.logf("[提示] mediainfo(DVD) 未返回字幕元数据。")
		return dvdMediaInfoResult{}, false
	}

	r.logf("[信息] 已调用 mediainfo(DVD)：IFO=%s | VOB=%s",
		displayProbeValue(result.ProbePath),
		displayProbeValue(result.SelectedVOBPath),
	)
	if strings.TrimSpace(result.LanguageFallbackPath) != "" {
		r.logf("[信息] mediainfo(DVD) 语言回退：IFO 缺语言，已从 BUP 补齐：%s", result.LanguageFallbackPath)
	}
	return result, true
}

// logInternalSubtitleTracks 会把当前可见的内挂字幕轨和补充元数据按统一格式写入日志。
func (r *screenshotRunner) logInternalSubtitleTracks(raw []subtitleTrack, helper []blurayHelperTrack, helperResult blurayHelperResult, bluray []subtitleTrack, blurayMode string, dvdMediaInfo []dvdMediaInfoTrack, dvdMediaInfoResult dvdMediaInfoResult) {
	if len(raw) == 0 {
		return
	}

	helperLangByPID := map[int]blurayHelperTrack{}
	for _, item := range helper {
		helperLangByPID[item.PID] = item
	}
	dvdTrackByStreamID := resolveDVDMediaInfoTracks(raw, dvdMediaInfo)

	r.logf("[信息] 可用内挂字幕轨（共 %d 条）：", len(raw))
	for index, track := range raw {
		langForPick := track.Language
		titleForPick := track.Title
		tagDetails := make([]string, 0, 4)
		pidDetail := ""
		payloadDetail := ""
		bitrateDetail := ""
		helperMeta := blurayHelperTrack{}
		helperMetaOK := false

		if pid, ok := normalizeStreamPID(track.StreamID); ok {
			pidDetail = fmt.Sprintf(" | PID=%s", formatStreamPID(pid))
			if blurayMode == "helper" || blurayMode == "helper+ffprobe" {
				if meta, ok := helperLangByPID[pid]; ok {
					helperMeta = meta
					helperMetaOK = true
					if strings.TrimSpace(meta.Lang) != "" {
						langForPick = strings.ToLower(strings.TrimSpace(meta.Lang))
					}
				}
			}
			if !helperMetaOK && len(helper) == len(raw) && index < len(helper) && (blurayMode == "helper" || blurayMode == "helper+ffprobe") {
				helperMeta = helper[index]
				helperMetaOK = true
				if strings.TrimSpace(helperMeta.Lang) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(helperMeta.Lang))
				}
			}
			if helperMetaOK {
				bdsubTag := fmt.Sprintf("bdsub: coding_type=%d, char_code=%d, subpath_id=%d", helperMeta.CodingType, helperMeta.CharCode, helperMeta.SubpathID)
				if blurayHelperHasPayloadBytes(helperResult) {
					payloadDetail = fmt.Sprintf(" | payload_bytes=%d", helperMeta.PayloadBytes)
					bdsubTag += fmt.Sprintf(", payload_bytes=%d", helperMeta.PayloadBytes)
				}
				if helperResult.BitrateScanned {
					bitrateDetail = fmt.Sprintf(" | bitrate=%d", helperMeta.Bitrate)
					bdsubTag += fmt.Sprintf(", bitrate=%d", helperMeta.Bitrate)
				}
				tagDetails = append(tagDetails, bdsubTag)
			}
			if meta, ok := dvdTrackByStreamID[pid]; ok {
				if strings.TrimSpace(meta.Language) != "" {
					langForPick = strings.ToLower(strings.TrimSpace(meta.Language))
				}
				if strings.TrimSpace(meta.Title) != "" {
					titleForPick = strings.TrimSpace(meta.Title)
				}
				tagDetails = append(tagDetails, fmt.Sprintf("mediainfo: id=%s, format=%s, source=%s",
					displayProbeValue(meta.ID),
					displayProbeValue(meta.Format),
					displayProbeValue(meta.Source),
				))
			}
		}
		if (blurayMode == "ffprobe" || blurayMode == "helper+ffprobe") && index < len(bluray) {
			needsSupplement := blurayMode == "ffprobe" || subtitleNeedsBluraySupplement(langForPick, titleForPick)
			if needsSupplement {
				if bluray[index].Language != "" && bluray[index].Language != "unknown" {
					langForPick = bluray[index].Language
				}
				if bluray[index].Title != "" {
					titleForPick = bluray[index].Title
				}
			} else if strings.TrimSpace(titleForPick) == "" && bluray[index].Title != "" {
				titleForPick = bluray[index].Title
			}
			if bluray[index].Tags != "" {
				tagDetails = append(tagDetails, "ffprobe(bluray) tags: "+bluray[index].Tags)
			}
		}

		langClass := classifySubtitleLanguage(strings.TrimSpace(langForPick + " " + titleForPick))
		if langClass == "" {
			langClass = "未识别"
		}

		r.logf("[字幕] 流索引 %d / 字幕序号 %d%s | 格式：%s | 语言：%s | title：%s | default=%d | forced=%d | codec=%s | 分类=%s | 处理=%s%s%s",
			track.Index,
			index,
			pidDetail,
			subtitleFormatLabel(track.Codec),
			displayProbeValue(langForPick),
			displayProbeValue(titleForPick),
			track.IsDefault,
			track.Forced,
			track.Codec,
			langClass,
			subtitleHandlingLabel(track.Codec),
			payloadDetail,
			bitrateDetail,
		)

		if langClass == "未识别" {
			details := make([]string, 0, 3)
			if track.Tags != "" {
				details = append(details, "ffprobe(file) tags: "+track.Tags)
			}
			if len(tagDetails) > 0 {
				details = append(details, tagDetails...)
			}
			if len(details) > 0 {
				r.logf("[字幕] 流索引 %d 标签：%s", track.Index, strings.Join(details, " | "))
			}
		}
	}

	if (blurayMode == "helper" || blurayMode == "helper+ffprobe") && helperResult.Source != "" {
		r.logf("[信息] bdsub 来源：%s / clip=%s / pg_stream_count=%d / payload_ready=%t / bitrate_scanned=%t / bitrate_mode=%s / packet_seconds=%.3f",
			helperResult.Source,
			displayProbeValue(helperResult.Clip.ClipID),
			helperResult.Clip.PGStreamCount,
			blurayHelperHasPayloadBytes(helperResult),
			helperResult.BitrateScanned,
			displayProbeValue(helperResult.BitrateMode),
			helperResult.Clip.PacketSeconds,
		)
	}
	if len(dvdMediaInfo) > 0 {
		r.logf("[信息] mediainfo(DVD) 来源：IFO=%s | VOB=%s | subtitle_count=%d / duration=%s",
			displayProbeValue(dvdMediaInfoResult.ProbePath),
			displayProbeValue(dvdMediaInfoResult.SelectedVOBPath),
			len(dvdMediaInfo),
			secToHMS(dvdMediaInfoResult.Duration),
		)
	}
}

// resolveRelativeSubtitleIndex 把 ffprobe 的绝对流索引转换成 ffmpeg 需要的相对字幕序号。
func (r *screenshotRunner) resolveRelativeSubtitleIndex(input string, streamIndex int) (int, error) {
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

// probeSubtitleTracks 调用 ffprobe 读取字幕轨，并归一化语言、标题和 disposition 信息。
func (r *screenshotRunner) probeSubtitleTracks(input string, extraArgs ...string) ([]subtitleTrack, error) {
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

	var payload ffprobeStreamsPayload
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return nil, err
	}

	tracks := make([]subtitleTrack, 0, len(payload.Streams))
	for _, stream := range payload.Streams {
		tracks = append(tracks, subtitleTrack{
			Index:     stream.Index,
			StreamID:  jsonString(stream.ID),
			Codec:     strings.ToLower(strings.TrimSpace(stream.CodecName)),
			Language:  strings.ToLower(strings.TrimSpace(firstSubtitleLanguage(stream.Tags))),
			Title:     strings.TrimSpace(firstSubtitleTitle(stream.Tags)),
			Forced:    stream.Disposition.Forced,
			IsDefault: stream.Disposition.Default,
			Tags:      subtitleTagsSummary(stream.Tags),
		})
	}
	return tracks, nil
}

// logSubtitleFallback 会记录字幕回退，方便排查当前执行过程中的关键状态。
func (r *screenshotRunner) logSubtitleFallback(modeLabel string) {
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

// logSelectedSubtitleSummary 记录最终选中的字幕来源、格式和渲染方式。
func (r *screenshotRunner) logSelectedSubtitleSummary() {
	if r.subtitle.Mode == "none" {
		return
	}

	source := "外挂"
	render := "直接使用外挂文件"
	if r.subtitle.ExtractedText {
		source = "内封"
		render = "提取内挂文字字幕后按外挂文件渲染"
	} else if r.subtitle.Mode == "internal" {
		source = "内封"
		render = "直接使用内封轨道"
	}

	r.logf("[字幕格式] 来源：%s | 格式：%s | 渲染：%s", source, subtitleFormatLabel(r.subtitle.Codec), render)
}

// subtitleProbeSource 返回 ffprobe 探测字幕轨时使用的输入路径。
func (r *screenshotRunner) subtitleProbeSource() string {
	return r.sourcePath
}

// dvdMediaInfoSource 返回 DVD MediaInfo 探测优先使用的输入路径。
func (r *screenshotRunner) dvdMediaInfoSource() string {
	if strings.TrimSpace(r.dvdMediaInfoPath) != "" {
		return r.dvdMediaInfoPath
	}
	return r.dvdProbeSource()
}

// dvdSelectedIFOPath 返回当前 DVD 选择对应的最佳 IFO 路径。
func (r *screenshotRunner) dvdSelectedIFOPath() string {
	resolved, ok := dvdMediaInfoIFOPath(r.dvdProbeSource())
	if ok {
		return resolved
	}
	resolved, ok = dvdMediaInfoIFOPath(r.dvdMediaInfoSource())
	if ok {
		return resolved
	}
	return r.dvdMediaInfoSource()
}

// dvdSelectedVOBPath 返回当前 DVD 选择对应的最佳标题 VOB 路径。
func (r *screenshotRunner) dvdSelectedVOBPath() string {
	resolved, ok := dvdMediaInfoTitleVOBPath(r.dvdProbeSource())
	if ok {
		return resolved
	}
	resolved, ok = dvdMediaInfoTitleVOBPath(r.dvdMediaInfoSource())
	if ok {
		return resolved
	}
	return ""
}

// dvdProbeSource 返回 DVD 字幕探测时使用的基础输入路径。
func (r *screenshotRunner) dvdProbeSource() string {
	return r.sourcePath
}
