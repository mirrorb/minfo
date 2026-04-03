package handlers

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"minfo/internal/config"
	"minfo/internal/httpapi/logstream"
	"minfo/internal/httpapi/transport"
	"minfo/internal/media"
	"minfo/internal/system"
)

func MediaInfoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !transport.EnsurePost(w, r) {
			return
		}
		if err := transport.ParseForm(w, r); err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer transport.CleanupMultipart(r)

		logger := newInfoLogger(logstream.Open(r.FormValue("log_session")))
		defer logger.Close()

		path, cleanup, err := transport.InputPath(r)
		if err != nil {
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		defer cleanup()
		logger.Logf("[mediainfo] 输入路径: %s", path)

		bin, err := system.ResolveBin(envKey, fallback)
		if err != nil {
			logger.Logf("[mediainfo] 未找到可执行文件: %s", err.Error())
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		logger.Logf("[mediainfo] 使用命令: %s", bin)

		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()

		candidates, sourceCleanup, err := media.ResolveMediaInfoCandidates(ctx, path, media.MediaInfoCandidateLimit)
		if err != nil {
			logger.Logf("[mediainfo] 解析候选源失败: %s", err.Error())
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		defer sourceCleanup()
		logger.Logf("[mediainfo] 候选源数量: %d", len(candidates))

		var lastErr string
		for idx, sourcePath := range candidates {
			sourceDir := filepath.Dir(sourcePath)
			sourceName := filepath.Base(sourcePath)
			logger.Logf("[mediainfo] 尝试 %d/%d: %s", idx+1, len(candidates), sourcePath)
			logger.Logf("[mediainfo] 执行命令: cwd=%s | %s", sourceDir, formatCommand(bin, sourceName))

			stdout, stderr, err := system.RunCommandInDirLive(ctx, sourceDir, bin, logger.CommandOutput("mediainfo"), sourceName)
			if err != nil {
				lastErr = system.BestErrorMessage(err, stderr, stdout)
				logger.LogMultiline("[mediainfo][error] ", lastErr)
				continue
			}

			output := system.CombineCommandOutput(stdout, stderr)
			if output == "" {
				lastErr = fmt.Sprintf("mediainfo returned empty output for: %s", sourcePath)
				logger.Logf("[mediainfo] 返回空输出: %s", sourcePath)
				continue
			}

			logger.Logf("[mediainfo] 完成: %s", sourcePath)
			transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{
				OK:     true,
				Output: output,
				Logs:   logger.String(),
			})
			return
		}

		if lastErr == "" {
			lastErr = "mediainfo returned empty output"
		}
		writeInfoError(w, http.StatusInternalServerError, lastErr, logger)
	}
}

func BDInfoHandler(envKey, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !transport.EnsurePost(w, r) {
			return
		}
		if err := transport.ParseForm(w, r); err != nil {
			transport.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		defer transport.CleanupMultipart(r)

		logger := newInfoLogger(logstream.Open(r.FormValue("log_session")))
		defer logger.Close()

		path, cleanup, err := transport.InputPath(r)
		if err != nil {
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		defer cleanup()
		logger.Logf("[bdinfo] 输入路径: %s", path)

		bin, err := system.ResolveBin(envKey, fallback)
		if err != nil {
			logger.Logf("[bdinfo] 未找到可执行文件: %s", err.Error())
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		logger.Logf("[bdinfo] 使用命令: %s", bin)

		ctx, cancel := context.WithTimeout(r.Context(), config.RequestTimeout)
		defer cancel()

		bdPath, bdCleanup, err := media.ResolveBDInfoSource(ctx, path)
		if err != nil {
			logger.Logf("[bdinfo] 解析源路径失败: %s", err.Error())
			writeInfoError(w, http.StatusBadRequest, err.Error(), logger)
			return
		}
		defer bdCleanup()
		logger.Logf("[bdinfo] 实际检测路径: %s", bdPath)

		logger.Logf("[bdinfo] 执行命令: %s", formatCommand(bin, bdPath))
		stdout, stderr, err := system.RunCommandLive(ctx, bin, logger.CommandOutput("bdinfo"), bdPath)
		if err != nil {
			lastErr := system.BestErrorMessage(err, stderr, stdout)
			logger.LogMultiline("[bdinfo][error] ", lastErr)
			writeInfoError(w, http.StatusInternalServerError, lastErr, logger)
			return
		}

		output := system.CombineCommandOutput(stdout, stderr)
		if shouldExtractBDInfoCode(r.FormValue("bdinfo_mode")) {
			logger.Logf("[bdinfo] 输出模式: 精简报告")
			output = extractBDInfoCodeBlock(output)
		} else {
			logger.Logf("[bdinfo] 输出模式: 完整报告")
		}

		logger.Logf("[bdinfo] 完成: %s", bdPath)
		transport.WriteJSON(w, http.StatusOK, transport.InfoResponse{
			OK:     true,
			Output: output,
			Logs:   logger.String(),
		})
	}
}

func shouldExtractBDInfoCode(mode string) bool {
	return strings.TrimSpace(strings.ToLower(mode)) != "full"
}

func extractBDInfoCodeBlock(output string) string {
	matches := regexp.MustCompile(`(?is)\[code\](.*?)\[/code\]`).FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return output
	}

	best := ""
	bestScore := -1
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		block := strings.TrimSpace(match[1])
		if block == "" {
			continue
		}

		score := len(block)
		if strings.Contains(strings.ToUpper(block), "DISC INFO:") {
			score += 1_000_000
		}

		if score > bestScore {
			best = block
			bestScore = score
		}
	}

	if best == "" {
		return output
	}
	return best
}

type infoLogger struct {
	session *logstream.Session
	lines   []timedLogLine
}

type timedLogLine struct {
	timestamp string
	message   string
}

func newInfoLogger(session *logstream.Session) *infoLogger {
	return &infoLogger{
		session: session,
		lines:   make([]timedLogLine, 0, 32),
	}
}

func (l *infoLogger) Logf(format string, args ...any) {
	if l == nil {
		return
	}
	timestamp := time.Now().Format("15:04:05")
	line := fmt.Sprintf(format, args...)
	l.lines = append(l.lines, timedLogLine{
		timestamp: timestamp,
		message:   line,
	})
	if l.session != nil {
		l.session.Publish(line)
	}
}

func (l *infoLogger) LogLine(line string) {
	if l == nil {
		return
	}
	l.Logf("%s", line)
}

func (l *infoLogger) LogMultiline(prefix, text string) {
	if l == nil {
		return
	}
	for _, line := range splitLogLines(text) {
		if prefix == "" {
			l.Logf("%s", line)
			continue
		}
		l.Logf("%s%s", prefix, line)
	}
}

func (l *infoLogger) CommandOutput(scope string) system.OutputLineHandler {
	return func(stream, line string) {
		l.Logf("[%s][%s] %s", scope, stream, line)
	}
}

func (l *infoLogger) String() string {
	if l == nil || len(l.lines) == 0 {
		return ""
	}

	formatted := make([]string, 0, len(l.lines))
	for _, line := range l.lines {
		if line.timestamp == "" {
			formatted = append(formatted, line.message)
			continue
		}
		formatted = append(formatted, fmt.Sprintf("[%s] %s", line.timestamp, line.message))
	}
	return strings.Join(formatted, "\n")
}

func (l *infoLogger) Close() {
	if l == nil || l.session == nil {
		return
	}
	l.session.Close()
}

func writeInfoError(w http.ResponseWriter, status int, message string, logger *infoLogger) {
	transport.WriteJSON(w, status, transport.InfoResponse{
		OK:    false,
		Error: message,
		Logs:  logger.String(),
	})
}

func splitLogLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if normalized == "" {
		return []string{""}
	}
	return strings.Split(normalized, "\n")
}

func formatCommand(bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteArg(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n\"'\\") {
		return strconv.Quote(value)
	}
	return value
}
