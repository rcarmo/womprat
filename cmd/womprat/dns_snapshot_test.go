package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"tailscale.com/client/local"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

func TestDNSNetmapSnapshotUsesReadOnlyLocalAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/localapi/v0/debug" || r.URL.Query().Get("action") != "current-netmap" {
			t.Errorf("unexpected snapshot request: %s %s", r.Method, r.URL)
			w.WriteHeader(400)
			return
		}
		self := &tailcfg.Node{Name: "womprat.tailnet.ts.net.", Addresses: []netip.Prefix{netip.MustParsePrefix("100.64.0.1/32")}}
		json.NewEncoder(w).Encode(&netmap.NetworkMap{SelfNode: self.View(), DNS: tailcfg.DNSConfig{ExtraRecords: []tailcfg.DNSRecord{{Name: "flint.local", Type: "A", Value: "100.64.0.2"}}}})
	}))
	defer server.Close()
	lc := &local.Client{Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}, OmitAuth: true}
	nm, err := readDNSNetmap(context.Background(), lc)
	if err != nil || nm == nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if !nm.SelfNode.Valid() || nm.SelfNode.Name() != "womprat.tailnet.ts.net." || len(nm.DNS.ExtraRecords) != 1 {
		t.Fatal("DNS snapshot lost self/records")
	}
	if _, known, err := localTailnetDNSAnswer(nm, "flint.local", "A"); err != nil || !known {
		t.Fatal("snapshot cannot resolve local DNS record", err)
	}
}

func TestDNSNetmapSnapshotFailsClosedOnMissingOrInvalidState(t *testing.T) {
	for _, body := range []string{"null", "{}", "not-json"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		lc := &local.Client{Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}, OmitAuth: true}
		if _, err := readDNSNetmap(context.Background(), lc); err == nil {
			t.Error("invalid snapshot accepted", body)
		}
		server.Close()
	}
}
