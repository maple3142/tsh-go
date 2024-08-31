package utils

import (
	"errors"
	"io"
	"time"
)

var errInvalidWrite = errors.New("invalid write result")

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

type TimeoutReader interface {
	ReadTimeout(p []byte, timeout time.Duration) (int, error)
}

// type DeadlineReader interface {
// 	SetReadDeadline(t time.Time) error
// 	Read(p []byte) (n int, err error)
// }

// func ReadTimeout(conn DeadlineReader, p []byte, timeout time.Duration) (int, error) {
// 	defer conn.SetReadDeadline(time.Time{})
// 	conn.SetReadDeadline(time.Now().Add(timeout))
// 	n, err := conn.Read(p)
// 	return n, err
// }

func CopyBufferTimeout(dst io.Writer, src TimeoutReader, buf []byte, timeout time.Duration) (written int64, err error) {
	for {
		nr, er := src.ReadTimeout(buf, timeout)
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

func StreamPipe(src io.Reader, dst io.WriteCloser, buf []byte) (int64, error) {
	n, err := CopyBuffer(dst, src, buf)
	dst.Close()
	return n, err
}

// func StreamPipeTimeout(src TimeoutReader, dst io.WriteCloser, buf []byte, timeout time.Duration) error {
// 	_, err := CopyBufferTimeout(dst, src, buf, timeout)
// 	if err != nil {
// 		dst.Close()
// 		return err
// 	}
// 	return nil
// }

// type MyNetConn struct {
// 	net.Conn
// }

// func (conn MyNetConn) ReadTimeout(p []byte, timeout time.Duration) (int, error) {
// 	defer conn.SetReadDeadline(time.Time{})
// 	conn.SetReadDeadline(time.Now().Add(timeout))
// 	return conn.Read(p)
// }
