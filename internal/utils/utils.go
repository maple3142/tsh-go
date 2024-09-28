package utils

import (
	"errors"
	"io"
	"net"
	"tsh-go/internal/constants"
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

func StreamPipe(src io.Reader, dst io.WriteCloser, buf []byte) (int64, error) {
	/// just CopyBuffer, but left to right
	return CopyBuffer(dst, src, buf)
}

func DuplexPipe(localReader io.Reader, localWriter io.WriteCloser, remoteReader io.Reader, remoteWriter io.WriteCloser, bufLocal2Remote []byte, bufRemote2Local []byte) {
	// local refers to the connection that related to the client
	// remote refers to the target that the client wants to connect to
	if bufLocal2Remote == nil {
		bufLocal2Remote = make([]byte, constants.MaxMessagesize)
	}
	if bufRemote2Local == nil {
		bufRemote2Local = make([]byte, constants.MaxMessagesize)
	}

	ch := make(chan struct{})
	go func() {
		StreamPipe(remoteReader, localWriter, bufRemote2Local)
		// log.Println("remoteReader closed", time.Now())
		localWriter.Close()
		ch <- struct{}{}
	}()
	go func() {
		StreamPipe(localReader, remoteWriter, bufLocal2Remote)
		// log.Println("localReader closed", time.Now())
		// same as `nc -w ? ...` behavior
		remoteWriter.Close()
	}()
	<-ch
}

type tcpConnReadCloser struct {
	conn *net.TCPConn
}

func (t *tcpConnReadCloser) Read(p []byte) (n int, err error) {
	return t.conn.Read(p)
}

func (t *tcpConnReadCloser) Close() error {
	return t.conn.CloseRead()
}

type tcpConnWriteCloser struct {
	Conn *net.TCPConn
}

func (t *tcpConnWriteCloser) Write(p []byte) (n int, err error) {
	return t.Conn.Write(p)
}

func (t *tcpConnWriteCloser) Close() error {
	return t.Conn.CloseWrite()
}

func NewTCPConnReadCloser(conn *net.TCPConn) io.ReadCloser {
	return &tcpConnReadCloser{conn: conn}
}

func NewTCPConnWriteCloser(conn *net.TCPConn) io.WriteCloser {
	return &tcpConnWriteCloser{Conn: conn}
}
