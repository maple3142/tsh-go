package protocol

import (
	"encoding/binary"
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

const (
	statusOK    byte = 0
	statusError byte = 1
)

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

// request protocol: 1 byte mode

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

// status signaling protocol: 1 byte status code
// 0 = OK, 1 = error
// if error, followed by 2 bytes little-endian length and error message

func WriteStatusOK(w io.Writer) error {
	_, err := w.Write([]byte{statusOK})
	return err
}

func WriteStatusError(w io.Writer, err error) error {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return WriteStatusErrorString(w, message)
}

func WriteStatusErrorString(w io.Writer, message string) error {
	if len(message) > 0xffff {
		message = message[:0xffff]
	}
	buf := make([]byte, 3+len(message))
	buf[0] = statusError
	binary.LittleEndian.PutUint16(buf[1:3], uint16(len(message)))
	copy(buf[3:], message)
	_, err := w.Write(buf)
	return err
}

func ReadStatus(r io.Reader) error {
	var status [1]byte
	if _, err := io.ReadFull(r, status[:]); err != nil {
		return err
	}
	switch status[0] {
	case statusOK:
		return nil
	case statusError:
		var lengthBuf [2]byte
		if _, err := io.ReadFull(r, lengthBuf[:]); err != nil {
			return err
		}
		length := int(binary.LittleEndian.Uint16(lengthBuf[:]))
		message := make([]byte, length)
		if _, err := io.ReadFull(r, message); err != nil {
			return err
		}
		if len(message) == 0 {
			return fmt.Errorf("remote error")
		}
		return fmt.Errorf("remote error: %s", string(message))
	default:
		return fmt.Errorf("unknown protocol status: %d", status[0])
	}
}
