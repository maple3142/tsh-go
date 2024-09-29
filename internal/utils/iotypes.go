package utils

import (
	"io"
	"net"
)

type DuplexStreamEx interface {
	io.Reader
	io.Writer
	io.Closer
	CloseRead() error
	CloseWrite() error
}

type DuplexStreamExReadCloser struct {
	s DuplexStreamEx
}

func (r *DuplexStreamExReadCloser) Read(p []byte) (n int, err error) {
	return r.s.Read(p)
}

func (r *DuplexStreamExReadCloser) Close() error {
	return r.s.CloseRead()
}

func DSE2ReadCloser(stream DuplexStreamEx) io.ReadCloser {
	return &DuplexStreamExReadCloser{s: stream}
}

type DuplexStreamExWriteCloser struct {
	s DuplexStreamEx
}

func (w *DuplexStreamExWriteCloser) Write(p []byte) (n int, err error) {
	return w.s.Write(p)
}

func (w *DuplexStreamExWriteCloser) Close() error {
	return w.s.CloseWrite()
}

func DSE2WriteCloser(stream DuplexStreamEx) io.WriteCloser {
	return &DuplexStreamExWriteCloser{s: stream}
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
