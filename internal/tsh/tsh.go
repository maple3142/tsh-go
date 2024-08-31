package tsh

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"tsh-go/internal/constants"
	"tsh-go/internal/pel"
	"tsh-go/internal/utils"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/crypto/ssh/terminal"
)

func Run(secret string, host string, port int, mode uint8, args []string) {
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

	if isConnectBack {
		// connect back mode
		addr := fmt.Sprintf(":%d", port)
		ln, err := pel.Listen(addr, secret, false)
		if err != nil {
			fmt.Println("Address already in use.")
			os.Exit(0)
		}
		fmt.Print("Waiting for the server to connect...")
		layer, err := ln.Accept()
		ln.Close()
		if err != nil {
			fmt.Print("\nPassword: ")
			fmt.Scanln()
			fmt.Println("Authentication failed.")
			os.Exit(0)
		}
		fmt.Println("connected.")
		defer layer.Close()
		layer.Write([]byte{mode})
		switch mode {
		case constants.RunShell:
			handleRunShell(layer, command)
		case constants.GetFile:
			handleGetFile(layer, srcfile, dstdir)
		case constants.PutFile:
			handlePutFile(layer, srcfile, dstdir)
		}
	} else {
		addr := fmt.Sprintf("%s:%d", host, port)
		layer, err := pel.Dial(addr, secret, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to connect to %s:%d\n", host, port)
			fmt.Fprintf(os.Stderr, "It is possible that the server is not running or the secret is incorrect.\n")
			os.Exit(0)
		}
		defer layer.Close()
		layer.Write([]byte{mode})
		switch mode {
		case constants.RunShell:
			handleRunShell(layer, command)
		case constants.GetFile:
			handleGetFile(layer, srcfile, dstdir)
		case constants.PutFile:
			handlePutFile(layer, srcfile, dstdir)
		}
	}
}

func handleGetFile(layer *pel.PktEncLayer, srcfile, dstdir string) {
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

func handlePutFile(layer *pel.PktEncLayer, srcfile, dstdir string) {
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

func handleRunShell(layer *pel.PktEncLayer, command string) {
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
