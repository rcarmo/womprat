package main

import "testing"

func TestParseVNCURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		host string
		port int
	}{
		{name: "explicit scheme and port", raw: "vnc://host.example:5901", host: "host.example", port: 5901},
		{name: "default port", raw: "vnc://host.example", host: "host.example", port: 5900},
		{name: "bare hostport", raw: "host.example:5902", host: "host.example", port: 5902},
		{name: "ipv6", raw: "vnc://[fd7a:115c:a1e0::1]:5903", host: "fd7a:115c:a1e0::1", port: 5903},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVNCURL(tt.raw)
			if err != nil {
				t.Fatalf("parseVNCURL(%q) error = %v", tt.raw, err)
			}
			if got.Host != tt.host || got.Port != tt.port {
				t.Fatalf("parseVNCURL(%q) = %#v, want host=%q port=%d", tt.raw, got, tt.host, tt.port)
			}
		})
	}
}

func TestParseVNCURLRejectsInvalidTargets(t *testing.T) {
	for _, raw := range []string{"", "vnc://", "vnc://host:0", "vnc://host:70000", "vnc://bad host:5900", "vnc://host/path", "vnc://host?x=1"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseVNCURL(raw); err == nil {
				t.Fatalf("parseVNCURL(%q) succeeded, want error", raw)
			}
		})
	}
}
