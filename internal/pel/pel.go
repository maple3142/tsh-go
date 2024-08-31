package pel

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"tsh-go/internal/constants"

	"golang.org/x/crypto/chacha20poly1305"
)

// Packet Encryption Layer
type PktEncLayer struct {
	conn          net.Conn
	secret        []byte
	sendEncrypter cipher.AEAD
	recvDecrypter cipher.AEAD
	sendPktCtr    uint
	recvPktCtr    uint
	readBuffer    []byte // used for avoid allocation
	writeBuffer   []byte // used for avoid allocation
	tmpBuffer     []byte // used for store remaining data if the read buffer is not enough
}

// Packet Encryption Layer Listener
type PktEncLayerListener struct {
	listener net.Listener
	secret   []byte
	isServer bool
}

func NewPktEncLayerListener(address string, secret []byte, isServer bool) (*PktEncLayerListener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	ln := &PktEncLayerListener{
		listener: listener,
		secret:   secret,
		isServer: isServer,
	}
	return ln, nil
}

func NewPktEncLayer(conn net.Conn, secret []byte) (*PktEncLayer, error) {
	layer := &PktEncLayer{
		conn:        conn,
		secret:      secret,
		sendPktCtr:  0,
		recvPktCtr:  0,
		readBuffer:  make([]byte, 2+constants.Bufsize),
		writeBuffer: make([]byte, 2+constants.Bufsize),
		tmpBuffer:   nil,
	}
	return layer, nil
}

func NewPelError(err int) error {
	return fmt.Errorf("PelError(%d)", err)
}

func Listen(address string, secret []byte, isServer bool) (*PktEncLayerListener, error) {
	listener, err := NewPktEncLayerListener(address, secret, isServer)
	return listener, err
}

func (ln *PktEncLayerListener) Close() error {
	return ln.listener.Close()
}

func (ln *PktEncLayerListener) Addr() net.Addr {
	return ln.listener.Addr()
}

func (ln *PktEncLayerListener) Accept() (l *PktEncLayer, err error) {
	defer func() {
		if _err := recover(); _err != nil {
			l = nil
			err = NewPelError(constants.PelSystemError)
		}
	}()
	conn, err := ln.listener.Accept()
	if err != nil {
		return nil, err
	}
	layer, _ := NewPktEncLayer(conn, ln.secret)
	err = layer.Handshake(ln.isServer)
	if err != nil {
		layer.Close()
		return nil, err
	}
	return layer, nil
}

func Dial(address string, secret []byte, isServer bool) (l *PktEncLayer, err error) {
	defer func() {
		if _err := recover(); _err != nil {
			l = nil
			err = NewPelError(constants.PelSystemError)
		}
	}()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, err
	}
	layer, _ := NewPktEncLayer(conn, secret)
	err = layer.Handshake(isServer)
	if err != nil {
		layer.Close()
		return nil, err
	}
	return layer, nil
}

func (layer *PktEncLayer) hashKey(iv []byte) []byte {
	h := hmac.New(sha256.New, []byte(layer.secret))
	h.Write(iv)
	return h.Sum(nil)
}

// exchange IV with client and setup the encryption layer
// return err if the packet read/write operation
// takes more than HandshakeRWTimeout (default: 3) seconds
func (layer *PktEncLayer) Handshake(isServer bool) error {
	timeout := time.Duration(constants.HandshakeRWTimeout) * time.Second
	if isServer {
		randomness := make([]byte, 32)
		if err := layer.readConnUntilFilledTimeout(randomness, timeout); err != nil {
			return err
		}
		rand1 := randomness[:16]
		rand2 := randomness[16:]

		var key []byte
		var aead cipher.AEAD

		key = layer.hashKey(rand1)
		aead, _ = chacha20poly1305.New(key)
		layer.sendEncrypter = aead

		key = layer.hashKey(rand2)
		aead, _ = chacha20poly1305.New(key)
		layer.recvDecrypter = aead

		n, err := layer.ReadTimeout(randomness[:16], timeout)
		if n != 16 || err != nil ||
			subtle.ConstantTimeCompare(randomness[:16], constants.Challenge) != 1 {
			return NewPelError(constants.PelWrongChallenge)
		}

		layer.conn.SetWriteDeadline(
			time.Now().Add(time.Duration(constants.HandshakeRWTimeout) * time.Second))
		n, err = layer.Write(constants.Challenge)
		layer.conn.SetWriteDeadline(time.Time{})
		if n != 16 || err != nil {
			return NewPelError(constants.PelFailure)
		}
		return nil
	} else {
		randomness := make([]byte, 32)
		rand.Read(randomness)
		layer.conn.SetWriteDeadline(
			time.Now().Add(time.Duration(constants.HandshakeRWTimeout) * time.Second))
		n, err := layer.conn.Write(randomness)
		layer.conn.SetWriteDeadline(time.Time{})
		if n != 32 || err != nil {
			return NewPelError(constants.PelFailure)
		}
		rand1 := randomness[:16]
		rand2 := randomness[16:]

		var key []byte
		var aead cipher.AEAD

		key = layer.hashKey(rand2)
		aead, _ = chacha20poly1305.New(key)
		layer.sendEncrypter = aead

		key = layer.hashKey(rand1)
		aead, _ = chacha20poly1305.New(key)
		layer.recvDecrypter = aead

		layer.conn.SetWriteDeadline(
			time.Now().Add(time.Duration(constants.HandshakeRWTimeout) * time.Second))
		n, err = layer.Write(constants.Challenge)
		layer.conn.SetWriteDeadline(time.Time{})
		if n != 16 || err != nil {
			return NewPelError(constants.PelFailure)
		}

		challenge := make([]byte, 16)
		n, err = layer.ReadTimeout(challenge, timeout)
		if n != 16 || err != nil {
			return NewPelError(constants.PelFailure)
		}
		if subtle.ConstantTimeCompare(constants.Challenge, challenge) != 1 {
			return NewPelError(constants.PelWrongChallenge)
		}
		return nil
	}
}

func (layer *PktEncLayer) Close() error {
	return layer.conn.Close()
}

func (layer *PktEncLayer) Write(p []byte) (int, error) {
	return layer.write(p[:min(len(p), constants.MaxMessagesize)])
}

// packet format
// | length (2 bytes) | nonce (12 bytes) | encrypted data |
//                    | <-         length bytes        -> |

func (layer *PktEncLayer) write(p []byte) (int, error) {
	length := len(p)
	if length <= 0 || length > constants.Bufsize {
		return 0, NewPelError(constants.PelBadMsgLength)
	}

	data_length := chacha20poly1305.NonceSize + length + chacha20poly1305.Overhead
	if data_length > (1 << 16) {
		return 0, NewPelError(constants.PelBadMsgLength)
	}
	pkt_length := 2 + data_length
	buffer := layer.writeBuffer[0:pkt_length]
	binary.LittleEndian.PutUint16(buffer, uint16(data_length))

	additionalData := make([]byte, 4)
	binary.LittleEndian.PutUint32(additionalData, uint32(layer.sendPktCtr))

	nonce := buffer[2 : 2+chacha20poly1305.NonceSize]
	rand.Read(nonce)

	layer.sendEncrypter.Seal(nonce, nonce, p, additionalData) // append ciphertext (with tag) to nonce

	idx := 0
	for idx < pkt_length {
		n, err := layer.conn.Write(buffer[idx:pkt_length])
		if err != nil {
			return 0, err
		}
		idx += n
	}
	layer.sendPktCtr++
	return length, nil
}

func (layer *PktEncLayer) Read(p []byte) (int, error) {
	if layer.tmpBuffer != nil {
		n := copy(p, layer.tmpBuffer)
		if n < len(layer.tmpBuffer) {
			layer.tmpBuffer = layer.tmpBuffer[n:]
			return n, nil
		}
		layer.tmpBuffer = nil
		if n < len(p) {
			n2, err := layer.Read(p[n:])
			return n + n2, err
		}
		return n, nil
	}
	return layer.read(p)
}

func (layer *PktEncLayer) ReadTimeout(p []byte, timeout time.Duration) (int, error) {
	defer layer.conn.SetReadDeadline(time.Time{})
	layer.conn.SetReadDeadline(time.Now().Add(timeout))
	n, err := layer.Read(p)
	return n, err
}

func (layer *PktEncLayer) read(p []byte) (int, error) {
	buffer := layer.readBuffer

	if err := layer.readConnUntilFilled(buffer[:2]); err != nil {
		return 0, err
	}

	data_length := int(binary.LittleEndian.Uint16(buffer))
	if data_length <= 0 || data_length > constants.Bufsize {
		return 0, NewPelError(constants.PelBadMsgLength)
	}

	data := layer.readBuffer[0:data_length]

	if err := layer.readConnUntilFilled(data); err != nil {
		return 0, NewPelError(constants.PelConnClosed)
	}

	additionalData := make([]byte, 4)
	binary.LittleEndian.PutUint32(additionalData, uint32(layer.recvPktCtr))

	nonce := data[0:chacha20poly1305.NonceSize]
	ciphertext := data[chacha20poly1305.NonceSize:data_length]

	pt, err := layer.recvDecrypter.Open(ciphertext[:0], nonce, ciphertext, additionalData)
	if err != nil {
		return 0, NewPelError(constants.PelCorruptedData)
	}
	n_copied := copy(p, pt)
	if n_copied < len(pt) {
		layer.tmpBuffer = pt[len(p):]
	}

	layer.recvPktCtr++
	return n_copied, nil
}

func (layer *PktEncLayer) readConnUntilFilled(p []byte) error {
	idx := 0
	tot := len(p)
	for idx < tot {
		n, err := layer.conn.Read(p[idx:tot])
		if err != nil {
			return err
		}
		idx += n
	}
	return nil
}

func (layer *PktEncLayer) readConnUntilFilledTimeout(p []byte, timeout time.Duration) error {
	defer layer.conn.SetReadDeadline(time.Time{})
	layer.conn.SetReadDeadline(time.Now().Add(timeout))
	if err := layer.readConnUntilFilled(p); err != nil {
		return err
	}
	return nil
}
