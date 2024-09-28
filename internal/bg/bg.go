//go:build !windows

package bg

import (
	"os"
	"os/exec"
	"syscall"
)

func RunInBackground() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Env = append(os.Environ(), "TSH_RUNNING_AS_DAEMON=1")
	cmd.Start()
	return nil
}
