//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxDebugServerKeepsServing(t *testing.T) {
	b, err := os.ReadFile("gui_other.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"WOMPRAT_SHELL_URL=%s",
		"WOMPRAT_TOKEN=%s",
		"signal.Notify(ch, os.Interrupt, syscall.SIGTERM)",
		"<-ch",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("non-Windows debug server missing %q", want)
		}
	}
}
