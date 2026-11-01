//go:build linux

package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLinuxBrowserProxyFailsClosed(t *testing.T) {
	old := allowDirectDial
	allowDirectDial = false
	defer func() { allowDirectDial = old }()
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("host route reached") }))
	defer canary.Close()
	a := &App{config: defaultConfig()}
	proxy, stop, err := a.startBrowserProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for _, request := range []string{"GET " + canary.URL + "/ HTTP/1.1\r\nHost: " + strings.TrimPrefix(canary.URL, "http://") + "\r\nConnection: close\r\n\r\n", "CONNECT " + strings.TrimPrefix(canary.URL, "http://") + " HTTP/1.1\r\nHost: " + strings.TrimPrefix(canary.URL, "http://") + "\r\nConnection: close\r\n\r\n"} {
		conn, err := net.Dial("tcp", strings.TrimPrefix(proxy, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprint(conn, request)
		response, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 502 {
			t.Fatalf("status %d", response.StatusCode)
		}
		response.Body.Close()
		conn.Close()
	}
}

func TestLinuxBrowserProxyRejectsInvalidTargets(t *testing.T) {
	a := &App{config: defaultConfig()}
	for _, target := range []string{"/relative", "https://example.com/", "http://user:password@example.com/"} {
		r := httptest.NewRequest("GET", target, nil)
		w := httptest.NewRecorder()
		a.serveBrowserProxy(w, r, &http.Transport{})
		if w.Code != 400 {
			t.Fatalf("%s: %d", target, w.Code)
		}
	}
}

func TestLinuxBrowserProxyLocked(t *testing.T) {
	a := &App{config: defaultConfig(), locked: true}
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	w := httptest.NewRecorder()
	a.serveBrowserProxy(w, r, &http.Transport{})
	if w.Code != http.StatusLocked {
		t.Fatalf("status %d", w.Code)
	}
}

func TestLinuxBrowserProxyWebSocketAndStop(t *testing.T) {
	old := allowDirectDial
	allowDirectDial = true
	defer func() { allowDirectDial = old }()
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked")
		}
		if !proxyHeaderToken(r.Header.Get("Connection"), "upgrade") {
			t.Error("upgrade removed")
		}
		c, b, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		b.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		b.Flush()
		for {
			value, err := b.ReadByte()
			if err != nil {
				return
			}
			if _, err = c.Write([]byte{value}); err != nil {
				return
			}
		}
	}))
	defer canary.Close()
	a := &App{config: defaultConfig()}
	proxy, stop, err := a.startBrowserProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	c, err := net.Dial("tcp", strings.TrimPrefix(proxy, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "GET %s/socket HTTP/1.1\r\nHost: %s\r\nConnection: keep-alive, Upgrade\r\nUpgrade: websocket\r\nProxy-Authorization: secret\r\n\r\n", canary.URL, strings.TrimPrefix(canary.URL, "http://"))
	b := bufio.NewReader(c)
	r, err := http.ReadResponse(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != 101 {
		t.Fatalf("upgrade status %d", r.StatusCode)
	}
	c.Write([]byte("echo"))
	value := make([]byte, 4)
	if _, err = io.ReadFull(b, value); err != nil {
		t.Fatal(err)
	}
	if string(value) != "echo" {
		t.Fatalf("echo %q", value)
	}
	stop()
	if _, err = b.ReadByte(); err == nil {
		t.Fatal("proxy stop left upgraded connection open")
	}
}

func TestLinuxBrowserProxyHeaderIsolation(t *testing.T) {
	header := http.Header{"Connection": []string{"X-Secret, keep-alive"}, "X-Secret": []string{"secret"}, "Proxy-Authorization": []string{"secret"}, "Cookie": []string{"safe=value"}}
	removeProxyHeaders(header)
	if header.Get("X-Secret") != "" || header.Get("Proxy-Authorization") != "" || header.Get("Connection") != "" {
		t.Fatal("proxy headers leaked")
	}
	if header.Get("Cookie") != "safe=value" {
		t.Fatal("origin cookie removed")
	}
}
