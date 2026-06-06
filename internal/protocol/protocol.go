package protocol

import (
	"fmt"
	"io"
)

type Mode uint8

const (
	Kill Mode = iota
	GetFile
	PutFile
	RunShell
	RunShellNoTTY
	SOCKS5
	Pipe
)

type Request struct {
	Mode Mode
}

func (m Mode) Valid() bool {
	switch m {
	case Kill, GetFile, PutFile, RunShell, RunShellNoTTY, SOCKS5, Pipe:
		return true
	default:
		return false
	}
}

func (m Mode) String() string {
	switch m {
	case Kill:
		return "kill"
	case GetFile:
		return "get_file"
	case PutFile:
		return "put_file"
	case RunShell:
		return "run_shell"
	case RunShellNoTTY:
		return "run_shell_no_tty"
	case SOCKS5:
		return "socks5"
	case Pipe:
		return "pipe"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(m))
	}
}

func ParseMode(b byte) (Mode, error) {
	mode := Mode(b)
	if !mode.Valid() {
		return 0, fmt.Errorf("unknown protocol mode: %d", b)
	}
	return mode, nil
}

func WriteRequest(w io.Writer, req Request) error {
	if !req.Mode.Valid() {
		return fmt.Errorf("unknown protocol mode: %d", uint8(req.Mode))
	}
	_, err := w.Write([]byte{byte(req.Mode)})
	return err
}

func ReadRequest(r io.Reader) (Request, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return Request{}, err
	}
	mode, err := ParseMode(buf[0])
	if err != nil {
		return Request{}, err
	}
	return Request{Mode: mode}, nil
}
