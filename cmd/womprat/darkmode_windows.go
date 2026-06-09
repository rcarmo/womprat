//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2"
)

var (
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const DWMWA_USE_IMMERSIVE_DARK_MODE = 20

func applyDarkModeToHWND(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	var preference int32 = 1
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(DWMWA_USE_IMMERSIVE_DARK_MODE),
		uintptr(unsafe.Pointer(&preference)),
		uintptr(unsafe.Sizeof(preference)),
	)
}

func applyDarkMode(w webview2.WebView) {
	applyDarkModeToHWND(uintptr(w.Window()))
}
