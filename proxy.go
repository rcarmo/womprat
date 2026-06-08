package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// handleProxy proxies HTTP requests through the tsnet connection
// URL format: /api/proxy?url=http://hostname:port/path
func (a *App) handleProxy(w http.ResponseWriter, r *http.Request) {
	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing url param", 400)
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "invalid url: "+err.Error(), 400)
		return
	}

	if a.tsServer == nil {
		http.Error(w, "tailscale not connected", 503)
		return
	}

	// Create an HTTP client that dials through tsnet
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return a.tsServer.Dial(ctx, network, addr)
			},
		},
		Timeout: 30 * time.Second,
	}

	// Build the proxied request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Copy relevant headers
	for _, h := range []string{"Content-Type", "Accept", "Authorization"} {
		if v := r.Header.Get(h); v != "" {
			proxyReq.Header.Set(h, v)
		}
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "proxy error: "+err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	// Rewrite absolute URLs in HTML responses to go through our proxy
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		// Rewrite src/href attributes pointing to the same host
		html := string(body)
		baseURL := parsed.Scheme + "://" + parsed.Host
		html = strings.ReplaceAll(html, `src="/`, `src="/api/proxy?url=`+url.QueryEscape(baseURL)+`/`)
		html = strings.ReplaceAll(html, `href="/`, `href="/api/proxy?url=`+url.QueryEscape(baseURL)+`/`)
		w.Write([]byte(html))
		return
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
