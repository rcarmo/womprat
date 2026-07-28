//go:build windows

package main

import "golang.org/x/sys/windows"

var (
	consoleKernel32       = windows.NewLazySystemDLL("kernel32.dll")
	consoleUser32         = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow  = consoleKernel32.NewProc("GetConsoleWindow")
	procShowConsoleWindow = consoleUser32.NewProc("ShowWindow")
)

const (
	consoleHide = 0
	consoleShow = 5
)

// setConsoleVisible keeps the Windows console aligned with debug logging.
// ShowWindow is reversible, unlike FreeConsole, so logging can be enabled and
// disabled without restarting the application.
func setConsoleVisible(visible bool) {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return
	}
	command := uintptr(consoleHide)
	if visible {
		command = consoleShow
	}
	_, _, _ = procShowConsoleWindow.Call(hwnd, uintptr(command))
}
