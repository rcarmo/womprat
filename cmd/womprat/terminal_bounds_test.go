package main

import "testing"

func TestTerminalWebSocketReadLimit(t *testing.T) {
	if maxTerminalWebSocketMessageSize <= 0 || maxTerminalWebSocketMessageSize > 2<<20 {
		t.Fatalf("unexpected terminal websocket read limit: %d", maxTerminalWebSocketMessageSize)
	}
}

func TestParseTerminalDimension(t *testing.T) {
	tests := []struct {
		raw      string
		fallback int
		min      int
		max      int
		want     int
	}{
		{raw: "", fallback: 80, min: 20, max: 500, want: 80},
		{raw: "abc", fallback: 80, min: 20, max: 500, want: 80},
		{raw: "0", fallback: 80, min: 20, max: 500, want: 80},
		{raw: "1", fallback: 80, min: 20, max: 500, want: 20},
		{raw: "120", fallback: 80, min: 20, max: 500, want: 120},
		{raw: "9999", fallback: 80, min: 20, max: 500, want: 500},
	}
	for _, tt := range tests {
		if got := parseTerminalDimension(tt.raw, tt.fallback, tt.min, tt.max); got != tt.want {
			t.Fatalf("parseTerminalDimension(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestClampTerminalDimension(t *testing.T) {
	for _, tt := range []struct{ value, min, max, want int }{
		{0, 20, 500, 0},
		{-1, 20, 500, 0},
		{10, 20, 500, 20},
		{80, 20, 500, 80},
		{501, 20, 500, 500},
		{80, 0, 500, 0},
		{80, 500, 20, 0},
	} {
		if got := clampTerminalDimension(tt.value, tt.min, tt.max); got != tt.want {
			t.Fatalf("clampTerminalDimension(%d,%d,%d) = %d, want %d", tt.value, tt.min, tt.max, got, tt.want)
		}
	}
}
