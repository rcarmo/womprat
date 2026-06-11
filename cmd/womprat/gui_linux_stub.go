//go:build linux && !webkitgui

package main

// Default Linux build: no embedded webview (keeps builds/tests cgo-free). Use
// `-tags webkitgui` to build the real WebKitGTK GUI. Without it, run as a
// headless shell/API server for scripted automation.
func runGUI(app *App, shellURL string) {
	runHeadlessServer(app, shellURL)
}
