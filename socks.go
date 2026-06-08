package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"tailscale.com/tsnet"
)

// socksProxy holds the SOCKS5 server state and access control
type socksProxy struct {
	ts         *tsnet.Server
	listener   net.Listener
	mu         sync.Mutex
	allowedIPs map[string]bool // only these source IPs can connect
}

// startSOCKS5 starts a local SOCKS5 proxy restricted to allowed clients
func startSOCKS5(ts *tsnet.Server, listenAddr string) (*socksProxy, error) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	sp := &socksProxy{
		ts:       ts,
		listener: ln,
		allowedIPs: map[string]bool{
			"127.0.0.1": true, // only local WebView2 process
			"::1":       true,
		},
	}
	go sp.serve()
	log.Printf("SOCKS5 proxy listening on %s (localhost-only)", listenAddr)
	return sp, nil
}

func (sp *socksProxy) serve() {
	for {
		conn, err := sp.listener.Accept()
		if err != nil {
			return
		}
		// Check source IP
		host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		sp.mu.Lock()
		allowed := sp.allowedIPs[host]
		sp.mu.Unlock()
		if !allowed {
			log.Printf("SOCKS5: rejected connection from %s", conn.RemoteAddr())
			conn.Close()
			continue
		}
		go sp.handleConn(conn)
	}
}

func (sp *socksProxy) Close() {
	sp.listener.Close()
}

func (sp *socksProxy) handleConn(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != 0x05 {
		return
	}

	// No auth
	conn.Write([]byte{0x05, 0x00})

	// CONNECT request
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[1] != 0x01 {
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	var port uint16
	switch buf[3] {
	case 0x01: // IPv4
		if n < 10 {
			return
		}
		host = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		port = uint16(buf[8])<<8 | uint16(buf[9])
	case 0x03: // Domain
		dLen := int(buf[4])
		if n < 5+dLen+2 {
			return
		}
		host = string(buf[5 : 5+dLen])
		port = uint16(buf[5+dLen])<<8 | uint16(buf[5+dLen+1])
	case 0x04: // IPv6
		if n < 22 {
			return
		}
		host = net.IP(buf[4:20]).String()
		port = uint16(buf[20])<<8 | uint16(buf[21])
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	// Only allow connections to Tailscale IPs (100.x.x.x) and tailnet DNS names
	// Block connections to local network, public internet, etc.
	if !sp.isAllowedDestination(host) {
		log.Printf("SOCKS5: blocked destination %s:%d", host, port)
		conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // not allowed
		return
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	remote, err := sp.ts.Dial(context.Background(), "tcp", addr)
	if err != nil {
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // connection refused
		return
	}
	defer remote.Close()

	// Success
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Relay
	done := make(chan struct{}, 2)
	go func() { relay(remote, conn); done <- struct{}{} }()
	go func() { relay(conn, remote); done <- struct{}{} }()
	<-done
}

// isAllowedDestination restricts proxy to tailnet addresses only
func (sp *socksProxy) isAllowedDestination(host string) bool {
	// Tailscale IPv4 CGNAT range: 100.64.0.0/10
	ip := net.ParseIP(host)
	if ip != nil {
		ip4 := ip.To4()
		if ip4 != nil {
			// 100.64.0.0/10 (Tailscale CGNAT)
			return ip4[0] == 100 && (ip4[1]&0xC0) == 64
		}
		// Tailscale IPv6: fd7a:115c:a1e0::/48
		if ip[0] == 0xfd && ip[1] == 0x7a {
			return true
		}
		return false
	}
	// DNS names — allow anything (tsnet will resolve via tailnet MagicDNS)
	// This lets hostnames like "smith", "tnas", etc. resolve
	return true
}

func relay(dst, src net.Conn) {
	buf := make([]byte, 32 * 1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			dst.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
