package utils

import (
	"errors"
	"io"
)

// note: net.TCPConn and pel.PktEncLayer implements DuplexStreamEx
type DuplexStreamEx interface {
	io.Reader
	io.Writer
	io.Closer
	CloseWrite() error
}

type dseWrapper struct {
	r io.Reader
	w io.WriteCloser
}

func (d *dseWrapper) Read(p []byte) (n int, err error) {
	return d.r.Read(p)
}

func (d *dseWrapper) Write(p []byte) (n int, err error) {
	return d.w.Write(p)
}

func (d *dseWrapper) Close() error {
	err := d.w.Close()
	if r, ok := d.r.(io.Closer); ok {
		err = errors.Join(err, r.Close())
	}
	return err
}

func (d *dseWrapper) CloseWrite() error {
	return d.w.Close()
}

func DSEFromRW(r io.Reader, w io.WriteCloser) DuplexStreamEx {
	return &dseWrapper{r: r, w: w}
}
