package pel

import (
	"bytes"
	"crypto/rand"
	"testing"
	"tsh-go/internal/constants"
)

var testSecret = []byte("just some secret")

const testAddress = "127.0.0.1:2333"

func (layer *PktEncLayer) WriteFull(p []byte) (int, error) {
	total := len(p)
	idx := 0
	for idx < total {
		n, err := layer.Write(p[idx:min(idx+constants.MaxMessagesize, total)])
		if err != nil {
			return idx, err
		}
		idx += n
	}
	return idx, nil
}

func (layer *PktEncLayer) ReadFull(p []byte) error {
	total := len(p)
	idx := 0
	for idx < total {
		n, err := layer.Read(p[idx:total])
		if err != nil {
			return err
		}
		idx += n
	}
	return nil
}

func runProtocolTest(t *testing.T, fn func(*PktEncLayer, *PktEncLayer)) {
	listener, err := Listen(testAddress, testSecret, true)
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
	client, err = Dial(testAddress, testSecret, false)
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
		client.WriteFull(data2)
		recv1 := make([]byte, len(data1))
		_, err := server.Read(recv1)
		if err != nil {
			t.Fatal("Read 1", err)
		}
		recv2 := make([]byte, len(data2))
		err = server.ReadFull(recv2)
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
		server.WriteFull(data2)
		recv1 = make([]byte, len(data1))
		_, err = client.Read(recv1)
		if err != nil {
			t.Fatal("Read 1", err)
		}
		recv2 = make([]byte, len(data2))
		err = client.ReadFull(recv2)
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
		err := client.ReadFull(recv)
		if err != nil {
			t.Fatal("Read " + err.Error())
		}
		if !bytes.Equal(recv, data) {
			t.Fatal("data 1 mismatch")
		}
	})
}
