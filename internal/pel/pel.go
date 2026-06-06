package pel

import (
	"crypto/cipher"
	"fmt"
	"net"
	"sync/atomic"

	"tsh-go/internal/constants"
)

// Packet Encryption Layer
type PktEncLayer struct {
	conn       net.Conn
	secret     []byte
	sendAead   cipher.AEAD
	recvAead   cipher.AEAD
	sendPktCtr uint
	recvPktCtr uint
	recvBuffer []byte // used for avoid allocation
	sendBuffer []byte // used for avoid allocation
	tmpBuffer  []byte // used for store remaining data if the read buffer is not enough
	// record whether the connection is still readable
	readClosed atomic.Bool
}

// Packet Encryption Layer Listener
type PktEncLayerListener struct {
	listener    net.Listener
	secret      []byte
	isInitiator bool
}

func NewPktEncLayer(conn net.Conn, secret []byte) (*PktEncLayer, error) {
	layer := &PktEncLayer{
		conn:       conn,
		secret:     secret,
		sendPktCtr: 0,
		recvPktCtr: 0,
		recvBuffer: make([]byte, 2+constants.Bufsize),
		sendBuffer: make([]byte, 2+constants.Bufsize),
		tmpBuffer:  nil,
	}
	return layer, nil
}

func NewPelError(err int) error {
	return fmt.Errorf("PelError(code=%d)", err)
}

func NewHandshakeError(err int, reason string) error {
	return fmt.Errorf("PelError(at=\"Handshake\", code=%d, reason=%#v)", err, reason)
}
