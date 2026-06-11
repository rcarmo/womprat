package main

import (
	"os"
	"strings"
	"testing"
)

func TestRDPHandlerUsesInjectedDialerOnly(t *testing.T) {
	src, err := os.ReadFile("rdp.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "rdp.NewClientWithDialContext") {
		t.Fatal("RDP handler must use injected dialer")
	}
	if strings.Contains(text, "rdp.NewClient(") {
		t.Fatal("RDP handler must not use direct rdp.NewClient")
	}
	if !strings.Contains(text, "return dialTSNetPreferIPv4(ctx, ts, addr)") {
		t.Fatal("RDP handler must dial through tsnet")
	}
	if strings.Contains(text, "EnableMultitransport") {
		t.Fatal("RDP handler must not enable UDP/multitransport outside tsnet TCP routing")
	}
}
