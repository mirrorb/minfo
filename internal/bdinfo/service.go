// Package bdinfo 负责调用 BDInfo 可执行文件并整理输出结果。

package bdinfo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"minfo/internal/media"
	"minfo/internal/system"
)

const defaultBinaryPath = system.BDInfoBinaryPath

// RunOptions 定义 BDInfo 执行过程中的可选日志回调。
type RunOptions struct {
	CommandOutput system.OutputLineHandler
	Logf          func(format string, args ...any)
}

// Result 表示一次 BDInfo 运行返回的最终结果。
type Result struct {
	ResolvedPath string
	Output       string
}

// Run 会执行完整的 BDInfo 处理流程，包括源路径准备、命令调用和报告整理。
func Run(ctx context.Context, inputPath string, options RunOptions) (Result, error) {
	resolved, cleanup, err := media.ResolveBDInfoSource(ctx, inputPath)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	options.logf("[bdinfo] 实际检测路径: %s", resolved.Path)
	if resolved.Playlist != "" {
		options.logf("[bdinfo] 指定 playlist: %s", resolved.Playlist)
	}

	binaryPath, err := resolveBinary()
	if err != nil {
		return Result{}, err
	}
	options.logf("[bdinfo] 使用命令: %s", binaryPath)

	staged, err := stageSource(ctx, resolved.Path, options)
	if err != nil {
		return Result{}, err
	}
	defer staged.cleanup()

	args, err := buildCommandArgs(staged.scanInput, staged.workDir, resolved.Playlist)
	if err != nil {
		return Result{}, err
	}
	options.logf("[bdinfo] 执行命令: cwd=%s | %s", staged.workDir, formatCommand(binaryPath, args...))

	stdout, stderr, err := system.RunCommandInDirLive(ctx, staged.workDir, binaryPath, options.CommandOutput, args...)
	if err != nil {
		return Result{}, fmt.Errorf(system.BestErrorMessage(err, stderr, stdout))
	}

	reportPath, err := findReportFile(staged.workDir)
	if err != nil {
		message := strings.TrimSpace(system.CombineCommandOutput(stdout, stderr))
		if message != "" {
			return Result{}, fmt.Errorf("%s\n\n%s", err.Error(), message)
		}
		return Result{}, err
	}
	options.logf("[bdinfo] 输出报告: %s", reportPath)

	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		return Result{}, err
	}

	output := SelectLongestPlaylistBlock(string(reportBytes))
	if strings.TrimSpace(output) == "" {
		output = string(reportBytes)
	}

	return Result{
		ResolvedPath: resolved.Path,
		Output:       output,
	}, nil
}

// logf 会在可选日志回调存在时写入一条格式化日志。
func (o RunOptions) logf(format string, args ...any) {
	if o.Logf == nil {
		return
	}
	o.Logf(format, args...)
}

// resolveBinary 按环境变量和默认目录查找可用的 BDInfo 可执行文件。
func resolveBinary() (string, error) {
	binaryPath, err := system.ResolveBin(defaultBinaryPath)
	if err != nil {
		return "", errors.New("bdinfo: BDInfo binary not found under /usr/local/bin")
	}
	return binaryPath, nil
}
