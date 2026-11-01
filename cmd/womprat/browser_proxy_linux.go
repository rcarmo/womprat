//go:build linux

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WebKit HTTP proxy: destinations are resolved/dialled only by Womprat's
// existing tailnet dial path. Content never receives a loopback bypass list.
func (a *App) startBrowserProxy() (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		a.mu.Lock()
		ts := a.tsServer
		a.mu.Unlock()
		return dialTSNetPreferIPv4(ctx, ts, addr)
	}, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSHandshakeTimeout: socksDialTimeout, ResponseHeaderTimeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	server := &http.Server{BaseContext: func(net.Listener) context.Context { return ctx }, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.serveBrowserProxy(w, r, transport)
	}), ReadHeaderTimeout: 10 * time.Second}
	go server.Serve(ln)
	return "http://" + ln.Addr().String(), func() { cancel(); server.Close(); transport.CloseIdleConnections() }, nil
}

func (a *App) serveBrowserProxy(w http.ResponseWriter, r *http.Request, transport *http.Transport) {
	a.mu.Lock()
	locked := a.locked
	a.mu.Unlock()
	if locked {
		http.Error(w, "Unlock Womprat before browsing", http.StatusLocked)
		return
	}
	if r.Method == http.MethodConnect {
		addr := r.Host
		host, port, err := net.SplitHostPort(addr)
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || number < 1 || number > 65535 || host == "" || strings.ContainsAny(addr, " /?#@\\") {
			http.Error(w, "Invalid tunnel destination", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), socksDialTimeout)
		a.mu.Lock()
		ts := a.tsServer
		a.mu.Unlock()
		remote, err := dialTSNetPreferIPv4(ctx, ts, addr)
		cancel()
		if err != nil {
			http.Error(w, "Tailnet connection unavailable", http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			remote.Close()
			http.Error(w, "Tunnel unavailable", http.StatusInternalServerError)
			return
		}
		client, buffered, err := hijacker.Hijack()
		if err != nil {
			remote.Close()
			return
		}
		defer client.Close()
		defer remote.Close()
		stopCancel := context.AfterFunc(r.Context(), func() { client.Close(); remote.Close() })
		defer stopCancel()
		if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := buffered.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { io.Copy(remote, buffered); halfCloseWrite(remote); close(done) }()
		io.Copy(client, remote)
		halfCloseWrite(client)
		// A disconnected browser must not leave its upload relay blocked.
		client.Close()
		remote.Close()
		<-done
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" || r.URL.User != nil || r.Host != r.URL.Host {
		http.Error(w, "Absolute HTTP destination required", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	upgrade := strings.EqualFold(out.Header.Get("Upgrade"), "websocket") && proxyHeaderToken(out.Header.Get("Connection"), "upgrade")
	removeProxyHeaders(out.Header)
	if upgrade {
		out.Header.Set("Connection", "Upgrade")
		out.Header.Set("Upgrade", "websocket")
	}
	response, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "Tailnet connection unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusSwitchingProtocols {
		remote, ok := response.Body.(io.ReadWriteCloser)
		hijacker, canHijack := w.(http.Hijacker)
		if !upgrade || !ok || !canHijack {
			http.Error(w, "Invalid protocol upgrade", http.StatusBadGateway)
			return
		}
		client, buffered, err := hijacker.Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		stopCancel := context.AfterFunc(r.Context(), func() { client.Close(); remote.Close() })
		defer stopCancel()
		removeProxyHeaders(response.Header)
		response.Header.Set("Connection", "Upgrade")
		response.Header.Set("Upgrade", "websocket")
		if _, err := buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\n"); err != nil {
			return
		}
		if err := response.Header.Write(buffered); err != nil {
			return
		}
		if _, err := buffered.WriteString("\r\n"); err != nil {
			return
		}
		if err := buffered.Flush(); err != nil {
			return
		}
		done := make(chan struct{})
		go func() { io.Copy(remote, buffered); remote.Close(); close(done) }()
		io.Copy(client, remote)
		client.Close()
		remote.Close()
		<-done
		return
	}
	removeProxyHeaders(response.Header)
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	io.Copy(w, response.Body)
}

func proxyHeaderToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func removeProxyHeaders(header http.Header) {
	for _, name := range strings.Split(header.Get("Connection"), ",") {
		header.Del(strings.TrimSpace(name))
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "Transfer-Encoding", "TE", "Trailer", "Upgrade"} {
		header.Del(name)
	}
}
