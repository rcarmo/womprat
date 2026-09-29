package main

import (
	"net/netip"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
	"testing"
)

func TestTailnetLocalRecordsStayLocalAndCustomLocalNamesGoUpstream(t *testing.T) {
	node := (&tailcfg.Node{Name: "smith.example.ts.net.", Addresses: []netip.Prefix{netip.MustParsePrefix("100.70.0.2/32"), netip.MustParsePrefix("fd7a:115c:a1e0::2/128")}}).View()
	nm := &netmap.NetworkMap{Peers: []tailcfg.NodeView{node}, DNS: tailcfg.DNSConfig{ExtraRecords: []tailcfg.DNSRecord{{Name: "alias.example.test", Value: "100.70.0.3"}}}}
	for _, tc := range []struct{ name, kind, want string }{{"smith.example.ts.net", "A", "100.70.0.2"}, {"smith", "AAAA", "fd7a:115c:a1e0::2"}, {"ALIAS.example.test.", "A", "100.70.0.3"}} {
		packet, known, err := localTailnetDNSAnswer(nm, tc.name, tc.kind)
		if err != nil || !known {
			t.Fatalf("%+v: known=%t err=%v", tc, known, err)
		}
		ips, err := tailnetDNSAddresses(packet, tc.kind)
		if err != nil || len(ips) != 1 || ips[0].String() != tc.want {
			t.Fatalf("%+v: %v %v", tc, ips, err)
		}
	}
	_, known, err := localTailnetDNSAnswer(nm, "smith.local", "A")
	if known || err != nil {
		t.Fatalf("smith.local must use configured split route, not fabricated peer alias: known=%t %v", known, err)
	}
	packet, known, err := localTailnetDNSAnswer(nm, "alias.example.test", "AAAA")
	ips, parseErr := tailnetDNSAddresses(packet, "AAAA")
	if !known || err != nil || parseErr != nil || len(ips) != 0 {
		t.Fatalf("known A-only record must produce AAAA NODATA: %t %v %v %v", known, err, parseErr, ips)
	}
}
