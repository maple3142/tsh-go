package constants

import "golang.org/x/crypto/chacha20poly1305"

const (
	Bufsize        = 65534 // max 16-bit unsigned integer, with 65535 = 0xffff to signal EOF
	MaxMessagesize = Bufsize - chacha20poly1305.NonceSize - chacha20poly1305.Overhead

	Kill          = 0
	GetFile       = 1
	PutFile       = 2
	RunShell      = 3
	RunShellNoTTY = 4
	SOCKS5        = 5
	Pipe          = 6

	PelSuccess = 1
	PelFailure = 0

	PelSystemError    = -1
	PelConnClosed     = -2
	PelBadMsgLength   = -3
	PelCorruptedData  = -4
	PelUndefinedError = -5

	HandshakeRWTimeout = 3 // seconds
)
