package main

import "net"

func newListener(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}
