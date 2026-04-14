// Package system 提供外部命令执行和实时输出转发能力。

package system

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const (
	FFmpegBinaryPath    = "/usr/bin/ffmpeg"
	FFprobeBinaryPath   = "/usr/bin/ffprobe"
	MediaInfoBinaryPath = "/usr/bin/mediainfo"
	OxiPNGBinaryPath    = "/usr/bin/oxipng"
	PNGQuantBinaryPath  = "/usr/bin/pngquant"
	BDInfoBinaryPath    = "/usr/local/bin/bdinfo"
	BDSubBinaryPath     = "/usr/local/bin/bdsub"
	MountBinaryPath     = "/bin/mount"
	UmountBinaryPath    = "/bin/umount"
	ModprobeBinaryPath  = "/sbin/modprobe"
)

// ResolveBin 会校验固定路径的可执行文件当前可用，并返回该固定路径。
func ResolveBin(path string) (string, error) {
	if _, err := exec.LookPath(path); err != nil {
		return "", fmt.Errorf("%s not found", path)
	}
	return path, nil
}

// RunCommand 会在默认工作目录中执行外部命令，并返回完整 stdout、stderr 和错误状态。
func RunCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
	return runCommand(ctx, "", bin, args...)
}

// RunCommandInDir 会在指定目录中执行外部命令，并返回完整 stdout、stderr 和错误状态。
func RunCommandInDir(ctx context.Context, dir, bin string, args ...string) (string, string, error) {
	return runCommand(ctx, dir, bin, args...)
}

// OutputLineHandler 处理实时命令输出的单行内容。
type OutputLineHandler func(stream, line string)

// RunCommandLive 会执行外部命令，并把输出按行实时转发给调用方。
func RunCommandLive(ctx context.Context, bin string, onLine OutputLineHandler, args ...string) (string, string, error) {
	return runCommandLive(ctx, "", bin, onLine, args...)
}

// RunCommandInDirLive 会在指定目录中执行外部命令，并把输出按行实时转发给调用方。
func RunCommandInDirLive(ctx context.Context, dir, bin string, onLine OutputLineHandler, args ...string) (string, string, error) {
	return runCommandLive(ctx, dir, bin, onLine, args...)
}

// runCommand 在指定目录启动命令，并完整收集 stdout 和 stderr。
func runCommand(ctx context.Context, dir, bin string, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	setCommandProcessGroup(cmd)

	stdoutFile, err := os.CreateTemp("", "minfo-stdout-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(stdoutFile.Name())
	defer stdoutFile.Close()

	stderrFile, err := os.CreateTemp("", "minfo-stderr-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(stderrFile.Name())
	defer stderrFile.Close()

	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		killCommandProcessGroup(cmd)
		waitErr = ctx.Err()
		<-waitCh
	}

	stdoutData, _ := os.ReadFile(stdoutFile.Name())
	stderrData, _ := os.ReadFile(stderrFile.Name())
	return string(stdoutData), string(stderrData), waitErr
}

// runCommandLive 在命令运行过程中按行转发输出，同时保留完整 stdout 和 stderr。
func runCommandLive(ctx context.Context, dir, bin string, onLine OutputLineHandler, args ...string) (string, string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	setCommandProcessGroup(cmd)

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	stdoutWriter := io.Writer(&stdoutBuf)
	stderrWriter := io.Writer(&stderrBuf)

	var stdoutRelay *lineRelayWriter
	var stderrRelay *lineRelayWriter
	if onLine != nil {
		stdoutRelay = newLineRelayWriter("stdout", onLine)
		stderrRelay = newLineRelayWriter("stderr", onLine)
		stdoutWriter = io.MultiWriter(&stdoutBuf, stdoutRelay)
		stderrWriter = io.MultiWriter(&stderrBuf, stderrRelay)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		killCommandProcessGroup(cmd)
		waitErr = ctx.Err()
		<-waitCh
	}

	if stdoutRelay != nil {
		stdoutRelay.Flush()
	}
	if stderrRelay != nil {
		stderrRelay.Flush()
	}

	return stdoutBuf.String(), stderrBuf.String(), waitErr
}

type lineRelayWriter struct {
	mu     sync.Mutex
	stream string
	onLine OutputLineHandler
	buffer bytes.Buffer
}

// newLineRelayWriter 创建一个按行拆分并转发输出的 Writer。
func newLineRelayWriter(stream string, onLine OutputLineHandler) *lineRelayWriter {
	return &lineRelayWriter{
		stream: stream,
		onLine: onLine,
	}
}

// Write 把命令输出写入缓冲区，并在读到完整行时立即回调。
func (w *lineRelayWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.buffer.Write(p); err != nil {
		return 0, err
	}
	w.flushCompleteLinesLocked()
	return len(p), nil
}

// Flush 会刷新缓冲内容，确保待发送数据及时写出。
func (w *lineRelayWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	remaining := strings.TrimRight(w.buffer.String(), "\r\n")
	if remaining != "" || w.buffer.Len() > 0 {
		w.onLine(w.stream, remaining)
	}
	w.buffer.Reset()
}

// flushCompleteLinesLocked 假定调用方已持锁，并把缓冲区中完整的行逐一回调出去。
func (w *lineRelayWriter) flushCompleteLinesLocked() {
	data := w.buffer.Bytes()
	start := 0
	for idx := 0; idx < len(data); idx++ {
		ch := data[idx]
		if ch != '\n' && ch != '\r' {
			continue
		}
		line := strings.TrimRight(string(data[start:idx]), "\r")
		w.onLine(w.stream, line)
		if ch == '\r' && idx+1 < len(data) && data[idx+1] == '\n' {
			start = idx + 2
			idx++
			continue
		}
		start = idx + 1
	}
	if start == 0 {
		return
	}
	w.buffer.Next(start)
}

// BestErrorMessage 组合 err、stderr 和 stdout，生成更适合展示给用户的错误文本。
func BestErrorMessage(err error, stderr, stdout string) string {
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		msg = err.Error()
	}
	if strings.TrimSpace(stdout) != "" {
		msg += "\n\n" + strings.TrimSpace(stdout)
	}
	return msg
}

// CombineCommandOutput 合并 stdout 和 stderr，并保留两者的可读分隔。
func CombineCommandOutput(stdout, stderr string) string {
	output := strings.TrimSpace(stdout)
	if strings.TrimSpace(stderr) != "" {
		if output != "" {
			output += "\n\n"
		}
		output += strings.TrimSpace(stderr)
	}
	return output
}
