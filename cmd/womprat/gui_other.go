//go:build !windows && !linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runGUI on non-Windows builds has no native WebView2 host. Instead of exiting
// (which would tear down the local HTTP server), it keeps the shell/API server
// running so the app can be driven end-to-end with an external browser under
// Xvfb + xdotool/Playwright for debugging. The shell URL and session token are
// printed to stdout so automation harnesses can find them, and the shell can be
// auto-opened by setting WOMPRAT_OPEN=1.
func runGUI(app *App, shellURL string) {
	log.Printf("womprat (non-Windows debug server): shell at %s", shellURL)
	// Machine-readable lines for automation. The served shell HTML already injects
	// the session token; the token is also printed here for direct API calls.
	fmt.Printf("WOMPRAT_SHELL_URL=%s\n", shellURL)
	fmt.Printf("WOMPRAT_TOKEN=%s\n", app.sessionToken)

	if os.Getenv("WOMPRAT_OPEN") == "1" {
		opener := os.Getenv("BROWSER")
		if opener == "" {
			opener = "xdg-open"
		}
		if err := exec.Command(opener, shellURL).Start(); err != nil {
			log.Printf("failed to open browser via %s: %v", opener, err)
		}
	}

	// Keep serving until interrupted so external automation can attach.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Printf("womprat debug server shutting down")
}
