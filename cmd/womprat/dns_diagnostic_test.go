package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

type fakeDNSQueryClient func(context.Context, string, string) ([]byte, []string, string, string, string, error)

func (f fakeDNSQueryClient) Query(ctx context.Context, name, record string) ([]byte, []string, string, string, string, error) {
	return f(ctx, name, record)
}

func TestSmithDNSDiagnosticRecordsActualResolverCandidatesAndAnswers(t *testing.T) {
	var calls []string
	fake := fakeDNSQueryClient(func(_ context.Context, name, record string) ([]byte, []string, string, string, string, error) {
		calls = append(calls, name+"/"+record)
		if name != "smith.local" || record != "A" {
			t.Fatalf("unexpected diagnostic query %s/%s", name, record)
		}
		return dnsAnswer(t, name, record, netip.MustParseAddr("100.101.102.103")), []string{"100.100.100.100"}, "100.100.100.100", "tsnet TCP DNS", "100.100.100.100 via relay (0.0.0.0/0)", nil
	})
	got := probeTailnetDNS(context.Background(), fake, "smith.local", "A")
	if !reflect.DeepEqual(calls, []string{"smith.local/A"}) || got.RCode != dnsmessage.RCodeSuccess.String() || !reflect.DeepEqual(got.Addresses, []string{"100.101.102.103"}) || !reflect.DeepEqual(got.Resolvers, []string{"100.100.100.100"}) || got.Error != "" || got.Transport != "tsnet TCP DNS" || got.PeerRoute == "" {
		t.Fatalf("diagnostic=%+v, queries=%v", got, calls)
	}
}

func TestSmithDNSDiagnosticReportsNXDOMAINAndTimeout(t *testing.T) {
	msg := dnsmessage.Message{Header: dnsmessage.Header{Response: true, RCode: dnsmessage.RCodeNameError}}
	packet, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeDNSQueryClient(func(_ context.Context, _, record string) ([]byte, []string, string, string, string, error) {
		if record == "A" {
			return packet, []string{"http://relay/dns-query"}, "http://relay/dns-query", "tsnet TCP exit-node DoH", "relay via 0.0.0.0/0", nil
		}
		return nil, []string{"100.70.0.53"}, "100.70.0.53", "tsnet TCP DNS", "relay via 0.0.0.0/0", context.DeadlineExceeded
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

func TestDNSDiagnosticValidatesHostnameAndMethod(t *testing.T) {
	app := newTestApp(t)
	for _, name := range []string{"", "127.0.0.1", "http://smith.local", "a..local", "a.local:53", "name%0a.local"} {
		req := httptest.NewRequest(http.MethodPost, "/api/settings/diagnostics/dns", strings.NewReader(`{"name":`+strconv.Quote(name)+`}`))
		w := httptest.NewRecorder()
		app.handleDNSDiagnostic(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("name %q: status=%d body=%s", name, w.Code, w.Body.String())
		}
	}
	for _, input := range []string{`{"name":"smith.local","unexpected":true}`, `{"name":"smith.local"}{"name":"other.local"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/settings/diagnostics/dns", strings.NewReader(input))
		w := httptest.NewRecorder()
		app.handleDNSDiagnostic(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("JSON %q: status=%d", input, w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/settings/diagnostics/dns", nil)
	w := httptest.NewRecorder()
	app.handleDNSDiagnostic(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status=%d", w.Code)
	}
	if got, err := normalizeDiagnosticDNSName("  Smith.Local. "); err != nil || got != "smith.local" {
		t.Fatalf("normalise=%q, %v", got, err)
	}
}

func TestDNSDiagnosticDisconnectedDoesNotEnrolOrQuery(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/settings/diagnostics/dns", strings.NewReader(`{"name":"smith.local"}`))
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
