package main

import "testing"

func TestFrontendAssetPath(t *testing.T) {
	tests := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "", want: "frontend/index.html", ok: true},
		{raw: "/", want: "frontend/index.html", ok: true},
		{raw: "/index.html", want: "frontend/index.html", ok: true},
		{raw: "/rle/rle.wasm", want: "frontend/rle/rle.wasm", ok: true},
		{raw: "index.html", ok: false},
		{raw: "/api/about", ok: false},
		{raw: "/../config.enc", ok: false},
		{raw: "/foo/../../bar", ok: false},
		{raw: "/foo\\bar", ok: false},
		{raw: "/bad\x00path", ok: false},
	}
	for _, tt := range tests {
		got, ok := frontendAssetPath(tt.raw)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("frontendAssetPath(%q) = %q,%v want %q,%v", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}
