package main

import (
	"os"
	"strings"
	"testing"

	"github.com/rcarmo/go-rdp/pkg/protocol/pdu"
)

func TestParseRDPURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		host string
		port int
		user string
	}{
		{name: "explicit user and port", raw: "rdp://alice@host.example:3390", host: "host.example", port: 3390, user: "alice"},
		{name: "user colon placeholder", raw: "rdp://alice:@host.example:3390", host: "host.example", port: 3390, user: "alice"},
		{name: "default port", raw: "rdp://host.example", host: "host.example", port: 3389},
		{name: "user and default port", raw: "rdp://me@platinum", host: "platinum", port: 3389, user: "me"},
		{name: "uppercase scheme", raw: "RDP://me@platinum", host: "platinum", port: 3389, user: "me"},
		{name: "bare hostport", raw: "host.example:3391", host: "host.example", port: 3391},
		{name: "ipv6", raw: "rdp://bob@[fd7a:115c:a1e0::2]:3389", host: "fd7a:115c:a1e0::2", port: 3389, user: "bob"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRDPURL(tt.raw)
			if err != nil {
				t.Fatalf("parseRDPURL(%q) error = %v", tt.raw, err)
			}
			if got.Host != tt.host || got.Port != tt.port || got.User != tt.user {
				t.Fatalf("parseRDPURL(%q) = %#v, want host=%q port=%d user=%q", tt.raw, got, tt.host, tt.port, tt.user)
			}
		})
	}
}

func TestParseRDPURLRejectsInvalidTargets(t *testing.T) {
	for _, raw := range []string{"", "rdp://", "rdp://host:0", "rdp://host:70000", "rdp://bad host:3389", "rdp://host/path", "rdp://host?x=1"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseRDPURL(raw); err == nil {
				t.Fatalf("parseRDPURL(%q) succeeded, want error", raw)
			}
		})
	}
}

func TestViewerWebSocketReadLimits(t *testing.T) {
	if maxRDPWebSocketMessageBytes <= 0 || maxRDPWebSocketMessageBytes > 2<<20 {
		t.Fatalf("unexpected RDP websocket read limit: %d", maxRDPWebSocketMessageBytes)
	}
	if maxVNCWebSocketMessageBytes <= 0 || maxVNCWebSocketMessageBytes > 2<<20 {
		t.Fatalf("unexpected VNC websocket read limit: %d", maxVNCWebSocketMessageBytes)
	}
}

func TestRDPCredentialHostMatchesTarget(t *testing.T) {
	target := rdpTarget{Host: "platinum", Port: 3389, User: "me"}
	for _, raw := range []string{"", "platinum", "platinum:3389", "rdp://platinum", "rdp://me@platinum:3389"} {
		if !rdpCredentialHostMatches(raw, target) {
			t.Fatalf("rdpCredentialHostMatches(%q) = false", raw)
		}
	}
	for _, raw := range []string{"other", "platinum:3390", "rdp://platinum/path", "bad host"} {
		if rdpCredentialHostMatches(raw, target) {
			t.Fatalf("rdpCredentialHostMatches(%q) = true", raw)
		}
	}
}

func TestParseRDPColorDepth(t *testing.T) {
	for _, raw := range []string{"8", "15", "16", "24", "32"} {
		depth := map[string]int{"8": 8, "15": 15, "16": 16, "24": 24, "32": 32}[raw]
		if got := parseRDPColorDepth(raw, 16); got != depth {
			t.Fatalf("parseRDPColorDepth(%s) = %d", raw, got)
		}
	}
	for _, raw := range []string{"", "12", "33", "abc"} {
		if got := parseRDPColorDepth(raw, 16); got != 16 {
			t.Fatalf("parseRDPColorDepth(%q) = %d, want fallback", raw, got)
		}
	}
}

func TestRDPFrontendEmbedsWASMCodecs(t *testing.T) {
	for _, path := range []string{"frontend/rle/rle.wasm", "frontend/rle/wasm_exec.js"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("missing embedded RDP codec asset %s: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("embedded RDP codec asset %s is empty", path)
		}
	}
	js, err := os.ReadFile("frontend/rdp.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/rle/wasm_exec.js", "/rle/rle.wasm", "all RDP decode paths use WASM", "WASM bitmap decode failed", "WASM surface decode failed", "rfx"} {
		if !strings.Contains(string(js), want) {
			t.Fatalf("rdp.js missing %q", want)
		}
	}
}

func TestRDPDefaultsEnableWASMBackedRFX(t *testing.T) {
	if got := rdpRFXEnabled(""); !got {
		t.Fatal("RFX should default to enabled")
	}
	if got := rdpRFXEnabled("false"); got {
		t.Fatal("explicit rfx=false should disable RFX")
	}
}

func TestNewRDPTabAcceptsUserAtHostWithoutPort(t *testing.T) {
	app := newTestApp(t)
	app.newRDPTab("rdp://me@platinum")
	if len(app.tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(app.tabs))
	}
	tab := app.tabs[0]
	if tab.Type != "rdp" || tab.Host != "platinum" || tab.User != "me" || tab.Port != 3389 || tab.URL != "rdp://me@platinum:3389" {
		t.Fatalf("rdp tab = %+v", tab)
	}
	if app.activeTab != tab.ID {
		t.Fatalf("activeTab = %q, want %q", app.activeTab, tab.ID)
	}
}

func TestRDPAdvertisesOnlyWASMBackedBitmapCodecs(t *testing.T) {
	set := pdu.NewBitmapCodecsWithRFXCapabilitySet()
	if set.BitmapCodecsCapabilitySet == nil {
		t.Fatal("missing bitmap codecs capability set")
	}
	codecs := set.BitmapCodecsCapabilitySet.BitmapCodecArray
	if len(codecs) != 2 {
		t.Fatalf("advertised codec count = %d, want NSCodec + RemoteFX-Image only", len(codecs))
	}
	if codecs[0].CodecGUID != pdu.NSCodecGUID {
		t.Fatalf("first advertised codec = %#v, want NSCodec", codecs[0].CodecGUID)
	}
	if codecs[1].CodecGUID != pdu.RemoteFXImageGUID {
		t.Fatalf("second advertised codec = %#v, want RemoteFX-Image", codecs[1].CodecGUID)
	}
}
