package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"minfo/internal/httpapi/transport"
)

var (
	mediainfoCandidatesPattern = regexp.MustCompile(`^\[mediainfo\] 候选源数量: (\d+)$`)
	mediainfoAttemptPattern    = regexp.MustCompile(`^\[mediainfo\] 尝试 (\d+)/(\d+): `)
	bdinfoScanProgressPattern  = regexp.MustCompile(`^\[bdinfo\]\[(?:stdout|stderr)\] Scanning\s+(\d+)%\s+-\s+(.+)$`)
)

// estimateInfoTaskRunningProgress 会根据任务种类分派到具体的信息任务进度估算器。
func estimateInfoTaskRunningProgress(kind string, entries []transport.LogEntry) *transport.TaskProgress {
	switch kind {
	case infoKindBDInfo:
		return estimateBDInfoRunningProgress(entries)
	case infoKindMediaInfo:
		fallthrough
	default:
		return estimateMediaInfoRunningProgress(entries)
	}
}

// estimateMediaInfoRunningProgress 会根据 MediaInfo 日志阶段估算当前运行进度。
func estimateMediaInfoRunningProgress(entries []transport.LogEntry) *transport.TaskProgress {
	totalCandidates := 0
	currentAttempt := 0
	seenInput := false
	seenBinary := false

	for _, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		switch {
		case strings.HasPrefix(line, "[mediainfo] 输入路径:"):
			seenInput = true
		case strings.HasPrefix(line, "[mediainfo] 使用命令:"):
			seenBinary = true
		case mediainfoCandidatesPattern.MatchString(line):
			matches := mediainfoCandidatesPattern.FindStringSubmatch(line)
			totalCandidates = parseInt(matches, 1)
		case mediainfoAttemptPattern.MatchString(line):
			matches := mediainfoAttemptPattern.FindStringSubmatch(line)
			currentAttempt = parseInt(matches, 1)
			if total := parseInt(matches, 2); total > 0 {
				totalCandidates = total
			}
		}
	}

	switch {
	case totalCandidates > 0 && currentAttempt > 0:
		processed := maxInt(currentAttempt-1, 0)
		percent := 34 + scaledProgress(processed, totalCandidates, 48)
		return progressSnapshot(percent, "分析媒体信息", fmt.Sprintf("正在处理候选源 %d/%d。", currentAttempt, totalCandidates), currentAttempt, totalCandidates, false)
	case totalCandidates > 0:
		return progressSnapshot(28, "准备候选源", fmt.Sprintf("已发现 %d 个候选源。", totalCandidates), 0, totalCandidates, false)
	case seenBinary:
		return progressSnapshot(18, "检查运行环境", "已找到 MediaInfo 可执行文件。", 0, 0, true)
	case seenInput:
		return progressSnapshot(10, "解析输入源", "正在准备候选媒体源。", 0, 0, true)
	default:
		return progressSnapshot(8, "启动中", "正在初始化 MediaInfo 任务。", 0, 0, true)
	}
}

// estimateBDInfoRunningProgress 会根据 BDInfo 扫描日志估算当前运行进度。
func estimateBDInfoRunningProgress(entries []transport.LogEntry) *transport.TaskProgress {
	seenResolvedPath := false
	seenBinary := false
	seenPreparedSource := false
	seenExec := false
	seenAnalyze := false
	seenScanStart := false
	scanPercent := 0
	scanDetail := ""
	seenGenerateReport := false
	seenReportSaved := false
	seenReport := false
	seenMode := false

	for _, entry := range entries {
		line := strings.TrimSpace(entry.Message)
		switch {
		case strings.HasPrefix(line, "[bdinfo] 实际检测路径:"):
			seenResolvedPath = true
		case strings.HasPrefix(line, "[bdinfo] 使用命令:"):
			seenBinary = true
		case strings.Contains(line, "包装 BDMV 根") || strings.Contains(line, "包装输入目录"):
			seenPreparedSource = true
		case strings.HasPrefix(line, "[bdinfo] 执行命令:"):
			seenExec = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Preparing to analyze the following:"):
			seenAnalyze = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Please wait while we scan the disc..."):
			seenScanStart = true
		case bdinfoScanProgressPattern.MatchString(line):
			matches := bdinfoScanProgressPattern.FindStringSubmatch(line)
			scanPercent = parseInt(matches, 1)
			scanDetail = strings.TrimSpace(matches[2])
			seenScanStart = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Please wait while we generate the report..."):
			seenGenerateReport = true
		case strings.HasPrefix(line, "[bdinfo][stdout] Report saved to:"):
			seenReportSaved = true
		case strings.HasPrefix(line, "[bdinfo] 输出报告:"):
			seenReport = true
		case strings.HasPrefix(line, "[bdinfo] 输出模式:"):
			seenMode = true
		}
	}

	switch {
	case seenMode:
		return progressSnapshot(98, "整理结果", "正在按所选模式整理报告内容。", 5, 5, false)
	case seenReport || seenReportSaved:
		return progressSnapshot(95, "读取报告", "已生成报告文件，正在读取结果。", 4, 5, false)
	case seenGenerateReport:
		return progressSnapshot(88, "生成报告", "BDInfo 已完成扫描，正在生成报告。", 4, 5, true)
	case scanPercent > 0:
		percent := 28.0 + scaledProgress(scanPercent, 100, 54)
		detail := "BDInfo 正在扫描目录内容。"
		if scanDetail != "" {
			detail = fmt.Sprintf("正在扫描蓝光目录：%s", scanDetail)
		}
		return progressSnapshot(percent, "扫描蓝光目录", detail, scanPercent, 100, false)
	case seenScanStart:
		return progressSnapshot(28, "扫描蓝光目录", "BDInfo 已启动扫描，正在读取蓝光文件。", 0, 100, true)
	case seenAnalyze:
		return progressSnapshot(24, "分析播放列表", "BDInfo 正在准备分析播放列表。", 2, 5, true)
	case seenExec:
		return progressSnapshot(22, "启动扫描", "BDInfo 命令已启动，等待扫描进度输出。", 2, 5, true)
	case seenPreparedSource:
		return progressSnapshot(16, "准备扫描目录", "已准备好 BDInfo 扫描目录。", 1, 5, false)
	case seenBinary:
		return progressSnapshot(10, "检查运行环境", "已找到 BDInfo 可执行文件。", 1, 5, false)
	case seenResolvedPath:
		return progressSnapshot(4, "解析输入源", "正在准备 BDInfo 实际检测路径。", 0, 5, true)
	default:
		return progressSnapshot(0, "启动中", "正在初始化 BDInfo 任务。", 0, 0, true)
	}
}
