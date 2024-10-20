package pel

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"time"

	"tsh-go/internal/constants"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/net/proxy"
)

var dialer = proxy.FromEnvironment() // automatically use proxy settings if set (all_proxy and no_proxy)

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
	isEof   bool
	eofChan chan struct{}
}

// Packet Encryption Layer Listener
type PktEncLayerListener struct {
	listener    net.Listener
	secret      []byte
	isInitiator bool
}

func NewPktEncLayerListener(address string, secret []byte, isInitiator bool) (*PktEncLayerListener, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	ln := &PktEncLayerListener{
		listener:    listener,
		secret:      secret,
		isInitiator: isInitiator,
	}
	return ln, nil
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
		isEof:      false,
		eofChan:    make(chan struct{}),
	}
	return layer, nil
}

func NewPelError(err int) error {
	return fmt.Errorf("PelError(code=%d)", err)
}

func NewHandshakeError(err int, reason string) error {
	return fmt.Errorf("PelError(at=\"Handshake\", code=%d, reason=%#v)", err, reason)
}

func Listen(address string, secret []byte, isInitiator bool) (*PktEncLayerListener, error) {
	listener, err := NewPktEncLayerListener(address, secret, isInitiator)
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
	err = layer.Handshake(ln.isInitiator)
	if err != nil {
		layer.Close()
		return nil, err
	}
	return layer, nil
}

func Dial(address string, secret []byte, isInitiator bool) (l *PktEncLayer, err error) {
	defer func() {
		if _err := recover(); _err != nil {
			l = nil
			err = NewPelError(constants.PelSystemError)
		}
	}()
	conn, err := dialer.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	layer, _ := NewPktEncLayer(conn, secret)
	err = layer.Handshake(isInitiator)
	if err != nil {
		layer.Close()
		return nil, err
	}
	return layer, nil
}

func (layer *PktEncLayer) hmac(bs ...[]byte) []byte {
	h := hmac.New(sha256.New, []byte(layer.secret))
	for _, b := range bs {
		h.Write(b)
	}
	return h.Sum(nil)
}

var key1Tag = []byte{112, 101, 107, 111, 109, 105, 107, 111}
var key2Tag = []byte{97, 107, 117, 115, 104, 105, 111, 0}

func multipleX25519(point []byte, scalars ...[]byte) ([]byte, error) {
	var err error
	pt := point
	for _, scalar := range scalars {
		pt, err = curve25519.X25519(scalar, pt)
		if err != nil {
			return nil, err
		}
	}
	return pt, nil
}

var curve25519q big.Int

func init() {
	curve25519q.SetString("1000000000000000000000000000000014def9dea2f79cd65812631a5cf5d3ed", 16)
}

func (layer *PktEncLayer) generateW() ([]byte, []byte) {
	// w * wi = 1 mod q
	var w big.Int
	w.SetBytes(layer.secret)
	w.Mod(&w, &curve25519q)
	var wi big.Int
	wi.ModInverse(&w, &curve25519q)
	wb := w.FillBytes(make([]byte, curve25519.ScalarSize))
	wib := wi.FillBytes(make([]byte, curve25519.ScalarSize))
	return wb, wib
}

// do an SPAKE2-like key exchange (likely weaker but simpler to implement)
// return err if the packet read/write operation
// takes more than HandshakeRWTimeout (default: 3) seconds
func (layer *PktEncLayer) Handshake(isInitiator bool) error {
	wb, wib := layer.generateW()

	timeout := time.Duration(constants.HandshakeRWTimeout) * time.Second
	// generate key pair
	my_sk := make([]byte, curve25519.ScalarSize)
	rand.Read(my_sk)
	my_pk, err := multipleX25519(curve25519.Basepoint, my_sk, wb) // pk'=(sk*w)*G
	if err != nil {
		return NewHandshakeError(constants.PelFailure, "Failed to generate key pair")
	}

	var remote_pk []byte = make([]byte, curve25519.PointSize)
	var shared_secret []byte

	// send public key
	layer.conn.SetWriteDeadline(
		time.Now().Add(time.Duration(constants.HandshakeRWTimeout) * time.Second))
	n, err := layer.conn.Write(my_pk)
	layer.conn.SetWriteDeadline(time.Time{})
	if n != curve25519.ScalarSize || err != nil {
		return NewHandshakeError(constants.PelFailure, "Failed to send public key")
	}

	// receive public key
	err = layer.readConnUntilFilledTimeout(remote_pk, timeout)
	if err != nil {
		return NewHandshakeError(constants.PelFailure, "Failed to receive public key")
	}

	// derive shared secret
	shared_secret, err = multipleX25519(remote_pk, my_sk, wib) // S=(sk*w^-1)*remote_pk'=(my_sk*remote_sk)*G
	if err != nil {
		return NewHandshakeError(constants.PelFailure, "Failed to derive shared secret")
	}
	key1 := layer.hmac(shared_secret, key1Tag)
	key2 := layer.hmac(shared_secret, key2Tag)
	var aead cipher.AEAD
	if isInitiator {
		aead, _ = chacha20poly1305.New(key1)
		layer.sendAead = aead
		aead, _ = chacha20poly1305.New(key2)
		layer.recvAead = aead
	} else {
		aead, _ = chacha20poly1305.New(key2)
		layer.sendAead = aead
		aead, _ = chacha20poly1305.New(key1)
		layer.recvAead = aead
	}

	// still need to confirm the shared secret is the same
	var pk1, pk2 []byte
	if isInitiator {
		pk1 = my_pk
		pk2 = remote_pk
	} else {
		pk1 = remote_pk
		pk2 = my_pk
	}
	confirm_message := layer.hmac(pk1, pk2, shared_secret)
	n, err = layer.Write(confirm_message)
	if n != len(confirm_message) || err != nil {
		return NewHandshakeError(constants.PelFailure, "Failed to send confirmation")
	}
	buf := make([]byte, len(confirm_message))
	_, err = io.ReadFull(layer, buf)
	if err != nil || subtle.ConstantTimeCompare(buf, confirm_message) != 1 {
		return NewHandshakeError(constants.PelFailure, "Failed to receive confirmation message (secret does not match?)")
	}
	return nil
}

func (layer *PktEncLayer) Close() error {
	return layer.conn.Close()
}

func (layer *PktEncLayer) WritePartial(p []byte) (int, error) {
	// this may write partial data
	// returns (number of bytes written, error)
	// and the number of bytes written may be less than len(p) even if err == nil
	return layer.write(p[:min(len(p), constants.MaxMessagesize)])
}

func (layer *PktEncLayer) Write(p []byte) (int, error) {
	// io.Writer requires that if err == nil, n == len(p)
	// so we need to write all data in p
	total := len(p)
	idx := 0
	for idx < total {
		n, err := layer.WritePartial(p[idx:total])
		if err != nil {
			return idx, err
		}
		idx += n
	}
	return idx, nil
}

// packet format
// | length (2 bytes) | nonce (12 bytes) | encrypted data |
//                    | <-         length bytes        -> |

func (layer *PktEncLayer) write(p []byte) (int, error) {
	length := len(p)
	if length <= 0 || length > constants.MaxMessagesize {
		return 0, NewPelError(constants.PelBadMsgLength)
	}

	data_length := chacha20poly1305.NonceSize + length + chacha20poly1305.Overhead
	if data_length > constants.Bufsize {
		return 0, NewPelError(constants.PelBadMsgLength)
	}
	pkt_length := 2 + data_length
	buffer := layer.sendBuffer[0:pkt_length]
	binary.LittleEndian.PutUint16(buffer, uint16(data_length))

	additionalData := make([]byte, 4)
	binary.LittleEndian.PutUint32(additionalData, uint32(layer.sendPktCtr))

	nonce := buffer[2 : 2+chacha20poly1305.NonceSize]
	rand.Read(nonce)

	layer.sendAead.Seal(nonce, nonce, p, additionalData) // append ciphertext (with tag) to nonce

	_, err := layer.conn.Write(buffer)
	if err != nil {
		return 0, err
	}
	layer.sendPktCtr++
	return length, nil
}

func (layer *PktEncLayer) CloseWrite() error {
	signal := []byte{0xff, 0xff} // EOF signal
	_, err := layer.conn.Write(signal)
	return err
}

func (layer *PktEncLayer) CloseRead() error {
	layer.isEof = true
	// non-blocking send
	select {
	case layer.eofChan <- struct{}{}:
	default:
	}
	return nil
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
	buffer := layer.recvBuffer

	if err := layer.readConnUntilFilled(buffer[:2]); err != nil {
		return 0, err
	}

	data_length := int(binary.LittleEndian.Uint16(buffer))
	if data_length == 0xffff { // EOF signal
		layer.CloseRead()
		return 0, io.EOF
	}
	if data_length <= 0 || data_length > constants.Bufsize {
		return 0, NewPelError(constants.PelBadMsgLength)
	}

	data := layer.recvBuffer[0:data_length]

	if err := layer.readConnUntilFilled(data); err != nil {
		return 0, NewPelError(constants.PelConnClosed)
	}

	additionalData := make([]byte, 4)
	binary.LittleEndian.PutUint32(additionalData, uint32(layer.recvPktCtr))

	nonce := data[0:chacha20poly1305.NonceSize]
	ciphertext := data[chacha20poly1305.NonceSize:data_length]

	pt, err := layer.recvAead.Open(ciphertext[:0], nonce, ciphertext, additionalData)
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
	if layer.isEof {
		return io.EOF
	}
	ch := make(chan error)
	go func() {
		_, err := io.ReadFull(layer.conn, p)
		ch <- err
	}()
	select {
	case err := <-ch:
		return err
	case <-layer.eofChan:
		return io.EOF
	}
}

func (layer *PktEncLayer) readConnUntilFilledTimeout(p []byte, timeout time.Duration) error {
	defer layer.conn.SetReadDeadline(time.Time{})
	layer.conn.SetReadDeadline(time.Now().Add(timeout))
	if err := layer.readConnUntilFilled(p); err != nil {
		return err
	}
	return nil
}
