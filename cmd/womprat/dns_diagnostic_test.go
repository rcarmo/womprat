package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
	"tailscale.com/types/dnstype"
)

type fakeDNSQueryClient func(context.Context, string, string) ([]byte, []*dnstype.Resolver, error)

func (f fakeDNSQueryClient) QueryDNS(ctx context.Context, name, record string) ([]byte, []*dnstype.Resolver, error) {
	return f(ctx, name, record)
}

func TestSmithDNSDiagnosticRecordsActualResolverCandidatesAndAnswers(t *testing.T) {
	var calls []string
	fake := fakeDNSQueryClient(func(_ context.Context, name, record string) ([]byte, []*dnstype.Resolver, error) {
		calls = append(calls, name+"/"+record)
		if name != "smith.local" || record != "A" {
			t.Fatalf("unexpected diagnostic query %s/%s", name, record)
		}
		return dnsAnswer(t, name, record, netip.MustParseAddr("100.101.102.103")), []*dnstype.Resolver{{Addr: "100.100.100.100"}}, nil
	})
	got := probeTailnetDNS(context.Background(), fake, "smith.local", "A")
	if !reflect.DeepEqual(calls, []string{"smith.local/A"}) || got.RCode != dnsmessage.RCodeSuccess.String() || !reflect.DeepEqual(got.Addresses, []string{"100.101.102.103"}) || !reflect.DeepEqual(got.Resolvers, []string{"100.100.100.100"}) || got.Error != "" {
		t.Fatalf("diagnostic=%+v, queries=%v", got, calls)
	}
}

func TestSmithDNSDiagnosticReportsNXDOMAINAndTimeout(t *testing.T) {
	msg := dnsmessage.Message{Header: dnsmessage.Header{Response: true, RCode: dnsmessage.RCodeNameError}}
	packet, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeDNSQueryClient(func(_ context.Context, _, record string) ([]byte, []*dnstype.Resolver, error) {
		if record == "A" {
			return packet, []*dnstype.Resolver{{Addr: "http://relay/dns-query"}}, nil
		}
		return nil, []*dnstype.Resolver{{Addr: "100.70.0.53"}}, context.DeadlineExceeded
	})
	a := probeTailnetDNS(context.Background(), fake, "smith.local", "A")
	if a.RCode != dnsmessage.RCodeNameError.String() || len(a.Addresses) != 0 || len(a.Resolvers) != 1 || a.Resolvers[0] != "http://relay/dns-query" {
		t.Fatalf("NXDOMAIN response: %+v", a)
	}
	v6 := probeTailnetDNS(context.Background(), fake, "smith.local", "AAAA")
	if !strings.Contains(v6.Error, "deadline exceeded") {
		t.Fatalf("timeout response: %+v", v6)
	}
	if len(v6.Resolvers) != 1 || v6.Resolvers[0] != "100.70.0.53" {
		t.Fatalf("resolver candidates lost: %+v", v6)
	}
}

func TestSmithDNSDiagnosticDisconnectedDoesNotEnrolOrQuery(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/diagnostics/dns", nil)
	w := httptest.NewRecorder()
	app.handleDNSDiagnostic(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", w.Code, w.Body.String())
	}
	var body dnsDiagnosticResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "smith.local" || !strings.Contains(body.Error, "not connected") || len(body.Queries) != 0 {
		t.Fatalf("disconnected diagnostic=%+v", body)
	}
}
