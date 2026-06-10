package main

import "testing"

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

func TestParseRDPColorDepth(t *testing.T) {
	for _, depth := range []int{8, 15, 16, 24, 32} {
		if got := parseRDPColorDepth(string(rune('0'+depth/10))+string(rune('0'+depth%10)), 16); got != depth {
			t.Fatalf("parseRDPColorDepth(%d) = %d", depth, got)
		}
	}
	for _, raw := range []string{"", "12", "33", "abc"} {
		if got := parseRDPColorDepth(raw, 16); got != 16 {
			t.Fatalf("parseRDPColorDepth(%q) = %d, want fallback", raw, got)
		}
	}
}
