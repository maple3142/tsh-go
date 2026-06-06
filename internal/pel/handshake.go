package pel

import (
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"math/big"
	"time"

	"tsh-go/internal/constants"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

var key1Tag = []byte{112, 101, 107, 111, 109, 105, 107, 111}
var key2Tag = []byte{97, 107, 117, 115, 104, 105, 111, 0}

var curve25519q big.Int

func init() {
	curve25519q.SetString("1000000000000000000000000000000014def9dea2f79cd65812631a5cf5d3ed", 16)
}

// Handshake performs the key exchange for this encrypted layer.
func (layer *PktEncLayer) Handshake(isInitiator bool) error {
	wb, wib := generateW(layer.secret)

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
	_, err = layer.conn.Write(my_pk)
	layer.conn.SetWriteDeadline(time.Time{})
	if err != nil {
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
	key1 := handshakeHMAC(layer.secret, shared_secret, key1Tag)
	key2 := handshakeHMAC(layer.secret, shared_secret, key2Tag)
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
	confirm_message := handshakeHMAC(layer.secret, pk1, pk2, shared_secret)
	_, err = layer.Write(confirm_message)
	if err != nil {
		return NewHandshakeError(constants.PelFailure, "Failed to send confirmation")
	}
	buf := make([]byte, len(confirm_message))
	_, err = io.ReadFull(layer, buf)
	if err != nil || subtle.ConstantTimeCompare(buf, confirm_message) != 1 {
		return NewHandshakeError(constants.PelFailure, "Failed to receive confirmation message (secret does not match?)")
	}
	return nil
}

func handshakeHMAC(secret []byte, bs ...[]byte) []byte {
	h := hmac.New(sha256.New, secret)
	for _, b := range bs {
		h.Write(b)
	}
	return h.Sum(nil)
}

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

func generateW(secret []byte) ([]byte, []byte) {
	// w * wi = 1 mod q
	var w big.Int
	w.SetBytes(secret)
	w.Mod(&w, &curve25519q)
	var wi big.Int
	wi.ModInverse(&w, &curve25519q)
	wb := w.FillBytes(make([]byte, curve25519.ScalarSize))
	wib := wi.FillBytes(make([]byte, curve25519.ScalarSize))
	return wb, wib
}
