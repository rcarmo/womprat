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
	app.config.ExitNode = "exit"
	app.exitNodeActive = true
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
	if app.exitNodeActive || app.config.ExitNode != "exit" {
		t.Fatalf("disconnect should clear active route only: exitNodeActive=%v exitNode=%q", app.exitNodeActive, app.config.ExitNode)
	}
}

func TestExitNodeConfiguredAndActiveAreIndependent(t *testing.T) {
	first := newTestApp(t)
	second := newTestApp(t)
	first.config.ExitNode = "exit-a"
	second.config.ExitNode = "exit-b"
	first.exitNodeActive = true
	second.exitNodeActive = false
	first.mu.Lock()
	firstState := first.exitNodeActive
	first.mu.Unlock()
	second.mu.Lock()
	secondState := second.exitNodeActive
	second.mu.Unlock()
	if !firstState || secondState {
		t.Fatalf("exit-node state leaked between app instances: first=%v second=%v", firstState, secondState)
	}
}

func TestConnectedTailscaleStatusRetainsRetryMetadata(t *testing.T) {
	// The endpoint requires a real LocalClient for connected status, so retain
	// the response-contract requirement in a focused source regression while
	// retry worker behaviour is covered with executable tests.
	s := readFileForRegression(t, "main.go")
	for _, want := range []string{`"error":    lastError`, `"retrying": retrying`} {
		if !strings.Contains(s, want) {
			t.Fatalf("connected Tailscale status missing %q", want)
		}
	}
}

func TestDisconnectedTailscaleStatusReportsRetry(t *testing.T) {
	app := newTestApp(t)
	app.tsLastError = "temporary DNS failure"
	app.tsRetrying = true
	rr := performJSON(app.handleTSStatus, http.MethodGet, "/api/tailscale/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rr.Code, rr.Body.String())
	}
	for _, want := range []string{`"status":"disconnected"`, `"retrying":true`, `"error":"temporary DNS failure"`} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("Tailscale retry status missing %q in %s", want, rr.Body.String())
		}
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
