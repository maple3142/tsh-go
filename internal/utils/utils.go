package utils

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
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

func StreamPipe(src io.Reader, dst io.Writer, buf []byte) (int64, error) {
	/// just CopyBuffer, but left to right
	return CopyBuffer(dst, src, buf)
}

func DuplexPipe(local, remote DuplexStreamEx, bufLocal2Remote, bufRemote2Local []byte) {
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
		StreamPipe(remote, local, bufRemote2Local)
		// log.Println("remoteReader closed", time.Now())
		local.CloseWrite()
		ch <- struct{}{}
	}()
	go func() {
		StreamPipe(local, remote, bufLocal2Remote)
		// log.Println("localReader closed", time.Now())
		remote.CloseWrite()
	}()
	<-ch
	local.Close()
	remote.Close()
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
	return sha256.New().Sum(secret)
}
