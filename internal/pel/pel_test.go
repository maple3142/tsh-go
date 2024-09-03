package pel

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"
	"time"
	"tsh-go/internal/constants"
)

var testSecret = []byte("just some secret")

const testAddress = "127.0.0.1:2333"

func runProtocolTest(t *testing.T, fn func(*PktEncLayer, *PktEncLayer)) {
	listener, err := Listen(testAddress, testSecret, false)
	if err != nil {
		t.Fatal("listen", err)
	}
	var client, server *PktEncLayer
	ch := make(chan error, 1)
	go func() {
		server, err = listener.Accept()
		listener.Close()
		if err != nil {
			ch <- err
		}
		ch <- nil
	}()
	client, err = Dial(testAddress, testSecret, true)
	if err != nil {
		t.Fatal("dial", err)
	}
	err = <-ch
	if err != nil {
		t.Fatal(err)
	}
	fn(client, server)
}

func TestProtocolBasic(t *testing.T) {
	data1 := make([]byte, constants.MaxMessagesize)
	rand.Read(data1)
	data2 := make([]byte, constants.MaxMessagesize*3+1234)
	rand.Read(data2)
	runProtocolTest(t, func(client, server *PktEncLayer) {
		client.Write(data1)
		client.Write(data2)
		recv1 := make([]byte, len(data1))
		_, err := server.Read(recv1)
		if err != nil {
			t.Fatal("Read 1", err)
		}
		recv2 := make([]byte, len(data2))
		_, err = io.ReadFull(server, recv2)
		if err != nil {
			t.Fatal("Read 2", err)
		}
		if !bytes.Equal(recv1, data1) {
			t.Fatal("data 1 mismatch")
		}
		if !bytes.Equal(recv2, data2) {
			t.Fatal("data 2 mismatch")
		}

		server.Write(data1)
		server.Write(data2)
		recv1 = make([]byte, len(data1))
		_, err = client.Read(recv1)
		if err != nil {
			t.Fatal("Read 1", err)
		}
		recv2 = make([]byte, len(data2))
		_, err = io.ReadFull(client, recv2)
		if err != nil {
			t.Fatal("Read 2", err)
		}
		if !bytes.Equal(recv1, data1) {
			t.Fatal("data 1 mismatch")
		}
		if !bytes.Equal(recv2, data2) {
			t.Fatal("data 2 mismatch")
		}
	})
}

func TestProtocolSmallRead(t *testing.T) {
	data := make([]byte, 10)
	rand.Read(data)
	runProtocolTest(t, func(client, server *PktEncLayer) {
		client.Write(data)
		bufsz1 := make([]byte, 1)
		recv := make([]byte, len(data))
		for i := 0; i < len(data); i++ {
			_, err := server.Read(bufsz1)
			if err != nil {
				t.Fatal("Read " + err.Error())
			}
			recv[i] = bufsz1[0]
		}
		if !bytes.Equal(recv, data) {
			t.Fatal("data 1 mismatch")
		}
	})
}

func TestProtocolSmallWrite(t *testing.T) {
	data := make([]byte, 10)
	rand.Read(data)
	runProtocolTest(t, func(client, server *PktEncLayer) {
		client.Write(data)
		bufsz1 := make([]byte, 1)
		recv := make([]byte, len(data))
		for i := 0; i < len(data); i++ {
			bufsz1[0] = data[i]
			_, err := server.Write(bufsz1)
			if err != nil {
				t.Fatal("Write " + err.Error())
			}
		}
		_, err := io.ReadFull(client, recv)
		if err != nil {
			t.Fatal("Read " + err.Error())
		}
		if !bytes.Equal(recv, data) {
			t.Fatal("data 1 mismatch")
		}
	})
}
func TestProtocolEOF(t *testing.T) {
	runProtocolTest(t, func(client, server *PktEncLayer) {
		client.Close()
		buf := make([]byte, 1)
		n, err := server.Read(buf)
		if n != 0 || err != io.EOF {
			t.Fatal("Read", n, err)
		}
	})
}
func TestProtocolReadTimeout(t *testing.T) {
	runProtocolTest(t, func(client, server *PktEncLayer) {
		buf2 := make([]byte, 2)
		n, err := server.ReadTimeout(buf2, 1*time.Second)
		if n != 0 || err == nil {
			t.Fatal("Read", n, err)
		}
	})
}
func TestProtocolVarLength(t *testing.T) {
	runProtocolTest(t, func(client, server *PktEncLayer) {
		data := make([]byte, 12345)
		rand.Read(data)
		err := client.WriteVarLength(data)
		if err != nil {
			t.Fatal("Write", err)
		}
		recv, err := server.ReadVarLength(nil)
		if err != nil {
			t.Fatal("Read", err)
		}
		if !bytes.Equal(recv, data) {
			t.Fatal("data mismatch")
		}

		data2 := make([]byte, constants.MaxMessagesize-2)
		rand.Read(data2)
		err = server.WriteVarLength(data2)
		if err != nil {
			t.Fatal("Write", err)
		}
		recv, err = client.ReadVarLength(make([]byte, 123))
		if err != nil {
			t.Fatal("Read", err)
		}
		if !bytes.Equal(recv, data2) {
			t.Fatal("data mismatch")
		}

		// should fail if the length is too long
		toolong := make([]byte, constants.MaxMessagesize-2+1)
		err = server.WriteVarLength(toolong)
		if err == nil {
			t.Fatal("Write", err)
		}
	})
}
