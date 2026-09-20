package main

import "testing"

func TestSameHTTPOrigin(t *testing.T) {
	for _, tc := range []struct {
		source, target string
		same           bool
	}{
		{"https://example.com/page", "https://example.com/file", true},
		{"https://EXAMPLE.com/page", "https://example.com/file", true},
		{"https://example.com/page", "https://example.com:443/file", true},
		{"http://example.com/page", "https://example.com/file", false},
		{"https://example.com/page", "https://sub.example.com/file", false},
		{"https://example.com/page", "https://other.test/file", false},
		{"about:blank", "https://example.com/file", false},
	} {
		if got := sameHTTPOrigin(tc.source, tc.target); got != tc.same {
			t.Fatalf("sameHTTPOrigin(%q,%q)=%v", tc.source, tc.target, got)
		}
	}
}
