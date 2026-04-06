// Package screenshot 提供截图引擎入口和运行器定义。

package screenshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"minfo/internal/media"
	"minfo/internal/system"
)

const (
	defaultSubtitleDuration = 4.0
	subtitleSnapEpsilon     = 0.50
	playlistScanMax         = 6
	oversizeBytes           = 10 * 1024 * 1024
)

var (
	langZHHansTokens = []string{"简体", "简中", "chs", "zh-hans", "zh_hans", "zh-cn", "zh_cn"}
	langZHHantTokens = []string{"繁体", "繁中", "cht", "big5", "zh-hant", "zh_hant", "zh-tw", "zh_tw"}
	langZHTokens     = []string{"中文", "chinese", "zho", "chi", "zh"}
	langENTokens     = []string{"en", "eng", "english"}

	clipIDPattern = regexp.MustCompile(`[0-9]{5}M2TS`)
)

type variantSettings struct {
	Ext            string
	ProbeSize      string
	Analyze        string
	CoarseBackText int
	CoarseBackPGS  int
	SearchBack     float64
	SearchForward  float64
	JPGQuality     int
}

type bitmapSubtitleKind string

const (
	bitmapSubtitleNone bitmapSubtitleKind = ""
	bitmapSubtitlePGS  bitmapSubtitleKind = "pgs"
	bitmapSubtitleDVD  bitmapSubtitleKind = "dvd"
)

type subtitleSelection struct {
	Mode          string
	File          string
	StreamIndex   int
	RelativeIndex int
	Lang          string
	Codec         string
	Title         string
	ExtractedText bool
}

type subtitleSpan struct {
	Start float64
	End   float64
}

type subtitleTrack struct {
	Index     int
	StreamID  string
	Codec     string
	Language  string
	Title     string
	Forced    int
	IsDefault int
	Tags      string
}

type blurayHelperTrack struct {
	PID          int    `json:"pid"`
	Lang         string `json:"lang"`
	CodingType   int    `json:"coding_type"`
	CharCode     int    `json:"char_code"`
	SubpathID    int    `json:"subpath_id"`
	PayloadBytes uint64 `json:"payload_bytes"`
	Bitrate      int64  `json:"bitrate"`
}

type blurayHelperResult struct {
	Source         string `json:"source"`
	BitrateScanned bool   `json:"bitrate_scanned"`
	BitrateMode    string `json:"bitrate_mode"`
	Clip           struct {
		ClipID        string              `json:"clip_id"`
		PGStreamCount int                 `json:"pg_stream_count"`
		PacketSeconds float64             `json:"packet_seconds"`
		PGStreams     []blurayHelperTrack `json:"pg_streams"`
	} `json:"clip"`
}

type blurayProbeContext struct {
	Root     string
	Playlist string
	Clip     string
}

type preferredSubtitleRank struct {
	LangClass        string
	LangScore        int
	DispositionScore int
	PID              int
	PIDOK            bool
	BitmapKind       bitmapSubtitleKind
	PayloadBytes     uint64
	UsePayloadBytes  bool
	Bitrate          int64
	UseBitrate       bool
}

type ffprobeStreamsPayload struct {
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

type ffprobePacketsPayload struct {
	Packets []ffprobePacket `json:"packets"`
}

type ffprobePacket struct {
	PTSTime      string `json:"pts_time"`
	DurationTime string `json:"duration_time"`
	Size         string `json:"size"`
}

type screenshotRunner struct {
	ctx              context.Context
	sourcePath       string
	dvdMediaInfoPath string
	outputDir        string
	variant          string
	subtitleMode     string
	requested        []float64
	settings         variantSettings
	ffmpegBin        string
	ffprobeBin       string
	mediainfoBin     string
	bdsubBin         string
	logLines         []string
	logHandler       LogHandler

	blurayContext            blurayProbeContext
	subtitle                 subtitleSelection
	subtitleIndex            []subtitleSpan
	rejectedBitmapCandidates map[string]struct{}
	tempSubtitleFile         string

	startOffset float64
	duration    float64
	videoWidth  int
	videoHeight int
	colorInfo   string
	colorChain  string
}

// runEngineScreenshotsWithLiveLogs 会解析输入源、生成随机时间点，并启动带实时日志的截图引擎流程。
func runEngineScreenshotsWithLiveLogs(ctx context.Context, inputPath, outputDir, variant, subtitleMode string, count int, onLog LogHandler) (ScreenshotsResult, error) {
	sourcePath, cleanup, err := media.ResolveScreenshotSource(ctx, inputPath)
	if err != nil {
		return ScreenshotsResult{}, err
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
		return ScreenshotsResult{}, err
	}

	return runScreenshotsFromSource(ctx, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode, timestamps, onLog)
}

// runScreenshotsFromSource 会基于已经解析好的媒体源创建运行器，并执行一次完整截图任务。
func runScreenshotsFromSource(ctx context.Context, sourcePath, dvdMediaInfoPath, outputDir, variant, subtitleMode string, timestamps []string, onLog LogHandler) (ScreenshotsResult, error) {
	runner := &screenshotRunner{
		ctx:              ctx,
		sourcePath:       sourcePath,
		dvdMediaInfoPath: dvdMediaInfoPath,
		outputDir:        outputDir,
		variant:          NormalizeVariant(variant),
		subtitleMode:     NormalizeSubtitleMode(subtitleMode),
		settings:         variantSettingsFor(variant),
		subtitle: subtitleSelection{
			Mode: "none",
		},
		logHandler: onLog,
	}
	defer runner.cleanupTemporarySubtitleFile()

	runner.logf("[信息] 已切换为 Go 截图引擎。")
	if looksLikeDVDSource(runner.dvdProbeSource()) {
		runner.logf("[信息] DVD 已选片段：VOB=%s | IFO=%s",
			displayProbeValue(runner.dvdSelectedVOBPath()),
			displayProbeValue(runner.dvdSelectedIFOPath()),
		)
	}

	if err := runner.init(timestamps); err != nil {
		return ScreenshotsResult{Logs: runner.logs()}, err
	}

	files, err := runner.run()
	if err != nil {
		return ScreenshotsResult{Logs: runner.logs()}, err
	}
	return ScreenshotsResult{Files: files, Logs: runner.logs()}, nil
}

// variantSettingsFor 会根据输出格式选择对应的探测、搜索和编码参数。
func variantSettingsFor(variant string) variantSettings {
	switch NormalizeVariant(variant) {
	case VariantJPG:
		return variantSettings{
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
		return variantSettings{
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

// init 会初始化截图运行器依赖、时间点、字幕状态和渲染参数。
func (r *screenshotRunner) init(timestamps []string) error {
	var err error

	r.ffmpegBin, err = system.ResolveBin(system.FFmpegBinaryPath)
	if err != nil {
		return err
	}
	r.ffprobeBin, err = system.ResolveBin(system.FFprobeBinaryPath)
	if err != nil {
		return err
	}
	if bin, binErr := system.ResolveBin(system.MediaInfoBinaryPath); binErr == nil {
		r.mediainfoBin = bin
	}
	if bin, binErr := system.ResolveBin(system.BDSubBinaryPath); binErr == nil {
		r.bdsubBin = bin
	}

	r.requested, err = parseRequestedTimestamps(timestamps)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(r.outputDir, 0o755); err != nil {
		return err
	}
	if err := clearDir(r.outputDir); err != nil {
		return err
	}

	if r.subtitleMode != SubtitleModeOff {
		r.prepareBlurayProbeContext()
	}
	r.chooseSubtitle()
	if err := r.prepareTextSubtitleRenderSource(); err != nil {
		return err
	}
	r.logSelectedSubtitleSummary()

	r.startOffset = r.detectStartOffset()
	r.duration, err = probeMediaDuration(r.ctx, r.ffprobeBin, r.sourcePath)
	if err != nil {
		return err
	}
	r.videoWidth, r.videoHeight = r.detectVideoDimensions()

	if r.variant == VariantPNG {
		r.colorInfo = r.detectColorspace()
		r.colorChain = buildColorspaceChain(r.colorInfo)
		if r.colorInfo != "" {
			r.logf("[信息] 检测到色彩空间：%s", strings.TrimSuffix(r.colorInfo, "|"))
		} else {
			r.logf("[信息] 无法检测色彩空间，将使用标准转换")
		}
	}

	r.logf("[信息] 容器起始偏移：%.3fs | 影片总时长：%s", r.startOffset, secToHMS(r.duration))
	return nil
}

// cleanupTemporarySubtitleFile 会在截图任务结束时清理提取出的临时字幕文件。
func (r *screenshotRunner) cleanupTemporarySubtitleFile() {
	if strings.TrimSpace(r.tempSubtitleFile) == "" {
		return
	}
	_ = os.Remove(r.tempSubtitleFile)
	r.tempSubtitleFile = ""
}

// run 会按请求时间点执行整轮截图流程，并汇总成功、失败和最终输出文件。
func (r *screenshotRunner) run() ([]string, error) {
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
					secToHMSMS(requested),
					secToHMSMS(candidate),
				)
			}
			aligned = candidate
		} else {
			r.logf("[提示] 请求 %s 对齐后未找到新的唯一秒，跳过该截图。", secToHMSMS(requested))
			continue
		}

		outputName := uniqueScreenshotName(aligned, r.settings.Ext, usedNames)
		outputPath := filepath.Join(r.outputDir, outputName)
		r.logf("[信息] 截图: 请求 %s → 对齐 %s → 输出 %s -> %s",
			secToHMSMS(requested),
			secToHMSMS(aligned),
			secToHMSMS(aligned),
			outputName,
		)

		if err := r.captureScreenshot(aligned, outputPath); err != nil {
			failures = append(failures, fmt.Sprintf("[失败] 文件: %s\n原因: %s", filepath.Base(outputPath), err.Error()))
			continue
		}
		usedSeconds[screenshotSecond(aligned)] = struct{}{}
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
