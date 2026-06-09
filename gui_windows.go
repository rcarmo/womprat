//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	hostUser32              = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassExW    = hostUser32.NewProc("RegisterClassExW")
	procCreateWindowExWHost = hostUser32.NewProc("CreateWindowExW")
	procDefWindowProcW      = hostUser32.NewProc("DefWindowProcW")
	procShowWindowHost      = hostUser32.NewProc("ShowWindow")
	procUpdateWindow        = hostUser32.NewProc("UpdateWindow")
	procGetMessageW         = hostUser32.NewProc("GetMessageW")
	procTranslateMessage    = hostUser32.NewProc("TranslateMessage")
	procDispatchMessageW    = hostUser32.NewProc("DispatchMessageW")
	procPostQuitMessage     = hostUser32.NewProc("PostQuitMessage")
)

const (
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = ^uintptr(0x7fffffff)
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

var activeHost *nativeContentManager

func hostWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmSize:
		if activeHost != nil {
			activeHost.resizeAll()
		}
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func createHostWindow(title string, width, height int) (uintptr, error) {
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)
	className, _ := windows.UTF16PtrFromString("womprat-host")
	wc := wndClassExW{CbSize: uint32(unsafe.Sizeof(wndClassExW{})), HInstance: hinstance, LpszClassName: className, LpfnWndProc: windows.NewCallback(hostWndProc)}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	windowName, _ := windows.UTF16PtrFromString(title)
	hwnd, _, err := procCreateWindowExWHost.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, uintptr(width), uintptr(height), 0, 0, uintptr(hinstance), 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("create host window: %w", err)
	}
	procShowWindowHost.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	return hwnd, nil
}

func createHostChild(parent uintptr) (uintptr, error) {
	className, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, err := procCreateWindowExWHost.Call(0, uintptr(unsafe.Pointer(className)), 0,
		wsChild|wsVisible|wsClipChildren|wsClipSiblings, 0, 0, 100, 100, parent, 0, 0, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("create child window: %w", err)
	}
	return hwnd, nil
}

func runHostMessageLoop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func runGUI(app *App, shellURL string) {
	host, err := createHostWindow("womprat", 1200, 800)
	if err != nil {
		log.Fatal(err)
	}
	shellChild, err := createHostChild(host)
	if err != nil {
		log.Fatal(err)
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{Debug: true, AutoFocus: true, Window: unsafe.Pointer(shellChild), DataPath: webviewDataPath()})
	if w == nil {
		log.Fatal("Failed to create shell WebView2")
	}
	defer w.Destroy()
	app.webview = w
	contentViews, err := newNativeContentManager(host, shellChild, webviewDataPath(), w)
	if err != nil {
		log.Printf("content WebView manager unavailable: %v", err)
	} else {
		app.contentViews = contentViews
		activeHost = contentViews
		contentViews.HideAll()
	}

	w.Bind("womprat_getTabs", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		data, _ := json.Marshal(map[string]interface{}{"tabs": app.tabs, "activeTab": app.activeTab, "port": app.serverPort, "token": app.sessionToken})
		return string(data)
	})
	w.Bind("womprat_getNetworkState", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		data, _ := json.Marshal(map[string]interface{}{"exitNode": app.config.ExitNode, "exitActive": useExitNode})
		return string(data)
	})
	w.Bind("womprat_navigate", func(url string) { app.navigateBrowser(url) })
	w.Bind("womprat_switchTab", func(tabID string) { app.switchTab(tabID) })
	w.Bind("womprat_browserBack", func() {
		if view := app.activeContentView(); view != nil {
			view.GoBack()
		}
	})
	w.Bind("womprat_browserForward", func() {
		if view := app.activeContentView(); view != nil {
			view.GoForward()
		}
	})
	w.Bind("womprat_browserReload", func() {
		if view := app.activeContentView(); view != nil {
			view.Reload()
		}
	})
	w.Bind("womprat_updateTitle", func(title, url, favicon string) { app.updateActiveBrowserTitle(title, url, favicon) })
	w.Bind("womprat_closeTab", func(tabID string) { app.closeTab(tabID) })
	w.Bind("womprat_reorderTab", func(tabID string, toIndex int) { app.reorderTab(tabID, toIndex) })
	w.Bind("womprat_forgetTab", func(tabID string) { app.forgetTab(tabID) })
	w.Bind("womprat_newBrowser", func(url string) { app.newBrowserTab(url) })
	w.Bind("womprat_openSettings", func() { app.openSettingsTab() })
	w.Bind("womprat_newTerminal", func(host, user string, port int) { app.newTerminalTab(host, user, port) })
	w.Bind("womprat_registerLocalTab", func(tabJSON string) { app.registerLocalTab(tabJSON) })
	w.Bind("womprat_goHome", func() { app.goHome() })
	w.Bind("womprat_clearActiveTab", func() { app.clearActiveTab() })

	w.Navigate(fmt.Sprintf("%s?v=%d", shellURL, time.Now().UnixMilli()))
	if contentViews != nil {
		contentViews.HideAll()
	}
	runHostMessageLoop()
}
