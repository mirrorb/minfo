// Package media 提供 ISO 挂载、UDF 模块处理与清理逻辑。

package media

import (
	"context"
	"fmt"
	"os"
	"strings"

	"minfo/internal/config"
	"minfo/internal/system"
)

// mountISO 将 ISO 只读挂载到临时目录，并返回挂载点和清理函数。
func mountISO(ctx context.Context, isoPath string) (string, func(), error) {
	mountBin, err := system.ResolveBin(system.MountBinaryPath)
	if err != nil {
		return "", func() {}, err
	}
	umountBin, err := system.ResolveBin(system.UmountBinaryPath)
	if err != nil {
		return "", func() {}, err
	}

	mountDir, err := os.MkdirTemp("", "minfo-iso-mount-*")
	if err != nil {
		return "", func() {}, err
	}

	mountCtx, cancel := context.WithTimeout(ctx, config.MountTimeout)
	defer cancel()

	modErr := LoadUDFModule(mountCtx)
	_, stderr, err := system.RunCommand(mountCtx, mountBin, "-o", "loop,ro", isoPath, mountDir)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		if isUnknownUDFMountError(msg) {
			if retryModErr := LoadUDFModule(mountCtx); retryModErr == nil {
				_, retryStderr, retryErr := system.RunCommand(mountCtx, mountBin, "-o", "loop,ro", isoPath, mountDir)
				if retryErr == nil {
					return mountDir, buildMountCleanup(mountDir, umountBin), nil
				}
				retryMsg := strings.TrimSpace(retryStderr)
				if retryMsg == "" {
					retryMsg = retryErr.Error()
				}
				_ = os.RemoveAll(mountDir)
				return "", func() {}, fmt.Errorf("mount iso failed after modprobe udf: %s", retryMsg)
			}
		}
		_ = os.RemoveAll(mountDir)
		return "", func() {}, fmt.Errorf("mount iso failed: %s", explainISOmountError(msg, modErr))
	}

	return mountDir, buildMountCleanup(mountDir, umountBin), nil
}

// explainISOmountError 补充 UDF 内核模块相关上下文，使挂载错误更容易定位。
func explainISOmountError(message string, modErr error) string {
	if isUnknownUDFMountError(message) {
		if modErr != nil {
			return message + "; auto `modprobe udf` failed: " + modErr.Error() + ". Ensure host supports udf and mount `/lib/modules:/lib/modules:ro` into container"
		}
		return message + "; attempted auto `modprobe udf`, please check host kernel module availability"
	}
	return message
}

// isUnknownUDFMountError 会判断 Unknown UDF 文件系统挂载错误是否满足当前条件。
func isUnknownUDFMountError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "unknown filesystem type 'udf'") || strings.Contains(lower, "unknown filesystem type \"udf\"")
}

// LoadUDFModule 尝试通过 modprobe 加载 UDF 内核模块。
func LoadUDFModule(ctx context.Context) error {
	modprobeBin, err := system.ResolveBin(system.ModprobeBinaryPath)
	if err != nil {
		return err
	}
	_, stderr, err := system.RunCommand(ctx, modprobeBin, "udf")
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("modprobe udf failed: %s", msg)
	}
	return nil
}

// buildMountCleanup 返回一个清理函数，用于卸载 ISO 挂载点并删除临时目录。
func buildMountCleanup(mountDir, umountBin string) func() {
	return func() {
		umountCtx, cancel := context.WithTimeout(context.Background(), config.UmountTimeout)
		defer cancel()
		if _, _, err := system.RunCommand(umountCtx, umountBin, mountDir); err != nil {
			_, _, _ = system.RunCommand(umountCtx, umountBin, "-l", mountDir)
		}
		_ = os.RemoveAll(mountDir)
	}
}
