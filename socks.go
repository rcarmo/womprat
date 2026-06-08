package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"tailscale.com/tsnet"
)

var socksAddr = "127.0.0.1:1080"

func startSOCKS5(ts *tsnet.Server) error {
	ln, err := net.Listen("tcp", socksAddr)
	if err != nil {
		return err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Only allow localhost
			host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
			if host != "127.0.0.1" && host != "::1" {
				conn.Close()
				continue
			}
			go handleSOCKS5(conn, ts)
		}
	}()
	log.Printf("SOCKS5 proxy on %s", socksAddr)
	return nil
}

func handleSOCKS5(conn net.Conn, ts *tsnet.Server) {
	defer conn.Close()

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != 0x05 {
		return
	}
	conn.Write([]byte{0x05, 0x00}) // no auth

	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[1] != 0x01 {
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	var port uint16
	switch buf[3] {
	case 0x01:
		if n < 10 { return }
		host = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		port = uint16(buf[8])<<8 | uint16(buf[9])
	case 0x03:
		dLen := int(buf[4])
		if n < 5+dLen+2 { return }
		host = string(buf[5 : 5+dLen])
		port = uint16(buf[5+dLen])<<8 | uint16(buf[5+dLen+1])
	case 0x04:
		if n < 22 { return }
		host = net.IP(buf[4:20]).String()
		port = uint16(buf[20])<<8 | uint16(buf[21])
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	remote, err := ts.Dial(context.Background(), "tcp", addr)
	if err != nil {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	done := make(chan struct{}, 2)
	go func() { relay(remote, conn); done <- struct{}{} }()
	go func() { relay(conn, remote); done <- struct{}{} }()
	<-done
}

func relay(dst, src net.Conn) {
	buf := make([]byte, 32 * 1024)
	for {
		n, err := src.Read(buf)
		if n > 0 { dst.Write(buf[:n]) }
		if err != nil { return }
	}
}
