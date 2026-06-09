package main

import "os"

func init() {
	// Route WebView2 through the local SOCKS5 listener. Use socks5h so Chromium
	// sends hostnames to the proxy instead of resolving them locally; this is
	// required for tailnet/exit-node DNS names such as .local aliases. The SOCKS
	// handler decides per destination whether to use tsnet (tailnet/MagicDNS, or
	// all traffic when exit-node mode is active) or a direct TCP dial for public
	// internet.
	//
	// Do not disable Chromium web security or frame-ancestor checks here: womprat
	// must behave like a normal browser over SOCKS, not a CSP/XFO bypass tool.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--proxy-server=socks5h://127.0.0.1:1080 --proxy-bypass-list=127.0.0.1 --force-dark-mode --enable-features=WebContentsForceDark")
}
