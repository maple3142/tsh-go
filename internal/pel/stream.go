package pel

import (
	"crypto/rand"
	"encoding/binary"
	"io"
	"time"

	"tsh-go/internal/constants"

	"golang.org/x/crypto/chacha20poly1305"
)

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
//
//	| <-         length bytes        -> |
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
	layer.readClosed.Store(true)
	return layer.conn.SetReadDeadline(time.Now()) // this cause timeout for any pending read
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
	if layer.readClosed.Load() {
		return io.EOF
	}
	_, err := io.ReadFull(layer.conn, p)
	if err != nil && layer.readClosed.Load() {
		// if another goroutine call CloseRead() while we are reading
		// it would cause timeout error, and if readClosed is set we can treat it as EOF
		return io.EOF
	}
	return err
}

func (layer *PktEncLayer) readConnUntilFilledTimeout(p []byte, timeout time.Duration) error {
	defer layer.conn.SetReadDeadline(time.Time{})
	layer.conn.SetReadDeadline(time.Now().Add(timeout))
	if err := layer.readConnUntilFilled(p); err != nil {
		return err
	}
	return nil
}
