package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
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
	if !socksMethodsContain(methods, 0x00) {
		if _, err := conn.Write([]byte{0x05, 0xff}); err != nil {
			log.Printf("SOCKS5 method rejection write failed: %v", err)
		}
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
	app.mu.Unlock()
	if ts == nil {
		writeSOCKSReply(conn, 0x05)
		return
	}

	// All WebView browser SOCKS traffic resolves and dials through tsnet. There
	// is intentionally no direct net.Dial fallback here: public internet, LAN
	// names, MagicDNS, and .local aliases must all use Tailscale's resolver and
	// routing policy. If an exit node is configured, tsnet handles it; otherwise
	// non-tailnet destinations fail closed instead of escaping locally.
	log.Printf("SOCKS5 connect %s via tsnet", addr)
	remote, err := ts.Dial(context.Background(), "tcp", addr)
	if err != nil {
		log.Printf("SOCKS5 tsnet dial failed for %s: %v", addr, err)
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

func socksMethodsContain(methods []byte, method byte) bool {
	for _, m := range methods {
		if m == method {
			return true
		}
	}
	return false
}

func readSOCKSAddr(r io.Reader, atyp byte) (string, uint16, error) {
	switch atyp {
	case 0x01: // IPv4
		buf := make([]byte, 4+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		return validateSOCKSTarget(net.IP(buf[:4]).String(), binary.BigEndian.Uint16(buf[4:]))
	case 0x03: // domain
		var l [1]byte
		if _, err := io.ReadFull(r, l[:]); err != nil {
			return "", 0, err
		}
		if l[0] == 0 {
			return "", 0, fmt.Errorf("empty domain")
		}
		buf := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		return validateSOCKSTarget(string(buf[:len(buf)-2]), binary.BigEndian.Uint16(buf[len(buf)-2:]))
	case 0x04: // IPv6
		buf := make([]byte, 16+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", 0, err
		}
		return validateSOCKSTarget(net.IP(buf[:16]).String(), binary.BigEndian.Uint16(buf[16:]))
	default:
		return "", 0, fmt.Errorf("unsupported address type %d", atyp)
	}
}

func validateSOCKSTarget(host string, port uint16) (string, uint16, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", 0, fmt.Errorf("empty host")
	}
	if port == 0 {
		return "", 0, fmt.Errorf("invalid port")
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), port, nil
	}
	if len(host) > 253 || strings.ContainsAny(host, " /?#\\@%") {
		return "", 0, fmt.Errorf("invalid host")
	}
	return host, port, nil
}

func writeSOCKSReply(w io.Writer, rep byte) error {
	_, err := w.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func relay(dst, src net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if werr := writeFull(dst, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
