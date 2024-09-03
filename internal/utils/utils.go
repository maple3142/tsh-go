package utils

import (
	"errors"
	"io"
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
	n, err := CopyBuffer(dst, src, buf)
	dst.Close()
	return n, err
}

func DuplexPipe(localReader io.Reader, localWriter io.WriteCloser, remote io.ReadWriteCloser, bufLocal2Remote []byte, bufRemote2Local []byte) {
	if bufLocal2Remote == nil {
		bufLocal2Remote = make([]byte, constants.MaxMessagesize)
	}
	if bufRemote2Local == nil {
		bufRemote2Local = make([]byte, constants.MaxMessagesize)
	}

	ch := make(chan struct{})
	go func() {
		StreamPipe(remote, localWriter, bufRemote2Local)
		ch <- struct{}{} // we can close once the remote connection is closed, no need to wait for local stream
	}()
	go StreamPipe(localReader, remote, bufLocal2Remote)
	<-ch
}
