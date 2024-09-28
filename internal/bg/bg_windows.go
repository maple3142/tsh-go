//go:build windows

package bg

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func RunInBackground() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:    windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		NoInheritHandles: true,
	}
	cmd.Env = append(os.Environ(), "TSH_RUNNING_AS_DAEMON=1")
	cmd.Start()
	cmd.Process.Release()
	return nil
}
