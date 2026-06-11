package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// withDirectDial enables the testing-only direct-dial bypass for the duration of
// a test, restoring the previous value afterwards.
func withDirectDial(t *testing.T) {
	t.Helper()
	prev := allowDirectDial
	allowDirectDial = true
	t.Cleanup(func() { allowDirectDial = prev })
}

// TestVNCWebSocketDialsAndPipesEndToEnd starts a local TCP server that emulates a
// VNC server's RFB banner, then drives the real VNC websocket handler with the
// direct-dial bypass and asserts the banner is relayed to the websocket client.
func TestVNCWebSocketDialsAndPipesEndToEnd(t *testing.T) {
	withDirectDial(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	const banner = "RFB 003.008\n"
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write([]byte(banner))
		// Drain client bytes until closed.
		_, _ = io.Copy(io.Discard, c)
	}()

	app := newTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(app.handleVNCWebSocket))
	defer srv.Close()

	_, port, _ := net.SplitHostPort(ln.Addr().String())
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/?target=" + url.QueryEscape("vnc://127.0.0.1:"+port)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("vnc ws dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	typ, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("vnc ws read: %v", err)
	}
	if typ != websocket.MessageBinary || string(data) != banner {
		t.Fatalf("vnc banner = %v %q, want binary %q", typ, string(data), banner)
	}
}

// TestSOCKSConnectRelaysEndToEnd verifies the SOCKS5 handler dials a real local
// HTTP origin (via the direct-dial bypass) and relays the full response, which
// also exercises the half-close relay fix.
func TestSOCKSConnectRelaysEndToEnd(t *testing.T) {
	withDirectDial(t)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "hello-through-socks")
	}))
	defer origin.Close()
	originHost, originPort, _ := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))

	app := newTestApp(t)
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { handleSOCKS5(server, app); close(done) }()
	defer func() { client.Close(); <-done }()

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))

	// Greeting: VER=5, NMETHODS=1, METHOD=0 (no auth).
	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greet := make([]byte, 2)
	if _, err := io.ReadFull(client, greet); err != nil || greet[0] != 0x05 || greet[1] != 0x00 {
		t.Fatalf("greeting = %v %v", greet, err)
	}

	// CONNECT to the origin by IPv4 literal.
	ip := net.ParseIP(originHost).To4()
	if ip == nil {
		t.Skipf("origin host %q is not IPv4", originHost)
	}
	p, err := strconv.Atoi(originPort)
	if err != nil {
		t.Fatal(err)
	}
	req := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(p >> 8), byte(p & 0xff)}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != 0x00 {
		t.Fatalf("socks reply = %v %v", reply, err)
	}

	// Now speak HTTP/1.0 through the tunnel and read the response body.
	if _, err := io.WriteString(client, "GET / HTTP/1.0\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(client)
	var body strings.Builder
	for {
		line, err := br.ReadString('\n')
		body.WriteString(line)
		if err != nil {
			break
		}
	}
	if !strings.Contains(body.String(), "hello-through-socks") {
		t.Fatalf("did not relay full HTTP response: %q", body.String())
	}
}

// TestSSHConnectReachesUpstream confirms the SSH connect handler dials the target
// (direct-dial bypass) and, when the upstream is not a real SSH server, falls
// back to prompting for a password instead of failing to connect.
func TestSSHConnectReachesUpstream(t *testing.T) {
	withDirectDial(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Not an SSH server: close immediately so the handshake fails.
			c.Close()
		}
	}()

	app := newTestApp(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	rr := performJSON(app.handleSSHConnect, "POST", "/api/ssh/connect", map[string]any{
		"host": "127.0.0.1", "user": "tester", "port": p,
	})
	if rr.Code != 200 {
		t.Fatalf("ssh connect = %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "needsPassword") {
		t.Fatalf("expected password prompt after reaching upstream, got %s", rr.Body.String())
	}
}
