package utils

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// wrap buffer to add a no-op Close, implement WriteCloser
type bufferWriteCloser struct {
	*bytes.Buffer
}

func (w bufferWriteCloser) Close() error {
	return nil
}

// for simulating read errors
type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

// for simulating blocking reader
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) {
	select {}
}

// for tracking whether Close was called on a reader
type closeTrackingReader struct {
	closed bool
}

func (r *closeTrackingReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (r *closeTrackingReader) Close() error {
	r.closed = true
	return nil
}

func TestDuplexPipeCopiesBothDirections(t *testing.T) {
	var localOut bytes.Buffer
	var remoteOut bytes.Buffer
	local := DSEFromRW(strings.NewReader("local to remote"), bufferWriteCloser{&localOut})
	remote := DSEFromRW(strings.NewReader("remote to local"), bufferWriteCloser{&remoteOut})

	if err := DuplexPipe(local, remote, make([]byte, 4), make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if got, want := localOut.String(), "remote to local"; got != want {
		t.Fatalf("local output = %q, want %q", got, want)
	}
	if got, want := remoteOut.String(), "local to remote"; got != want {
		t.Fatalf("remote output = %q, want %q", got, want)
	}
}

func TestDuplexPipeReturnsCopyErrors(t *testing.T) {
	wantErr := errors.New("read failed")
	local := DSEFromRW(strings.NewReader("local to remote"), bufferWriteCloser{&bytes.Buffer{}})
	remote := DSEFromRW(errReader{err: wantErr}, bufferWriteCloser{&bytes.Buffer{}})

	err := DuplexPipe(local, remote, make([]byte, 4), make([]byte, 4))
	if !errors.Is(err, wantErr) {
		t.Fatalf("DuplexPipe() error = %v, want %v", err, wantErr)
	}
}

func TestDuplexPipeReturnsWhenOppositeReaderDoesNotUnblock(t *testing.T) {
	local := DSEFromRW(blockingReader{}, bufferWriteCloser{&bytes.Buffer{}})
	remote := DSEFromRW(strings.NewReader("remote done"), bufferWriteCloser{&bytes.Buffer{}})

	start := time.Now()
	if err := DuplexPipe(local, remote, make([]byte, 4), make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("DuplexPipe took too long to return for blocking reader: %s", elapsed)
	}
}

func TestDuplexPipeTreatsPtyEIOAsExpectedClose(t *testing.T) {
	local := DSEFromRW(strings.NewReader(""), bufferWriteCloser{&bytes.Buffer{}})
	remote := DSEFromRW(errReader{err: &os.PathError{Op: "read", Path: "/dev/ptmx", Err: syscall.EIO}}, bufferWriteCloser{&bytes.Buffer{}})

	if err := DuplexPipe(local, remote, make([]byte, 4), make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
}

func TestDSEFromRWCloseClosesReaderWhenPossible(t *testing.T) {
	reader := &closeTrackingReader{}
	stream := DSEFromRW(reader, bufferWriteCloser{&bytes.Buffer{}})

	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if !reader.closed {
		t.Fatal("Close did not close reader")
	}
}
