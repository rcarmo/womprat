//go:build linux && webkitgui

package main

import (
	"fmt"
	"log"
	"os"

	webview "github.com/webview/webview_go"
)

// linuxShell adapts the embedded WebKitGTK webview to the shellWebView interface
// so the shared App methods (evalShell/navigate) work exactly as on Windows.
type linuxShell struct {
	w webview.WebView
}

func (s linuxShell) Navigate(url string) { s.w.Dispatch(func() { s.w.Navigate(url) }) }
func (s linuxShell) Eval(js string)      { s.w.Dispatch(func() { s.w.Eval(js) }) }
func (s linuxShell) Resize()             {}

// runGUI on Linux embeds a real WebKitGTK webview that loads the same shell and
// therefore exercises the identical URL-validation paths (openSpecialURL /
// parseCustomURL / normalizeBrowserURL) and the same SSH/VNC/RDP viewers that
// the Windows build uses. Browser tabs for external pages navigate the embedded
// webview itself (there is no separate WebView2 content host on Linux).
//
// Set WOMPRAT_HEADLESS=1 (or run without a DISPLAY) to fall back to a plain HTTP
// server for scripted/Xvfb-less automation.
func runGUI(app *App, shellURL string) {
	if os.Getenv("WOMPRAT_HEADLESS") == "1" || os.Getenv("DISPLAY") == "" {
		runHeadlessServer(app, shellURL)
		return
	}

	w := webview.New(app.config.DebugLog)
	defer w.Destroy()
	w.SetTitle("womprat")
	w.SetSize(1200, 800, webview.HintNone)

	shell := linuxShell{w: w}
	app.webview = shell
	app.dispatch = w.Dispatch

	// External (http/https) browser navigation: the shell calls these. With no
	// separate content host on Linux, navigate the embedded webview itself. Home
	// returns to the shell.
	w.Bind("womprat_newBrowser", func(url string) { shell.Navigate(url) })
	w.Bind("womprat_navigate", func(url string) { shell.Navigate(url) })
	w.Bind("womprat_goHome", func() { shell.Navigate(shellURL) })

	// Tab/session state used by the shell on startup and for local custom-scheme
	// viewers. These reuse the same App methods as Windows so URL validation and
	// the SSH/VNC/RDP viewers run through identical code paths.
	w.Bind("womprat_getTabs", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		return jsString(map[string]interface{}{"tabs": app.tabs, "activeTab": app.activeTab, "port": app.serverPort, "token": app.sessionToken})
	})
	w.Bind("womprat_getNetworkState", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		return jsString(map[string]interface{}{"exitNode": app.config.ExitNode, "exitActive": useExitNode})
	})
	w.Bind("womprat_newTerminal", func(host, user string, port int) { app.newTerminalTab(host, user, port) })
	w.Bind("womprat_newVNC", func(target string) { app.newVNCTab(target) })
	w.Bind("womprat_newRDP", func(target string) { app.newRDPTab(target) })
	w.Bind("womprat_registerLocalTab", func(tabJSON string) { app.registerLocalTab(tabJSON) })
	w.Bind("womprat_switchTab", func(tabID string) { app.switchTab(tabID) })
	w.Bind("womprat_closeTab", func(tabID string) { app.closeTab(tabID) })
	w.Bind("womprat_reorderTab", func(tabID string, toIndex int) { app.reorderTab(tabID, toIndex) })
	w.Bind("womprat_forgetTab", func(tabID string) { app.forgetTab(tabID) })
	w.Bind("womprat_openSettings", func() { app.openSettingsTab() })
	w.Bind("womprat_clearActiveTab", func() { app.clearActiveTab() })
	// No-op bridges that only matter for the native Windows content host.
	w.Bind("womprat_setChromeHeight", func(px int) {})
	w.Bind("womprat_focusShell", func() {})
	w.Bind("womprat_browserBack", func() { shell.Eval("history.back()") })
	w.Bind("womprat_browserForward", func() { shell.Eval("history.forward()") })
	w.Bind("womprat_browserReload", func() { shell.Eval("location.reload()") })

	log.Printf("womprat (linux/webkit2gtk): loading shell %s", shellURL)
	fmt.Printf("WOMPRAT_SHELL_URL=%s\n", shellURL)
	fmt.Printf("WOMPRAT_TOKEN=%s\n", app.sessionToken)
	w.Navigate(shellURL)
	w.Run()
}
