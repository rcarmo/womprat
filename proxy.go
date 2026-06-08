package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

// startProxyServer starts an HTTP reverse proxy that routes through tsnet
// Browser tabs load URLs as: http://127.0.0.1:<proxyPort>/p/<encoded-url>
func (a *App) startProxyHandler(mux *http.ServeMux) {
	mux.HandleFunc("/p/", a.handleBrowserProxy)
}

func (a *App) handleBrowserProxy(w http.ResponseWriter, r *http.Request) {
	// URL is /p/<encoded-target-url>
	encodedURL := r.URL.Path[len("/p/"):]
	if q := r.URL.RawQuery; q != "" {
		encodedURL += "?" + q
	}

	targetURL, err := url.PathUnescape(encodedURL)
	if err != nil {
		httpError(w, 400, "Invalid URL", err.Error())
		return
	}

	if a.tsServer == nil {
		httpError(w, 503, "Tailscale not connected", "Open Settings and connect first.")
		return
	}

	parsed, err := url.Parse(targetURL)
	if err != nil {
		httpError(w, 400, "Invalid URL", err.Error())
		return
	}

	// Determine host:port for dialing
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// Create HTTP client that dials through tsnet
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return a.tsServer.Dial(ctx, "tcp", host+":"+port)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects — let the browser handle them
		},
	}

	// Forward the request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		httpError(w, 500, "Request error", err.Error())
		return
	}
	// Copy request headers
	for k, vv := range r.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}
	proxyReq.Header.Del("Host")
	proxyReq.Host = parsed.Host

	resp, err := client.Do(proxyReq)
	if err != nil {
		httpError(w, 502, "Could not reach "+host, err.Error())
		return
	}
	defer resp.Body.Close()

	// Handle redirects — rewrite Location header to stay proxied
	if loc := resp.Header.Get("Location"); loc != "" {
		absLoc := resolveURL(parsed, loc)
		resp.Header.Set("Location", "/p/"+url.PathEscape(absLoc))
	}

	// Copy response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func resolveURL(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

func init() {
	_ = log.Prefix // satisfy import
}
