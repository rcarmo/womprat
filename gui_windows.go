//go:build windows

package main

import (
	"fmt"
	"log"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

func runGUI(app *App, shellURL string) {
	// Create WebView2
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     true,
		AutoFocus: true,
		DataPath:  webviewDataPath(),
		WindowOptions: webview2.WindowOptions{
			Title:  "womprat",
			Width:  1200,
			Height: 800,
		},
	})
	if w == nil {
		log.Fatal("Failed to create WebView2 window")
	}
	defer w.Destroy()
	app.webview = w

	// Set native title bar appearance
	applyDarkMode(w)
	applyAppIcon(w)

	// Bind Go functions callable from JS in any page
	w.Bind("womprat_getTabs", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		data, _ := json.Marshal(map[string]interface{}{
			"tabs":      app.tabs,
			"activeTab": app.activeTab,
			"port":      app.serverPort,
			"token":     app.sessionToken,
		})
		return string(data)
	})

	w.Bind("womprat_getNetworkState", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		data, _ := json.Marshal(map[string]interface{}{
			"exitNode":   app.config.ExitNode,
			"exitActive": useExitNode,
		})
		return string(data)
	})

	w.Bind("womprat_navigate", func(url string) {
		app.navigateBrowser(url)
	})

	w.Bind("womprat_switchTab", func(tabID string) {
		app.switchTab(tabID)
	})

	w.Bind("womprat_updateTitle", func(title, url, favicon string) {
		app.updateActiveBrowserTitle(title, url, favicon)
	})

	w.Bind("womprat_closeTab", func(tabID string) {
		app.closeTab(tabID)
	})

	w.Bind("womprat_reorderTab", func(tabID string, toIndex int) {
		app.reorderTab(tabID, toIndex)
	})

	w.Bind("womprat_forgetTab", func(tabID string) {
		app.forgetTab(tabID)
	})

	w.Bind("womprat_newBrowser", func(url string) {
		app.newBrowserTab(url)
	})

	w.Bind("womprat_openSettings", func() {
		app.openSettingsTab()
	})

	w.Bind("womprat_newTerminal", func(host, user string, port int) {
		app.newTerminalTab(host, user, port)
	})

	w.Bind("womprat_registerLocalTab", func(tabJSON string) {
		app.registerLocalTab(tabJSON)
	})

	w.Bind("womprat_goHome", func() {
		app.goHome()
	})

	w.Bind("womprat_clearActiveTab", func() {
		app.clearActiveTab()
	})

	// Inject floating chrome overlay into every page
	w.Init(chromeOverlayJS(app.serverPort, app.sessionToken))

	// Navigate to shell with a cache-buster so stale WebView2 shell HTML/JS
	// cannot resurrect removed iframe code paths.
	w.Navigate(fmt.Sprintf("%s?v=%d", shellURL, time.Now().UnixMilli()))

	w.Run()
}
