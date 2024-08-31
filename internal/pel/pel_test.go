package pel

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"
	"tsh-go/internal/constants"
)

const testSecret = "secrethaha"
const testAddress = "127.0.0.1:2333"

func TestProtocolBasic(t *testing.T) {
	data1 := make([]byte, constants.MaxMessagesize)
	rand.Read(data1)
	data2 := make([]byte, constants.MaxMessagesize)
	rand.Read(data2)
	listener, err := Listen(testAddress, testSecret, true)
	if err != nil {
		t.Fatal("Listen", err)
	}
	errs := make(chan error, 1)
	go func() {
		var conn *PktEncLayer
		conn, err = listener.Accept()
		if err != nil {
			errs <- fmt.Errorf("failed to accept connection: %v", err)
			return
		}
		recv1 := make([]byte, len(data1))
		_, err = conn.Read(recv1)
		if err != nil {
			errs <- fmt.Errorf("failed to read data 1: %v", err)
			return
		}
		recv2 := make([]byte, len(data2))
		_, err = conn.Read(recv2)
		if err != nil {
			errs <- fmt.Errorf("failed to read data 2: %v", err)
			return
		}
		conn.Close()
		if !bytes.Equal(recv1, data1) {
			errs <- fmt.Errorf("data 1 mismatch")
			return
		}
		if !bytes.Equal(recv2, data2) {
			errs <- fmt.Errorf("data 2 mismatch")
			return
		}
		errs <- nil
	}()
	conn, err := Dial(testAddress, testSecret, false)
	if err != nil {
		t.Fatal("Dial", err)
	}
	_, err = conn.Write(data1)
	if err != nil {
		t.Fatal("Write 1", err)
	}
	_, err = conn.Write(data2)
	if err != nil {
		t.Fatal("Write 2", err)
	}
	conn.Close()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}
