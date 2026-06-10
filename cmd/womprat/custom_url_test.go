package main

import "testing"

func TestParseCustomURLForSupportedSchemes(t *testing.T) {
	tests := []struct {
		raw    string
		scheme string
		host   string
		port   int
		user   string
	}{
		{raw: "ssh://me@platinum", scheme: "ssh", host: "platinum", port: 22, user: "me"},
		{raw: "vnc://platinum", scheme: "vnc", host: "platinum", port: 5900},
		{raw: "rdp://me@platinum", scheme: "rdp", host: "platinum", port: 3389, user: "me"},
		{raw: "rdp://me@platinum:3390", scheme: "rdp", host: "platinum", port: 3390, user: "me"},
		{raw: "rdp://me@[fd7a:115c:a1e0::2]:3389", scheme: "rdp", host: "fd7a:115c:a1e0::2", port: 3389, user: "me"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := parseCustomURL(tt.raw)
			if err != nil {
				t.Fatalf("parseCustomURL(%q) error = %v", tt.raw, err)
			}
			if got.Scheme != tt.scheme || got.Host != tt.host || got.Port != tt.port || got.User != tt.user {
				t.Fatalf("parseCustomURL(%q) = %+v, want scheme=%q host=%q port=%d user=%q", tt.raw, got, tt.scheme, tt.host, tt.port, tt.user)
			}
		})
	}
}

func TestParseCustomURLRejectsUnsupportedOrMalformedURLs(t *testing.T) {
	for _, raw := range []string{"", "platinum:3389", "http://platinum", "rdp://", "rdp://host/path", "rdp://host?x=1", "rdp://host:0", "rdp://host:70000"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseCustomURL(raw); err == nil {
				t.Fatalf("parseCustomURL(%q) succeeded, want error", raw)
			}
		})
	}
}
