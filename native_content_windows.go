//go:build windows

package main

import (
	"fmt"
	"time"
	"unsafe"

	webview2edge "github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const browserChromeHeight = 84

var (
	contentUser32       = windows.NewLazySystemDLL("user32.dll")
	procCreateWindowExW = contentUser32.NewProc("CreateWindowExW")
	procDestroyWindow   = contentUser32.NewProc("DestroyWindow")
	procGetClientRect   = contentUser32.NewProc("GetClientRect")
	procSetWindowPos    = contentUser32.NewProc("SetWindowPos")
	procShowWindow      = contentUser32.NewProc("ShowWindow")
	procIsWindow        = contentUser32.NewProc("IsWindow")
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

type nativeContentView struct {
	parent uintptr
	hwnd   uintptr
	edge   *webview2edge.Chromium
}

func newNativeContentView(parent unsafe.Pointer, dataPath string) (*nativeContentView, error) {
	parentHWND := uintptr(parent)
	if parentHWND == 0 {
		return nil, fmt.Errorf("missing parent HWND")
	}
	className, _ := windows.UTF16PtrFromString("STATIC")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		0x40000000|0x10000000, // WS_CHILD | WS_VISIBLE
		0, browserChromeHeight, 100, 100,
		parentHWND,
		0,
		0,
		0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("create content child window: %w", err)
	}
	cv := &nativeContentView{parent: parentHWND, hwnd: hwnd}
	edge := webview2edge.NewChromium()
	edge.DataPath = dataPath
	if !edge.Embed(hwnd) {
		procDestroyWindow.Call(hwnd)
		return nil, fmt.Errorf("embed content WebView2")
	}
	cv.edge = edge
	cv.resize()
	go cv.resizeLoop()
	return cv, nil
}

func (v *nativeContentView) resizeLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if v == nil || v.hwnd == 0 {
			return
		}
		ok, _, _ := procIsWindow.Call(v.hwnd)
		if ok == 0 {
			return
		}
		v.resize()
	}
}

func (v *nativeContentView) resize() {
	if v == nil || v.parent == 0 || v.hwnd == 0 {
		return
	}
	var r winRect
	procGetClientRect.Call(v.parent, uintptr(unsafe.Pointer(&r)))
	width := r.Right - r.Left
	height := r.Bottom - r.Top - browserChromeHeight
	if height < 0 {
		height = 0
	}
	procSetWindowPos.Call(v.hwnd, 0, 0, browserChromeHeight, uintptr(width), uintptr(height), 0x0010|0x0004) // SWP_NOACTIVATE | SWP_NOZORDER
	if v.edge != nil {
		v.edge.Resize()
	}
}

func (v *nativeContentView) Navigate(url string) {
	if v == nil || v.edge == nil {
		return
	}
	v.edge.Navigate(url)
}

func (v *nativeContentView) GoBack() {
	if v == nil || v.edge == nil {
		return
	}
	v.edge.Eval("history.back()")
}

func (v *nativeContentView) GoForward() {
	if v == nil || v.edge == nil {
		return
	}
	v.edge.Eval("history.forward()")
}

func (v *nativeContentView) Reload() {
	if v == nil || v.edge == nil {
		return
	}
	v.edge.Eval("location.reload()")
}

func (v *nativeContentView) Show() {
	if v == nil || v.hwnd == 0 {
		return
	}
	v.resize()
	procShowWindow.Call(v.hwnd, 5) // SW_SHOW
}

func (v *nativeContentView) Hide() {
	if v == nil || v.hwnd == 0 {
		return
	}
	procShowWindow.Call(v.hwnd, 0) // SW_HIDE
}
