package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHAuthPasswordPendingNoTailscale(t *testing.T) {
	app := newTestApp(t)
	app.pendingAuth["p"] = &pendingSSH{host: "smith", user: "root", port: 22}
	rr := performJSON(app.handleSSHAuthPassword, "POST", "/api/ssh/auth-password", map[string]string{"tabId": "p", "password": "x"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("pending no tailscale = %d %s", rr.Code, rr.Body.String())
	}
	if app.pendingAuth["p"] == nil {
		t.Fatal("pending auth should survive retryable no-tailscale failure")
	}
}

func TestSSHConnectValidationBranches(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSSHConnect, "POST", "/api/ssh/connect", map[string]any{"host": "smith", "user": "", "port": 0})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("connect no tailscale = %d %s", rr.Code, rr.Body.String())
	}
}

func TestSSHKeyImportRejectsOversizedContent(t *testing.T) {
	app := newTestApp(t)
	if err := app.importSSHKey("big", strings.Repeat("x", maxSSHKeyBytes+1)); err == nil {
		t.Fatal("oversized SSH key import succeeded")
	}
}

func TestValidSSHKeyImportAndFingerprint(t *testing.T) {
	app := newTestApp(t)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "valid")
	if err != nil {
		t.Fatal(err)
	}
	pemData := string(pem.EncodeToMemory(block))
	if err := app.importSSHKey("valid", pemData); err != nil {
		t.Fatalf("import valid key = %v", err)
	}
	fp := fingerprintFromPEM(pemData)
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("fingerprint = %q", fp)
	}
	if got := fingerprintFromPEM("not a key"); got != "—" {
		t.Fatalf("bad fingerprint = %q", got)
	}
}

func TestSSHWebSocketRejectsInvalidTabID(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSSHWebSocketFull, "GET", "/api/ssh/ws?tab=bad/id", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid ssh websocket tab = %d", rr.Code)
	}
}

func TestSSHResizePostNoContent(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSSHResize, "POST", "/api/ssh/resize", nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("ssh resize POST = %d", rr.Code)
	}
}

func TestWebSocketHandlersRejectPost(t *testing.T) {
	app := newTestApp(t)
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
	}{
		{"ssh-ws", app.handleSSHWebSocketFull},
		{"vnc-ws", app.handleVNCWebSocket},
		{"rdp-ws", app.handleRDPWebSocket},
	} {
		rr := performJSON(tc.h, "POST", "/", nil)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s POST = %d", tc.name, rr.Code)
		}
	}
}

func TestReadOnlyHandlersRejectPost(t *testing.T) {
	app := newTestApp(t)
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
	}{
		{"auth-status", app.handleAuthStatus},
		{"ts-status", app.handleTSStatus},
		{"ts-peers", app.handleTSPeers},
		{"about", app.handleAbout},
		{"browser-data", app.handleBrowserData},
		{"download-status", app.handleDownloadStatus},
		{"config", app.handleGetConfig},
	} {
		rr := performJSON(tc.h, "POST", "/", nil)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s POST = %d", tc.name, rr.Code)
		}
	}
}

func TestMethodBranches(t *testing.T) {
	app := newTestApp(t)
	cases := []struct {
		name   string
		h      http.HandlerFunc
		method string
	}{
		{"master", app.handleSetMasterPassword, "GET"},
		{"tailscale-key", app.handleSetTailscaleKey, "GET"},
		{"tailscale-disconnect", app.handleTailscaleDisconnect, "GET"},
		{"generate-key", app.handleGenerateSSHKey, "GET"},
		{"appearance", app.handleAppearance, "GET"},
		{"save-tabs", app.handleSaveTabs, "GET"},
		{"ssh-resize", app.handleSSHResize, "GET"},
		{"clear-cache", app.handleClearCache, "GET"},
		{"clear-cookies", app.handleClearCookies, "GET"},
		{"clear-passwords", app.handleClearPasswords, "GET"},
		{"clear-all", app.handleClearAll, "GET"},
		{"save-passwords", app.handleSavePasswordsToggle, "GET"},
	}
	for _, tc := range cases {
		rr := performJSON(tc.h, tc.method, "/", nil)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s method branch = %d", tc.name, rr.Code)
		}
	}
	rr := performJSON(app.handleSSHKeys, "PUT", "/api/settings/ssh-keys", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("ssh keys PUT = %d", rr.Code)
	}
	rr = performJSON(app.handleHosts, "POST", "/api/settings/hosts/smith", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("hosts POST = %d", rr.Code)
	}
	rr = performJSON(app.handleExitNode, "PUT", "/api/settings/exit-node", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("exit PUT = %d", rr.Code)
	}
}

func TestModuleVersion(t *testing.T) {
	if got := moduleVersion("definitely/not/a/module"); got != "bundled" {
		t.Fatalf("unknown moduleVersion = %q", got)
	}
	if got := moduleVersion("github.com/rcarmo/womprat"); got == "" {
		t.Fatal("empty main module version")
	}
}
