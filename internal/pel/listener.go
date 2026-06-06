package pel

import (
	"net"

	"tsh-go/internal/constants"

	"golang.org/x/net/proxy"
)

var dialer = proxy.FromEnvironment() // automatically use proxy settings if set (all_proxy and no_proxy)

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
