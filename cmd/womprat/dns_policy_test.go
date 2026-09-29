package main

import (
	"strings"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
	"testing"
)

func TestDNSPolicyReportsExitNodeEligibilityWithoutChangingIt(t *testing.T) {
	for _, allow := range []bool{false, true} {
		cfg := tailcfg.DNSConfig{Routes: map[string][]*dnstype.Resolver{"local": {{Addr: "100.70.0.53", UseWithExitNode: allow}}}}
		got := describeDNSPolicy(cfg, "Smith.LOCAL.", true)
		if got.MatchingSuffix != "local" || len(got.Resolvers) != 1 || got.Resolvers[0].UseWithExitNode != allow {
			t.Fatalf("policy %+v", got)
		}
		if strings.Contains(got.Explanation, "not enabled") == allow {
			t.Fatalf("eligibility explanation %+v", got)
		}
		if cfg.Routes["local"][0].UseWithExitNode != allow {
			t.Fatal("mutated tailnet policy")
		}
	}
}
func TestDNSPolicyUsesLongestSuffixAndLabelBoundary(t *testing.T) {
	cfg := tailcfg.DNSConfig{Routes: map[string][]*dnstype.Resolver{"local": {{Addr: "100.70.0.53"}}, "lab.local.": {{Addr: "100.70.0.54", UseWithExitNode: true}}}}
	got := describeDNSPolicy(cfg, "server.lab.local", true)
	if got.MatchingSuffix != "lab.local." || got.Resolvers[0].Address != "100.70.0.54" {
		t.Fatalf("policy %+v", got)
	}
	got = describeDNSPolicy(cfg, "notlocal", true)
	if got.MatchingSuffix != "" || !strings.Contains(got.Explanation, "exit node supplies DNS") {
		t.Fatalf("suffix match %+v", got)
	}
}
