package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

var socksAddr = "127.0.0.1:1080"

// startSOCKS5Listener starts the SOCKS5 listener immediately.
// It can serve requests as soon as app.tsServer is non-nil.
func startSOCKS5Listener(app *App) {
	ln, err := net.Listen("tcp", socksAddr)
	if err != nil {
		log.Printf("SOCKS5 listen failed: %v", err)
		return
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
			if host != "127.0.0.1" && host != "::1" {
				conn.Close()
				continue
			}
			go handleSOCKS5(conn, app)
		}
	}()
	log.Printf("SOCKS5 proxy on %s (waiting for tailscale)", socksAddr)
}

func handleSOCKS5(conn net.Conn, app *App) {
	defer conn.Close()

	// RFC 1928 greeting: VER, NMETHODS, METHODS...
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil || head[0] != 0x05 {
		return
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	// No-auth method.
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// Request: VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT.
	req := make([]byte, 4)
	if _, err := io.ReadFull(conn, req); err != nil || req[0] != 0x05 {
		return
	}
	if req[1] != 0x01 { // CONNECT only
		writeSOCKSReply(conn, 0x07)
		return
	}

	host, port, err := readSOCKSAddr(conn, req[3])
	if err != nil {
		writeSOCKSReply(conn, 0x08)
		return
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	app.mu.Lock()
	ts := app.tsServer
	exitNode := useExitNode
	app.mu.Unlock()

	var remote net.Conn
	if shouldRouteViaTSNet(host, exitNode) {
		if ts == nil {
			writeSOCKSReply(conn, 0x05)
			return
		}
		remote, err = ts.Dial(context.Background(), "tcp", addr)
	} else {
		remote, err = net.DialTimeout("tcp", addr, 10*time.Second)
	}
	if err != nil {
		writeSOCKSReply(conn, 0x05)
		return
	}
	defer remote.Close()

	if err := writeSOCKSReply(conn, 0x00); err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go func() { relay(remote, conn); done <- struct{}{} }()
	go func() { relay(conn, remote); done <- struct{}{} }()
	<-done
}

func readSOCKSAddr(r io.Reader, atyp byte) (string, uint16, error) {
	switch atyp {
	case 0x01: // IPv4
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		return net.IP(buf[:4]).String(), binary.BigEndian.Uint16(buf[4:]), nil
	case 0x03: // domain
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return "", 0, err
		}
		buf := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		return string(buf[:len(buf)-2]), binary.BigEndian.Uint16(buf[len(buf)-2:]), nil
	case 0x04: // IPv6
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		return net.IP(buf[:16]).String(), binary.BigEndian.Uint16(buf[16:]), nil
	default:
		return "", 0, fmt.Errorf("unsupported address type %d", atyp)
	}
}

func writeSOCKSReply(w io.Writer, rep byte) error {
	_, err := w.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

func shouldRouteViaTSNet(host string, exitNode bool) bool {
	if exitNode {
		return true
	}
	return isTailnetDest(host)
}

func isTailnetDest(host string) bool {
	h := strings.Trim(strings.ToLower(host), "[]")
	if ip := net.ParseIP(h); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4[0] == 100 && (ip4[1]&0xC0) == 64 // 100.64.0.0/10
		}
		return len(ip) == net.IPv6len && ip[0] == 0xfd && ip[1] == 0x7a // Tailscale ULA
	}

	// MagicDNS short names and ts.net names should go through tsnet.
	// Public FQDNs (e.g. news.ycombinator.com) should go direct unless an exit
	// node is active. .local is intentionally direct for mDNS/LAN.
	if strings.HasSuffix(h, ".local") {
		return false
	}
	if strings.HasSuffix(h, ".ts.net") {
		return true
	}
	return !strings.Contains(h, ".")
}

func relay(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			_, _ = dst.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
