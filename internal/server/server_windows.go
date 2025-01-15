//go:build windows
// +build windows

package server

import (
	"os/exec"

	"github.com/gonutz/ide/w32"
)

func init() {
	// hide console window
	console := w32.GetConsoleWindow()
	if console != 0 {
		_, consoleProcID := w32.GetWindowThreadProcessId(console)
		if w32.GetCurrentProcessId() == consoleProcID {
			w32.ShowWindowAsync(console, w32.SW_HIDE)
		}
	}
}

func runShellCommand(command string) *exec.Cmd {
	return exec.Command("cmd", "/c", command)
}
