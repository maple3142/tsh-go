package server

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"

	"tsh-go/internal/socks5"
	"tsh-go/internal/utils"

	"github.com/hashicorp/yamux"
)

func TestSocks5MuxConcurrentStreams(t *testing.T) {
	// let create a single tcp echo server
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()

	go func() {
		for {
			conn, err := target.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				io.Copy(conn, conn)
			}(conn)
		}
	}()

	// use net.Pipe to simulate client / server connection
	// and start socks5 server with handleSocks5
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	go handleSocks5(utils.DSEFromRW(serverConn, serverConn))

	// client side simulation
	config := yamux.DefaultConfig()
	config.LogOutput = io.Discard
	session, err := yamux.Client(clientConn, config)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	payloads := [][]byte{
		[]byte("first stream"),
		[]byte("second stream"),
	}
	var wg sync.WaitGroup
	for _, payload := range payloads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := roundTripSocks5MuxStream(session, target.Addr().String(), payload); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func roundTripSocks5MuxStream(session *yamux.Session, targetAddr string, payload []byte) error {
	// this function simulates a socks5 client
	// send payload to targetAddr using socks5 client (session)
	// and expect to receive the same payload back (echo server)
	conn, err := session.Open()
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := socks5.NewNegotiationRequest([]byte{socks5.MethodNone}).WriteTo(conn); err != nil {
		return err
	}
	reply, err := socks5.NewNegotiationReplyFrom(conn)
	if err != nil {
		return err
	}
	if reply.Method != socks5.MethodNone {
		return socks5.ErrBadReply
	}

	atyp, addr, port, err := socks5.ParseAddress(targetAddr)
	if err != nil {
		return err
	}
	if atyp == socks5.ATYPDomain {
		addr = addr[1:]
	}
	if _, err := socks5.NewRequest(socks5.CmdConnect, atyp, addr, port).WriteTo(conn); err != nil {
		return err
	}
	connectReply, err := socks5.NewReplyFrom(conn)
	if err != nil {
		return err
	}
	if connectReply.Rep != socks5.RepSuccess {
		return socks5.ErrBadReply
	}

	if _, err := conn.Write(payload); err != nil {
		return err
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		return err
	}
	if !bytes.Equal(got, payload) {
		return socks5.ErrBadReply
	}
	return nil
}
