package main

import (
	"errors"
	"testing"
)

func TestSocksReplyForDialError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want byte
	}{
		{"nil is success", nil, socksReplySucceeded},
		{"dns no such host -> host unreachable", errors.New("dial tcp: lookup example.com: no such host"), socksReplyHostUnreachable},
		{"server misbehaving -> host unreachable", errors.New("lookup x: server misbehaving"), socksReplyHostUnreachable},
		{"refused -> connection refused", errors.New("dial tcp 1.2.3.4:80: connect: connection refused"), socksReplyConnectionRefused},
		{"network unreachable -> network unreach", errors.New("connect: network is unreachable"), socksReplyNetworkUnreach},
		{"no route -> network unreach", errors.New("connect: no route to host"), socksReplyNetworkUnreach},
		{"timeout -> host unreachable", errors.New("dial tcp: i/o timeout"), socksReplyHostUnreachable},
		{"deadline -> host unreachable", errors.New("context deadline exceeded"), socksReplyHostUnreachable},
		{"unknown -> general failure", errors.New("weird unexpected failure"), socksReplyGeneralFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := socksReplyForDialError(tc.err); got != tc.want {
				t.Fatalf("socksReplyForDialError(%v) = 0x%02x, want 0x%02x", tc.err, got, tc.want)
			}
		})
	}
}

func TestSocksReplyConstantsAreDistinct(t *testing.T) {
	codes := map[byte]string{
		socksReplySucceeded:         "succeeded",
		socksReplyGeneralFailure:    "general",
		socksReplyNetworkUnreach:    "network",
		socksReplyHostUnreachable:   "host",
		socksReplyConnectionRefused: "refused",
		socksReplyCommandNotSupp:    "command",
		socksReplyAddrTypeNotSupp:   "addr",
	}
	if len(codes) != 7 {
		t.Fatalf("SOCKS reply codes collide: %v", codes)
	}
}
