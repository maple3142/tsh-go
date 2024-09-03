package pty

import "io"

type PtyWrapper interface {
	StdIn() io.WriteCloser
	StdOut() io.ReadCloser
	Close()
}
