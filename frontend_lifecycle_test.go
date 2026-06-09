package main

import (
	"os"
	"strings"
	"testing"
)

func TestFrontendTerminalLifecyclePreservesSessionOnSwitch(t *testing.T) {
	data, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	existing := "const existingSession = terminalSessions.get(tabId);"
	create := "const term = new Terminal"
	if !strings.Contains(s, existing) {
		t.Fatalf("frontend missing existing terminal session guard")
	}
	if strings.Index(s, existing) < 0 || strings.Index(s, create) < 0 || strings.Index(s, existing) > strings.Index(s, create) {
		t.Fatalf("existing session guard must run before creating a new terminal")
	}
	for _, want := range []string{"existingSession.term?.focus?.();", "return;", "terminalSession.ws?.close?.();", "terminalSession.term?.dispose?.();", "terminalSessions.delete(id);"} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend missing terminal lifecycle fragment %q", want)
		}
	}
}

func TestFrontendTabSwitchNotifiesNativeWithoutDestroyingTerminal(t *testing.T) {
	data, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"function activateTab(id, options = {})", "if (!options.skipNative && window.womprat_switchTab)", "terminalSessions.get(id)?.term?.focus?.();"} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend missing tab switch lifecycle fragment %q", want)
		}
	}
	activationStart := strings.Index(s, "function activateTab(id, options = {})")
	activationEnd := strings.Index(s[activationStart:], "window.showBrowserTab")
	if activationStart < 0 || activationEnd < 0 {
		t.Fatal("could not locate activateTab body")
	}
	body := s[activationStart : activationStart+activationEnd]
	if strings.Contains(body, "terminalSessions.delete") || strings.Contains(body, ".dispose") || strings.Contains(body, ".close") {
		t.Fatal("activateTab must not destroy terminal sessions while switching tabs")
	}
}
