//go:build windows

package main

import "testing"

func TestClampChromePx(t *testing.T) {
	for _, tt := range []struct {
		in   int32
		want int32
	}{
		{1, 24},
		{24, 24},
		{100, 100},
		{9999, 512},
	} {
		if got := clampChromePx(tt.in); got != tt.want {
			t.Fatalf("clampChromePx(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
