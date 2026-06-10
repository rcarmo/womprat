package main

import "testing"

func TestNormalizeBrowserURL(t *testing.T) {
	for _, tt := range []struct{ raw, want string }{
		{"example.com", "http://example.com"},
		{" http://example.com/a ", "http://example.com/a"},
		{"https://example.com", "https://example.com"},
	} {
		got, err := normalizeBrowserURL(tt.raw)
		if err != nil || got != tt.want {
			t.Fatalf("normalizeBrowserURL(%q) = %q,%v want %q", tt.raw, got, err, tt.want)
		}
	}
	for _, raw := range []string{"", "rdp://me@platinum:3389", "file:///tmp/x", "http:///missing-host", "https://user@example.com"} {
		if got, err := normalizeBrowserURL(raw); err == nil {
			t.Fatalf("normalizeBrowserURL(%q) = %q, want error", raw, got)
		}
	}
}
