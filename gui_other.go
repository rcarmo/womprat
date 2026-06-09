//go:build !windows

package main

import "log"

func runGUI(app *App, shellURL string) {
	log.Printf("GUI is only available on Windows; local shell URL: %s", shellURL)
}
