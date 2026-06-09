package main

import (
	"os"
	"strings"
	"testing"
)

func readFrontendUX(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFrontendCloseTabsUpdatesLocalStateAfterNativeClose(t *testing.T) {
	s := readFrontendUX(t)
	start := strings.Index(s, "window.closeTab = function")
	end := strings.Index(s[start:], "function isUsableFavicon")
	if start < 0 || end < 0 {
		t.Fatal("closeTab function not found")
	}
	body := s[start : start+end]
	if strings.Contains(body, "womprat_closeTab(id);\n    return;") {
		t.Fatal("closeTab must not return before updating local tab UI")
	}
	for _, want := range []string{"womprat_closeTab(id);", "saveOpenTabs();", "renderTabs();", "activateTab(state.activeTab)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("closeTab missing %q", want)
		}
	}
}

func TestFrontendCloseButtonDoesNotSuppressClick(t *testing.T) {
	s := readFrontendUX(t)
	if strings.Contains(s, "close.addEventListener('pointerup', stopCloseEvent") || strings.Contains(s, "close.addEventListener('mouseup', stopCloseEvent") {
		t.Fatal("close button should not swallow pointerup/mouseup before click")
	}
	if !strings.Contains(s, "close.addEventListener('click', closeFromEvent, true)") {
		t.Fatal("close button should close from click event")
	}
}

func TestFrontendProgressCannotSpinForever(t *testing.T) {
	s := readFrontendUX(t)
	if !strings.Contains(s, "}, 15000);") {
		t.Fatal("shell progress indicator needs a timeout fallback")
	}
}

func TestTerminalHotkeysDoNotInterceptPlainBackspace(t *testing.T) {
	s := readFrontendUX(t)
	start := strings.Index(s, "if (terminalFocused) {")
	end := strings.Index(s[start:], "if (e.altKey")
	if start < 0 || end < 0 {
		t.Fatal("terminal hotkey block not found")
	}
	body := s[start : start+end]
	if strings.Contains(strings.ToLower(body), "backspace") {
		t.Fatal("terminal hotkey block should not special-case or intercept Backspace")
	}
	if !strings.Contains(body, "return;") {
		t.Fatal("terminal hotkey block should return before browser hotkeys for plain terminal keys")
	}
}
