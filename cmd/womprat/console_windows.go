//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	consoleKernel32           = windows.NewLazySystemDLL("kernel32.dll")
	consoleUser32             = windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow      = consoleKernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = consoleKernel32.NewProc("GetConsoleProcessList")
	procShowConsoleWindow     = consoleUser32.NewProc("ShowWindow")
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
	// A console inherited from cmd.exe belongs to the user's shell too.
	// Never hide/show it, or a pseudo-console shared with other processes.
	var processID uint32
	count, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&processID)), 1)
	if count != 1 || processID != uint32(os.Getpid()) {
		return
	}
	command := uintptr(consoleHide)
	if visible {
		command = consoleShow
	}
	_, _, _ = procShowConsoleWindow.Call(hwnd, uintptr(command))
}
