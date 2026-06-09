package main

import (
	"os"
	"strings"
	"testing"
)

func TestWebView2ProxyArgumentsRouteThroughSOCKS(t *testing.T) {
	args := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	for _, want := range []string{
		"--proxy-server=socks5://127.0.0.1:1080",
		"--proxy-bypass-list=127.0.0.1",
		`--host-resolver-rules="MAP * ~NOTFOUND,EXCLUDE 127.0.0.1"`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS missing %q in %q", want, args)
		}
	}
	if strings.Contains(args, "socks5h://") {
		t.Fatalf("Chromium/WebView2 proxy args must not use curl socks5h scheme: %q", args)
	}
}
