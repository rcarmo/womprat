//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	hostUser32              = windows.NewLazySystemDLL("user32.dll")
	hostGdi32               = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush    = hostGdi32.NewProc("CreateSolidBrush")
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

// wompratDarkColor is the Fluent dark surface used so the native host/child
// windows do not flash white before WebView2 paints.
const wompratDarkColor = 0x00202020 // COLORREF 0x00BBGGRR for #202020

func darkBrush() windows.Handle {
	h, _, _ := procCreateSolidBrush.Call(uintptr(wompratDarkColor))
	return windows.Handle(h)
}

var childClassName *uint16

func registerChildClass() {
	if childClassName != nil {
		return
	}
	var hinstance windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hinstance)
	childClassName, _ = windows.UTF16PtrFromString("womprat-child")
	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		HInstance:     hinstance,
		LpszClassName: childClassName,
		LpfnWndProc: windows.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
			r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
			return r
		}),
		HbrBackground: darkBrush(),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

const (
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmExitSizeMove     = 0x0232
	wmDpiChanged       = 0x02E0
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
	case wmSize, wmExitSizeMove, wmDpiChanged:
		if activeHost != nil {
			activeHost.resizeAll()
			activeHost.notifyShellResize()
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
	wc := wndClassExW{CbSize: uint32(unsafe.Sizeof(wndClassExW{})), HInstance: hinstance, LpszClassName: className, LpfnWndProc: windows.NewCallback(hostWndProc), HbrBackground: darkBrush()}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	windowName, _ := windows.UTF16PtrFromString(title)
	hwnd, _, err := procCreateWindowExWHost.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowName)), wsOverlappedWindow,
		cwUseDefault, cwUseDefault, uintptr(width), uintptr(height), 0, 0, uintptr(hinstance), 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("create host window: %w", err)
	}
	return hwnd, nil
}

func showHostWindow(hwnd uintptr) {
	procShowWindowHost.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
}

func resizeChildToClient(parent, child uintptr, top, bottomInset int32) {
	var r winRect
	procGetClientRect.Call(parent, uintptr(unsafe.Pointer(&r)))
	width := r.Right - r.Left
	height := r.Bottom - r.Top - top - bottomInset
	if height < 0 {
		height = 0
	}
	procSetWindowPos.Call(child, hwndTop, 0, uintptr(top), uintptr(width), uintptr(height), swpNoActivate)
}

func createHostChild(parent uintptr) (uintptr, error) {
	registerChildClass()
	hwnd, _, err := procCreateWindowExWHost.Call(0, uintptr(unsafe.Pointer(childClassName)), 0,
		wsChild|wsVisible|wsClipChildren|wsClipSiblings, 0, 0, 100, 100, parent, 0, 0, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("create child window: %w", err)
	}
	return hwnd, nil
}

func runGUI(app *App, shellURL string) {
	log.Printf("gui: creating native host window")
	host, err := createHostWindow("womprat", 1200, 800)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("gui: host hwnd=0x%x", host)
	applyDarkModeToHWND(host)
	applyAppIconToHWND(host)
	shellChild, err := createHostChild(host)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("gui: shell child hwnd=0x%x", shellChild)
	resizeChildToClient(host, shellChild, 0, 0)
	w := webview2.NewWithOptions(webview2.WebViewOptions{Debug: app.config.DebugLog, AutoFocus: true, Window: unsafe.Pointer(shellChild), DataPath: webviewDataPath()})
	if w == nil {
		log.Fatal("Failed to create shell WebView2")
	}
	log.Printf("gui: shell WebView2 created")
	defer w.Destroy()
	app.webview = w
	app.dispatch = w.Dispatch
	applyDarkMode(w)
	applyAppIcon(w)
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
	w.Bind("womprat_setChromeHeight", func(px int) {
		setReportedChromePx(int32(px))
		if activeHost != nil {
			activeHost.resizeAll()
		}
	})

	// Defer showing the host window until the shell DOM is ready so WebView2's
	// default white surface never flashes before the dark shell paints.
	var showOnce sync.Once
	showShell := func() {
		showOnce.Do(func() {
			resizeChildToClient(host, shellChild, 0, 0)
			w.Resize()
			if contentViews != nil {
				contentViews.HideAll()
			}
			showHostWindow(host)
		})
	}
	w.Bind("womprat_shellReady", func() { showShell() })
	w.Init(`document.addEventListener('DOMContentLoaded',function(){try{window.womprat_shellReady&&window.womprat_shellReady();}catch(e){}});window.addEventListener('load',function(){try{window.womprat_shellReady&&window.womprat_shellReady();}catch(e){}});`)
	// Fallback: show anyway shortly after launch if the ready signal never fires.
	time.AfterFunc(2500*time.Millisecond, func() { w.Dispatch(showShell) })

	log.Printf("gui: navigating shell to %s", shellURL)
	w.Navigate(fmt.Sprintf("%s?v=%d", shellURL, time.Now().UnixMilli()))
	if contentViews != nil {
		contentViews.HideAll()
	}
	log.Printf("gui: entering run loop")
	w.Run()
}
