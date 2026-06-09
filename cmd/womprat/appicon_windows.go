//go:build windows

package main

import (
	"syscall"

	webview2 "github.com/jchv/go-webview2"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32Icon         = syscall.NewLazyDLL("kernel32.dll")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procSetClassLongPtrW = user32.NewProc("SetClassLongPtrW")
	procGetModuleHandleW = kernel32Icon.NewProc("GetModuleHandleW")
)

const (
	appIconResourceID = 1

	imageIcon      = 1
	lrDefaultColor = 0x00000000

	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1

	// Negative Win32 class offsets represented as uintptr values for syscall.
	gclpHIcon   = ^uintptr(13) // -14
	gclpHIconSm = ^uintptr(33) // -34
)

func applyAppIconToHWND(hwnd uintptr) {
	if hwnd == 0 {
		return
	}

	hinst, _, _ := procGetModuleHandleW.Call(0)
	if hinst == 0 {
		return
	}

	big := loadAppIcon(hinst, 32, 32)
	small := loadAppIcon(hinst, 16, 16)
	if big != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, big)
		procSetClassLongPtrW.Call(hwnd, gclpHIcon, big)
	}
	if small != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, small)
		procSetClassLongPtrW.Call(hwnd, gclpHIconSm, small)
	}
}

func applyAppIcon(w webview2.WebView) {
	applyAppIconToHWND(uintptr(w.Window()))
}

func loadAppIcon(hinst uintptr, cx, cy int) uintptr {
	icon, _, _ := procLoadImageW.Call(
		hinst,
		uintptr(appIconResourceID), // MAKEINTRESOURCE(1)
		uintptr(imageIcon),
		uintptr(cx),
		uintptr(cy),
		uintptr(lrDefaultColor),
	)
	return icon
}
