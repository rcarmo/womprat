package main

import "os"

func init() {
	// Use a PAC script served from our local HTTP server.
	// The PAC script dynamically routes:
	//   - tailnet IPs (100.64.*) → SOCKS5 proxy
	//   - .local domains → DIRECT (mDNS)
	//   - everything else → SOCKS5 only if exit node is active, else DIRECT
	//
	// The PAC URL uses a placeholder port that gets replaced in serveFrontend
	// Actually: we know our port at startup from the listener. Use port 0 trick.
	// Simpler: hardcode a known port for the PAC. Or just use a broad rule.
	//
	// Simplest that works: proxy everything through SOCKS, and in the SOCKS handler
	// dial public internet directly (not through tsnet) when no exit node.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"--proxy-server=socks5://127.0.0.1:1080 --proxy-bypass-list=127.0.0.1;localhost --force-dark-mode --enable-features=WebContentsForceDark")
}
