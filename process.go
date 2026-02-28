//go:build linux

package main

import (
    "os/exec"
    "syscall"
    "time"
)

func setCommandProcessGroup(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
    }
}

func killCommandProcessGroup(cmd *exec.Cmd) {
    if cmd == nil || cmd.Process == nil {
        return
    }
    pid := cmd.Process.Pid
    if pid <= 0 {
        return
    }

    // Negative PID targets the whole process group.
    _ = syscall.Kill(-pid, syscall.SIGTERM)
    time.Sleep(300 * time.Millisecond)
    _ = syscall.Kill(-pid, syscall.SIGKILL)
}

