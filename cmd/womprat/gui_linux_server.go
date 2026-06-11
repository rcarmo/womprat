//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// runHeadlessServer keeps the shell/API server alive without a GUI, for scripted
// automation where no display is available or the webkit GUI is not built in.
func runHeadlessServer(app *App, shellURL string) {
	log.Printf("womprat (linux headless): shell at %s", shellURL)
	fmt.Printf("WOMPRAT_SHELL_URL=%s\n", shellURL)
	fmt.Printf("WOMPRAT_TOKEN=%s\n", app.sessionToken)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	log.Printf("womprat headless server shutting down")
}
