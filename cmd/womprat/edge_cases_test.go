package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestNonWindowsStubsAndPaths(t *testing.T) {
	// runGUI intentionally blocks while serving the headless shell and cannot be
	// called as a synchronous stub test. Exercise only the non-blocking helpers.
	applyDarkMode(nil)
	applyAppIcon(nil)
	if got := tsnetStateDir(); !strings.Contains(got, "womprat") || !strings.HasSuffix(got, "tsnet") {
		t.Fatalf("tsnetStateDir = %q", got)
	}
}

func TestForgetTab(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "a"}, {ID: "b"}}
	app.activeTab = "a"
	app.forgetTab("a")
	if app.activeTab != "" || len(app.tabs) != 1 || app.tabs[0].ID != "b" {
		t.Fatalf("forget active = active %q tabs %+v", app.activeTab, app.tabs)
	}
	app.forgetTab("missing")
	if len(app.tabs) != 1 {
		t.Fatalf("forget missing changed tabs: %+v", app.tabs)
	}
}

func TestHostKeyCallbackTOFUAndMismatch(t *testing.T) {
	app := newTestApp(t)
	_, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer1, err := ssh.NewSignerFromKey(priv1)
	if err != nil {
		t.Fatal(err)
	}
	cb := app.hostKeyCallback("smith")
	if err := cb("smith:22", nil, signer1.PublicKey()); err != nil {
		t.Fatalf("first host key = %v", err)
	}
	if app.config.Hosts["smith"].HostKey == "" {
		t.Fatal("host key not stored")
	}
	if err := cb("smith:22", nil, signer1.PublicKey()); err != nil {
		t.Fatalf("same host key = %v", err)
	}
	_, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer2, err := ssh.NewSignerFromKey(priv2)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb("smith:22", nil, signer2.PublicKey()); err == nil {
		t.Fatal("expected host key mismatch")
	}
}

func TestHostKeyCallbackDoesNotChangeMemoryOnPersistFailure(t *testing.T) {
	app := newTestApp(t)
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.hostKeyCallback("smith")("smith:22", nil, signer.PublicKey()); err == nil {
		t.Fatal("expected host key persistence failure")
	}
	if app.config.Hosts["smith"].HostKey != "" {
		t.Fatalf("host key changed in memory despite persist failure: %+v", app.config.Hosts["smith"])
	}
}

func TestDownloadToFileNoTailscale(t *testing.T) {
	app := newTestApp(t)
	st := &downloadState{Status: "downloading"}
	app.downloadToFile("https://example.com/file.txt", t.TempDir()+"/file.txt", st)
	if st.Status != "error" || !strings.Contains(st.Error, "tailscale") {
		t.Fatalf("download no tailscale = %+v", st)
	}
}

func TestHandleDownloadStartsAndErrorsWithoutTailscale(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("HOME", t.TempDir())
	rr := performJSON(app.handleDownload, "GET", "/api/download?url=https://example.com/file.txt", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "started") {
		t.Fatalf("download start = %d %s", rr.Code, rr.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		downloadMu.Lock()
		st := currentDownload
		status, errText := "", ""
		if st != nil {
			status, errText = st.Status, st.Error
		}
		downloadMu.Unlock()
		if status == "error" && strings.Contains(errText, "tailscale") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not transition to tailscale error")
}

func TestSOCKSReplyAndRelay(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	if err := writeSOCKSReply(buf, 0); err != nil {
		t.Fatal(err)
	}
	if got := buf.Bytes(); len(got) != 10 || got[0] != 5 || got[1] != 0 {
		t.Fatalf("reply = %v", got)
	}

	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan struct{})
	go func() { relay(right, left); close(done) }()
	readDone := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 1)
		n, _ := right.Read(buf)
		readDone <- buf[:n]
	}()
	_, _ = left.Write([]byte("x"))
	if got := <-readDone; string(got) != "x" {
		t.Fatalf("relayed %q", got)
	}
	_ = left.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not return")
	}
}

func TestRegisterDownloadRoutes(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	app.registerDownloadRoutes(mux)
	req := httptest.NewRequest("GET", "/api/download/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("registered download route without token = %d", rr.Code)
	}
}
