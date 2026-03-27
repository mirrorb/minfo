package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"minfo/internal/config"
)

func ResolveBin(envKey, fallback string) (string, error) {
	bin := config.Getenv(envKey, fallback)
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("%s not found; set %s or add to PATH", bin, envKey)
	}
	return bin, nil
}

func RunCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
	return runCommand(ctx, "", bin, args...)
}

func RunCommandInDir(ctx context.Context, dir, bin string, args ...string) (string, string, error) {
	return runCommand(ctx, dir, bin, args...)
}

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
