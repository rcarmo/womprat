package main

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

func normalizeCustomURLForTest(t customURLTarget) string {
	host := t.Host
	if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		host = "[" + host + "]"
	}
	userinfo := ""
	if t.User != "" {
		userinfo = url.User(t.User).String() + "@"
	}
	return fmt.Sprintf("%s://%s%s:%d", t.Scheme, userinfo, host, t.Port)
}

func FuzzParseCustomURL(f *testing.F) {
	for _, seed := range []string{
		"ssh://root@platinum",
		"ssh://me@platinum:2222",
		"vnc://platinum",
		"vnc://platinum:5901",
		"rdp://me@platinum",
		"rdp://me@platinum:3389",
		"rdp://me@[fd7a:115c:a1e0::2]:3389",
		"http://example.com",
		"rdp://",
		"rdp://host/path",
		"rdp://host?x=1",
		"rdp://host:0",
		"rdp://host:70000",
		"rdp://bad host:3389",
		"platinum:3389",
		"",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := parseCustomURL(raw)
		if err != nil {
			return
		}
		if got.Scheme != "ssh" && got.Scheme != "vnc" && got.Scheme != "rdp" {
			t.Fatalf("unexpected scheme: %+v", got)
		}
		if got.Host == "" || strings.ContainsAny(got.Host, " /?#\\") {
			t.Fatalf("invalid accepted host: raw=%q target=%+v", raw, got)
		}
		if got.Port <= 0 || got.Port > 65535 {
			t.Fatalf("invalid accepted port: raw=%q target=%+v", raw, got)
		}
		if len(got.User) > 256 {
			t.Fatalf("invalid accepted user length: raw=%q target=%+v", raw, got)
		}

		roundTrip, err := parseCustomURL(normalizeCustomURLForTest(got))
		if err != nil {
			t.Fatalf("normalized target did not parse: raw=%q target=%+v err=%v", raw, got, err)
		}
		if roundTrip != got {
			t.Fatalf("round-trip mismatch: raw=%q got=%+v roundTrip=%+v", raw, got, roundTrip)
		}
	})
}
