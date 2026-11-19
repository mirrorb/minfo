// Package bdinfo 负责调用 BDInfo 可执行文件并整理输出结果。

package bdinfo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"minfo/internal/media"
)

// buildCommandArgs 会按 BDInfoCLI 的位置参数规则构建命令参数。
func buildCommandArgs(scanInput, reportDir, playlist string) ([]string, error) {
	extraArgs, err := splitCommandArgs(strings.TrimSpace(os.Getenv("BDINFO_ARGS")))
	if err != nil {
		return nil, fmt.Errorf("invalid BDINFO_ARGS: %w", err)
	}

	args := make([]string, 0, len(extraArgs)+5)
	args = append(args, extraArgs...)
	selectionSpecified := hasPlaylistSelectionArg(extraArgs)
	if playlist != "" && !selectionSpecified {
		args = append(args, "-m", playlist)
		selectionSpecified = true
	}
	if !selectionSpecified {
		if bdmv := detectBDMVSource(scanInput); bdmv != "" {
			names, err := media.ListBDInfoPlaylists(filepath.Join(bdmv, "PLAYLIST"))
			if err != nil {
				return nil, fmt.Errorf("bdinfo: list playlists: %w", err)
			}
			if len(names) == 0 {
				return nil, errors.New("bdinfo: no playlists remain after filtering loops and playlists shorter than 20 seconds")
			}
			args = append(args, "-m", strings.Join(names, ","))
		} else {
			// BDInfoCLI 默认会进入交互式 playlist 选择；服务端场景没有 TTY，
			// 无可读取的 BDMV 目录时保留 whole-disc 回退。
			args = append(args, "-w")
		}
	}
	args = append(args, scanInput, reportDir)
	return args, nil
}

// hasPlaylistSelectionArg 判断额外参数里是否已经显式指定了 playlist 选择模式。
func hasPlaylistSelectionArg(args []string) bool {
	for _, arg := range args {
		switch {
		case arg == "-w", arg == "--whole":
			return true
		case arg == "-l", arg == "--list":
			return true
		case arg == "-m", arg == "--mpls":
			return true
		case strings.HasPrefix(arg, "-m="), strings.HasPrefix(arg, "--mpls="):
			return true
		}
	}
	return false
}

// splitCommandArgs 以接近 shell 的规则拆分 BDINFO_ARGS，并保留引号和转义语义。
func splitCommandArgs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	args := make([]string, 0, 8)
	var current strings.Builder
	var quote byte
	escaped := false

	flushCurrent := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for index := 0; index < len(raw); index++ {
		ch := raw[index]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		switch ch {
		case '\\':
			if quote == '\'' {
				current.WriteByte(ch)
				continue
			}
			escaped = true
		case '\'', '"':
			if quote == 0 {
				quote = ch
				continue
			}
			if quote == ch {
				quote = 0
				continue
			}
			current.WriteByte(ch)
		case ' ', '\t', '\n', '\r':
			if quote != 0 {
				current.WriteByte(ch)
				continue
			}
			flushCurrent()
		default:
			current.WriteByte(ch)
		}
	}

	if escaped {
		return nil, errors.New("dangling escape")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	flushCurrent()
	return args, nil
}

// formatCommand 把命令和参数拼成适合日志展示的可读字符串。
func formatCommand(bin string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

// quoteArg 会转义参数，避免命令或过滤器拼接时出现语义错误。
func quoteArg(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n\"'\\") {
		return strconvQuote(value)
	}
	return value
}

// strconvQuote 以 Go 风格的双引号字面量形式转义字符串。
func strconvQuote(value string) string {
	return fmt.Sprintf("%q", value)
}
