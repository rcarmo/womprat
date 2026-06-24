package main

import "testing"

func TestWebErrorStatusMessageMapsConnectivityFailures(t *testing.T) {
	cases := []struct {
		name        string
		status      int32
		tsConnected bool
		wantSub     string
	}{
		{"dns fail disconnected points at tailscale", webErrorStatusHostNameNotResolved, false, "Tailscale is not connected"},
		{"dns fail connected stays network", webErrorStatusHostNameNotResolved, true, "resolve host name"},
		{"unreachable disconnected points at tailscale", webErrorStatusServerUnreachable, false, "Tailscale is not connected"},
		{"cannot connect disconnected points at tailscale", webErrorStatusCannotConnect, false, "Tailscale is not connected"},
		{"unreachable connected mentions exit node", webErrorStatusServerUnreachable, true, "exit node"},
		{"timeout disconnected points at tailscale", webErrorStatusTimeout, false, "Tailscale is not connected"},
		{"reset suggests reload", webErrorStatusConnectionReset, true, "reload"},
		{"cert invalid", webErrorStatusCertificateInvalid, true, "certificate"},
		{"proxy auth", webErrorStatusValidProxyAuthRequired, true, "Proxy authentication"},
		{"unknown disconnected points at tailscale", webErrorStatusUnknown, false, "Tailscale is not connected"},
		{"unknown connected generic", webErrorStatusUnknown, true, "failed to load"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := webErrorStatusMessage(tc.status, tc.tsConnected)
			if got == "" {
				t.Fatalf("empty message for status %d", tc.status)
			}
			if !containsFold(got, tc.wantSub) {
				t.Fatalf("message %q does not contain %q", got, tc.wantSub)
			}
		})
	}
}

func TestWebErrorStatusMessageAlwaysNonEmpty(t *testing.T) {
	for s := int32(0); s <= 18; s++ {
		if webErrorStatusMessage(s, true) == "" || webErrorStatusMessage(s, false) == "" {
			t.Fatalf("status %d produced empty message", s)
		}
	}
}

func containsFold(haystack, needle string) bool {
	h := []rune(toLowerASCII(haystack))
	n := []rune(toLowerASCII(needle))
	if len(n) == 0 {
		return true
	}
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLowerASCII(s string) string {
	b := []rune(s)
	for i, r := range b {
		if r >= 'A' && r <= 'Z' {
			b[i] = r + ('a' - 'A')
		}
	}
	return string(b)
}
