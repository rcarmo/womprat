package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeFrontend(t *testing.T) {
	app := newTestApp(t)
	app.serverPort = 12345
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	app.serveFrontend(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "window.__SESSION_TOKEN") || rr.Header().Get("Cache-Control") == "" {
		t.Fatalf("serve index = %d headers=%v body=%q", rr.Code, rr.Header(), rr.Body.String()[:min(80, rr.Body.Len())])
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/settings.html", nil)
	app.serveFrontend(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "window.__SESSION_TOKEN") || !strings.Contains(rr.Body.String(), "const TOKEN = window.__SESSION_TOKEN || '';") {
		t.Fatalf("serve settings = %d body=%q", rr.Code, rr.Body.String()[:min(120, rr.Body.Len())])
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/vendor/xterm.css", nil)
	app.serveFrontend(rr, req)
	if rr.Code != 200 || rr.Header().Get("Content-Type") != "text/css" {
		t.Fatalf("serve css = %d %q", rr.Code, rr.Header().Get("Content-Type"))
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/missing", nil)
	app.serveFrontend(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing = %d", rr.Code)
	}
}

func TestDisconnectedTailscaleHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleTSStatus, "GET", "/api/tailscale/status", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "disconnected") {
		t.Fatalf("ts status = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleTSPeers, "GET", "/api/tailscale/peers", nil)
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "[]" {
		t.Fatalf("peers = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleTailscaleDisconnect, "POST", "/api/settings/tailscale-disconnect", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "disconnected") {
		t.Fatalf("disconnect = %d %s", rr.Code, rr.Body.String())
	}
}

func TestAboutAndConfigHandlers(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "b", Type: "browser", URL: "http://example"}}
	app.config.ExitNode = "exit"
	app.config.RestoreTabs = true
	app.config.AutoConnect = true
	rr := performJSON(app.handleAbout, "GET", "/api/about", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "womprat") || !strings.Contains(rr.Body.String(), "exit") {
		t.Fatalf("about = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleGetConfig, "GET", "/api/settings/config", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "exit") {
		t.Fatalf("config = %d %s", rr.Code, rr.Body.String())
	}
}

func TestExitNodeHandlerDisconnected(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleExitNode, "GET", "/api/settings/exit-node", nil)
	if rr.Code != 200 {
		t.Fatalf("get exit node = %d", rr.Code)
	}
	rr = performJSON(app.handleExitNode, "POST", "/api/settings/exit-node", map[string]string{"exitNode": ""})
	if rr.Code != 200 {
		t.Fatalf("clear exit node = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleExitNode, "POST", "/api/settings/exit-node", map[string]string{"exitNode": "router"})
	if rr.Code != 500 {
		t.Fatalf("set exit node without ts = %d", rr.Code)
	}
}
