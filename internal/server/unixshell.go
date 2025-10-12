//go:build !windows
// +build !windows

package server

import "os/exec"

func runShellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}
