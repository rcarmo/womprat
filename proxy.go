package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

func (a *App) registerProxyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/p/", a.authMiddleware(a.handleBrowserProxy))
}

func (a *App) handleBrowserProxy(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Path[len("/p/"):]
	if q := r.URL.RawQuery; q != "" {
		rawURL += "?" + q
	}
	targetURL, err := url.PathUnescape(rawURL)
	if err != nil {
		httpError(w, 400, "Invalid URL", err.Error())
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		httpError(w, 400, "Invalid URL", err.Error())
		return
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" { port = "443" } else { port = "80" }
	}

	// Determine dial method
	var dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)
	
	a.mu.Lock()
	ts := a.tsServer
	a.mu.Unlock()

	if ts != nil && isTailnetDest(host) {
		dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return ts.Dial(ctx, "tcp", host+":"+port)
		}
	} else if ts != nil && useExitNode {
		dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return ts.Dial(ctx, "tcp", host+":"+port)
		}
	} else {
		dialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.DialTimeout("tcp", addr, 10*time.Second)
		}
	}

	transport := &http.Transport{
		DialContext: dialFunc,
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		httpError(w, 500, "Request error", err.Error())
		return
	}
	for k, vv := range r.Header {
		for _, v := range vv { proxyReq.Header.Add(k, v) }
	}
	proxyReq.Header.Del("Host")
	proxyReq.Host = parsed.Host

	resp, err := client.Do(proxyReq)
	if err != nil {
		httpError(w, 502, "Could not reach "+host, err.Error())
		return
	}
	defer resp.Body.Close()

	// Rewrite redirects
	if loc := resp.Header.Get("Location"); loc != "" {
		u, _ := url.Parse(loc)
		if u != nil {
			resolved := parsed.ResolveReference(u).String()
			resp.Header.Set("Location", "/p/"+url.PathEscape(resolved))
		}
	}

	// Strip CSP headers that block framing
	resp.Header.Del("Content-Security-Policy")
	resp.Header.Del("X-Frame-Options")

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv { w.Header().Add(k, v) }
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
