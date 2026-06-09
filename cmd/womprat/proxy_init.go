package main

import "os"

func init() {
	// Route WebView2 through the local SOCKS5 listener. Chromium/WebView2 expects
	// the proxy URI scheme to be socks5://, not curl's socks5h:// spelling. SOCKS5
	// still carries domain names to the proxy, and the host-resolver rule below
	// prevents Chromium from satisfying DNS locally before the proxy can hand the
	// name to tsnet/Tailscale.
	//
	// Do not disable Chromium web security or frame-ancestor checks here: womprat
	// must behave like a normal browser over SOCKS, not a CSP/XFO bypass tool.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--proxy-server=socks5://127.0.0.1:1080 --proxy-bypass-list=127.0.0.1 --host-resolver-rules=\"MAP * ~NOTFOUND,EXCLUDE 127.0.0.1\" --force-dark-mode --enable-features=WebContentsForceDark")
}
