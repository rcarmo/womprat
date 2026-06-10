package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHTTPError(t *testing.T) {
	rr := httptest.NewRecorder()
	httpError(rr, http.StatusTeapot, "short", "detail")
	if rr.Code != http.StatusTeapot || !strings.Contains(rr.Body.String(), "short") || !strings.Contains(rr.Body.String(), "detail") {
		t.Fatalf("httpError = %d %s", rr.Code, rr.Body.String())
	}
}

func TestShouldStartLocked(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if shouldStartLocked(nil) || shouldStartLocked(&AppConfig{UnlockMethod: "dpapi"}) {
		t.Fatal("dpapi/nil config should not start locked")
	}
	if shouldStartLocked(&AppConfig{UnlockMethod: "master"}) {
		t.Fatal("master without hash should not start locked")
	}
	if err := SaveCredential("master-hash", "not-json"); err != nil {
		t.Fatal(err)
	}
	if !shouldStartLocked(&AppConfig{UnlockMethod: "master"}) {
		t.Fatal("master with hash should start locked")
	}
}

func TestVerifyMasterPasswordInvalidRecords(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if ok, err := verifyMasterPassword(""); ok || err != nil {
		t.Fatalf("empty password = %v %v", ok, err)
	}
	if _, err := verifyMasterPassword("x"); err == nil {
		t.Fatal("missing hash should error")
	}
	if err := SaveCredential("master-hash", `{"kdf":"bad"}`); err != nil {
		t.Fatal(err)
	}
	if ok, err := verifyMasterPassword("x"); ok || err != nil {
		t.Fatalf("bad kdf = %v %v", ok, err)
	}
	cases := []masterHashRecord{
		{KDF: masterKDF, Iterations: minMasterKDFIterations - 1, Salt: base64.StdEncoding.EncodeToString(make([]byte, masterSaltBytes)), Hash: base64.StdEncoding.EncodeToString(make([]byte, masterHashBytes))},
		{KDF: masterKDF, Iterations: masterKDFIterations, Salt: base64.StdEncoding.EncodeToString(make([]byte, masterSaltBytes-1)), Hash: base64.StdEncoding.EncodeToString(make([]byte, masterHashBytes))},
		{KDF: masterKDF, Iterations: masterKDFIterations, Salt: base64.StdEncoding.EncodeToString(make([]byte, masterSaltBytes)), Hash: base64.StdEncoding.EncodeToString(make([]byte, masterHashBytes-1))},
	}
	for _, rec := range cases {
		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := SaveCredential("master-hash", string(data)); err != nil {
			t.Fatal(err)
		}
		if ok, err := verifyMasterPassword("x"); ok || err != nil {
			t.Fatalf("invalid record accepted: %+v ok=%v err=%v", rec, ok, err)
		}
	}
}

func TestDownloadStatusAndError(t *testing.T) {
	app := newTestApp(t)
	currentDownload = nil
	rr := performJSON(app.handleDownloadStatus, "GET", "/api/download/status", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "idle") {
		t.Fatalf("idle status = %d %s", rr.Code, rr.Body.String())
	}
	st := &downloadState{Filename: "x", Status: "downloading"}
	setDownloadError(st, errors.New("boom"))
	if st.Status != "error" || st.Error != "boom" {
		t.Fatalf("download error state = %+v", st)
	}
	currentDownload = st
	rr = performJSON(app.handleDownloadStatus, "GET", "/api/download/status", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("error status = %d %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDownloadValidation(t *testing.T) {
	app := newTestApp(t)
	cases := []string{"/api/download", "/api/download?url=file:///tmp/x", "/api/download?url=not-a-url"}
	for _, path := range cases {
		rr := performJSON(app.handleDownload, "GET", path, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d", path, rr.Code)
		}
	}
}

func TestBrowserHelpers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := webviewDataPath(); !strings.HasSuffix(got, "webview2") {
		t.Fatalf("webviewDataPath = %q", got)
	}
	if got := getCacheSize(); got == "" {
		t.Fatal("empty cache size")
	}
	if got := listCookieDomains(); len(got) != 0 {
		t.Fatalf("unexpected cookies: %+v", got)
	}
	paths := cookieDBPaths()
	if len(paths) != 2 || firstExistingCookieDB() != "" {
		t.Fatalf("cookie paths/db = %+v %q", paths, firstExistingCookieDB())
	}
	deleteCookiesForDomain("example.com")
	clearAllSavedPasswords()
}

func TestGetDownloadsDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	got := getDownloadsDir()
	if !strings.HasPrefix(got, dir) || !strings.HasSuffix(got, "Downloads") {
		t.Fatalf("downloads dir = %q", got)
	}
}

func TestDeleteCredentialMissingOK(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := DeleteCredential("missing"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil || cfg == nil || cfg.Hosts == nil {
		t.Fatalf("LoadConfig corrupt = %+v %v", cfg, err)
	}
}
