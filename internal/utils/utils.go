package utils

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
	"tsh-go/internal/constants"
)

var errInvalidWrite = errors.New("invalid write result")

const duplexPipeDrainTimeout = 10 * time.Millisecond

func CopyBuffer(dst io.Writer, src io.Reader, buf []byte) (written int64, err error) {
	// copied from https://cs.opensource.google/go/go/+/refs/tags/go1.23.0:src/io/io.go;l=407;drc=beea7c1ba6a93c2a2991e79936ac4050bae851c4
	// but this version ALWAYS use the provided buffer
	// which guarantees that it will not try to Read or Write more than the buffer size
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errInvalidWrite
				}
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}

func StreamPipe(src io.Reader, dst io.Writer, buf []byte) (int64, error) {
	/// just CopyBuffer, but left to right
	return CopyBuffer(dst, src, buf)
}

type duplexPipeResult struct {
	direction duplexPipeDirection
	err       error
}

type duplexPipeDirection uint8

const (
	duplexPipeRemoteToLocal duplexPipeDirection = iota
	duplexPipeLocalToRemote
)

func (d duplexPipeDirection) String() string {
	switch d {
	case duplexPipeRemoteToLocal:
		return "remote_to_local"
	case duplexPipeLocalToRemote:
		return "local_to_remote"
	default:
		return fmt.Sprintf("unknown_direction(%d)", uint8(d))
	}
}

func DuplexPipe(local, remote DuplexStreamEx, bufLocal2Remote, bufRemote2Local []byte) error {
	// local refers to the connection that related to the client
	// remote refers to the target that the client wants to connect to
	if bufLocal2Remote == nil {
		bufLocal2Remote = make([]byte, constants.MaxMessagesize)
	}
	if bufRemote2Local == nil {
		bufRemote2Local = make([]byte, constants.MaxMessagesize)
	}

	// start 2 goroutines to copy in both directions and collect their results
	results := make(chan duplexPipeResult, 2)
	go func() {
		_, copyErr := StreamPipe(remote, local, bufRemote2Local)
		closeErr := local.CloseWrite()
		results <- duplexPipeResult{
			direction: duplexPipeRemoteToLocal,
			err:       errors.Join(copyErr, closeErr),
		}
	}()
	go func() {
		_, copyErr := StreamPipe(local, remote, bufLocal2Remote)
		closeErr := remote.CloseWrite()
		results <- duplexPipeResult{
			direction: duplexPipeLocalToRemote,
			err:       errors.Join(copyErr, closeErr),
		}
	}()

	// we want preserve the original interactive-shell behavior:
	// once remote ends, the pipe is considered done even if the local input side is still open.
	var errs []error
	var localToRemoteDone bool
	for {
		result := <-results
		if result.err != nil && !isExpectedCloseError(result.err) {
			errs = append(errs, fmt.Errorf("%s: %w", result.direction, result.err))
		}
		if result.direction == duplexPipeRemoteToLocal {
			break
		}
		localToRemoteDone = true
	}

	// closing both sides gives the opposite copy goroutine a chance to unblock
	// prefer CloseRead on local when available so pending outbound data is not dropped before fully drained by peer
	closeReadOrClose(local)
	remote.Close()

	if !localToRemoteDone {
		// this fixes the case that user send Ctrl-D to signal EOF so remote closes, but local terminal might still open
		// sicne terminal reads do not always unblock on close
		// just wait briefly to collect real errors from ordinary streams or return after timeout

		// actually, closing fd in one thread does not unblock the read in another thread
		// this is a known linux issue
		select {
		case result := <-results:
			if result.err != nil && !isExpectedCloseError(result.err) {
				errs = append(errs, fmt.Errorf("%s: %w", result.direction, result.err))
			}
		case <-time.After(duplexPipeDrainTimeout):
		}
	}
	return errors.Join(errs...)
}

type closeReader interface {
	CloseRead() error
}

func closeReadOrClose(stream DuplexStreamEx) error {
	if stream, ok := stream.(closeReader); ok {
		return stream.CloseRead()
	}
	return stream.Close()
}

func isExpectedCloseError(err error) bool {
	// this function is used to filter out the expected errors that can happen during normal shutdown of the pipe
	// some streams report normal shutdown as close errors
	// e.g. PTYs commonly return EIO when the slave side exits (e.g. user send Ctrl-D in shell)
	return errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EIO)
}

func WriteVarLength(writer io.Writer, b []byte) error {
	length := len(b)
	if length > constants.MaxMessagesize-2 {
		return fmt.Errorf("message too long: %d", length)
	}
	buf := make([]byte, 2+length)
	binary.LittleEndian.PutUint16(buf, uint16(length))
	copy(buf[2:], b)
	_, err := writer.Write(buf)
	return err
}

func ReadVarLength(reader io.Reader, buf []byte) ([]byte, error) {
	if cap(buf) < 2 {
		buf = make([]byte, 2)
	}
	_, err := io.ReadFull(reader, buf[:2])
	if err != nil {
		return nil, err
	}
	length := int(binary.LittleEndian.Uint16(buf[:2]))
	if cap(buf) < length {
		buf = make([]byte, length)
	}
	_, err = io.ReadFull(reader, buf[:length])
	if err != nil {
		return nil, err
	}
	return buf[:length], nil
}

func KDF(secret []byte) []byte {
	// assuming that secret is not a short input like a password
	sum := sha256.Sum256(secret)
	return sum[:]
}
