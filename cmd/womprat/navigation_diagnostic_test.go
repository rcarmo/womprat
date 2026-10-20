package main

import (
	"strings"
	"testing"
)

func TestNavigationDiagnosticPreservesActualError(t *testing.T) {
	for _, code := range []string{"ERR_SSL_PROTOCOL_ERROR", "ERR_SSL_VERSION_OR_CIPHER_MISMATCH", "ERR_CERT_AUTHORITY_INVALID", "ERR_CONNECTION_CLOSED", "ERR_SOCKS_CONNECTION_FAILED"} {
		got := navigationFailureMessage(0, true, "", "net::"+code, "", "")
		if !strings.Contains(got, code) || strings.Contains(got, "Page failed to load") {
			t.Fatal("lost actual failure", got)
		}
	}
	got := navigationFailureMessage(0, true, "get_WebErrorStatus: HRESULT 0x80004005", "", "PEM unavailable", "Network.enable failed")
	for _, detail := range []string{"WebView2 error 0", "0x80004005", "PEM unavailable", "Network.enable failed"} {
		if !strings.Contains(got, detail) {
			t.Fatal("diagnostic omitted", got)
		}
	}
	for _, input := range []string{"<script>alert(1)</script>", "ERR_bad", "ERR_SSL_PROTOCOL_ERROR secret", "https://example.com/password"} {
		if chromiumNavigationMessage(input) != "" {
			t.Fatal("non-symbol diagnostic accepted", input)
		}
	}
}

func TestNavigationDiagnosticURLTracksExactNavigation(t *testing.T) {
	if navigationTarget("https://flint.local:8443/vault/06f2f9958d29e891#fragment") != "https://flint.local:8443/vault/06f2f9958d29e891" {
		t.Fatal("navigation target not preserved")
	}
	if navigationTarget("chrome-error://chromewebdata/") != "" {
		t.Fatal("internal error URL accepted")
	}
}
