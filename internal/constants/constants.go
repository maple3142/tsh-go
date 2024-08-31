package constants

import "golang.org/x/crypto/chacha20poly1305"

const (
	Bufsize        = 65535 // max 16-bit unsigned integer
	MaxMessagesize = Bufsize - chacha20poly1305.NonceSize - chacha20poly1305.Overhead
	Ctrsize        = 4
	Digestsize     = 20

	Kill     = 0
	GetFile  = 1
	PutFile  = 2
	RunShell = 3
	SOCKS5   = 4

	PelSuccess = 1
	PelFailure = 0

	PelSystemError    = -1
	PelConnClosed     = -2
	PelWrongChallenge = -3
	PelBadMsgLength   = -4
	PelCorruptedData  = -5
	PelUndefinedError = -6

	HandshakeRWTimeout = 3 // seconds
)

var Challenge = []byte{
	0xff, 0xd8, 0xd0, 0x43, 0xc9, 0x47, 0x49, 0x51, 0xa, 0x3, 0x9c, 0x47, 0x98, 0x7a, 0xeb, 0x5c,
}
