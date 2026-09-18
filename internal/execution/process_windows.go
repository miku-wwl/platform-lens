//go:build windows

package execution

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func terminateProcessTree(cmd *exec.Cmd, force bool) {
	if cmd.Process == nil {
		return
	}
	args := []string{"/PID", strconv.Itoa(cmd.Process.Pid), "/T"}
	if force {
		args = append([]string{"/F"}, args...)
	}
	_ = exec.Command("taskkill", args...).Run()
}
