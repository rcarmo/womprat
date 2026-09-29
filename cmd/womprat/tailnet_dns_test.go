package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"tailscale.com/tsnet"
)

func dnsAnswer(t *testing.T, name, record string, addresses ...netip.Addr) []byte {
	t.Helper()
	questionType := dnsmessage.TypeA
	if record == "AAAA" {
		questionType = dnsmessage.TypeAAAA
	}
	fqdn, err := dnsmessage.NewName(name + ".")
	if err != nil {
		t.Fatal(err)
	}
	msg := dnsmessage.Message{
		Header:    dnsmessage.Header{Response: true, RecursionAvailable: true},
		Questions: []dnsmessage.Question{{Name: fqdn, Type: questionType, Class: dnsmessage.ClassINET}},
	}
	for _, ip := range addresses {
		var body dnsmessage.ResourceBody
		if ip.Is4() {
			body = &dnsmessage.AResource{A: ip.As4()}
		} else {
			body = &dnsmessage.AAAAResource{AAAA: ip.As16()}
		}
		msg.Answers = append(msg.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: fqdn, Class: dnsmessage.ClassINET}, Body: body})
	}
	packet, err := msg.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestTailnetDNSParsesAddressesAndRejectsBadAnswers(t *testing.T) {
	packet := dnsAnswer(t, "printer.corp.example", "A", netip.MustParseAddr("10.21.0.5"), netip.MustParseAddr("10.21.0.6"))
	ips, err := tailnetDNSAddresses(packet, "A")
	if err != nil || len(ips) != 2 || ips[0].String() != "10.21.0.5" || ips[1].String() != "10.21.0.6" {
		t.Fatalf("A answers = %v, %v", ips, err)
	}
	ips, err = tailnetDNSAddresses(dnsAnswer(t, "printer.corp.example", "AAAA", netip.MustParseAddr("fd7a:115c:a1e0::5")), "AAAA")
	if err != nil || len(ips) != 1 || ips[0].String() != "fd7a:115c:a1e0::5" {
		t.Fatalf("AAAA answers = %v, %v", ips, err)
	}
	if _, err := tailnetDNSAddresses([]byte{0x00}, "A"); err == nil {
		t.Fatal("accepted malformed DNS packet")
	}
}

func TestTailnetSplitDNSResolutionDialsResolvedIP(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tailnet-dns-connection-ok")
	}))
	defer origin.Close()
	originHost, port, _ := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))
	var queries, dials []string
	query := func(ctx context.Context, name, record string) ([]byte, error) {
		queries = append(queries, name+"/"+record)
		if name != "service.split.example" {
			return nil, errors.New("unexpected DNS name")
		}
		if record == "A" {
			return dnsAnswer(t, name, record, netip.MustParseAddr(originHost)), nil
		}
		return dnsAnswer(t, name, record), nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dials = append(dials, network+"/"+address)
		var d net.Dialer
		return d.DialContext(ctx, network, address)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := dialTailnetResolved(ctx, net.JoinHostPort("service.split.example", port), query, dial)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.0\r\nHost: service.split.example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(bufio.NewReader(conn))
	if err != nil || !strings.Contains(string(response), "tailnet-dns-connection-ok") {
		t.Fatalf("application connection = %q, %v", response, err)
	}
	if fmt.Sprint(queries) != "[service.split.example/A]" || fmt.Sprint(dials) != "[tcp4/"+net.JoinHostPort(originHost, port)+"]" {
		t.Fatalf("DNS queries = %v, application dials = %v", queries, dials)
	}
}

func TestTailnetDNSFallbackAndFailClosed(t *testing.T) {
	var calls []string
	query := func(ctx context.Context, name, record string) ([]byte, error) {
		calls = append(calls, record)
		if record == "A" {
			return dnsAnswer(t, name, record, netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2")), nil
		}
		return dnsAnswer(t, name, record, netip.MustParseAddr("fd7a:115c:a1e0::2")), nil
	}
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		calls = append(calls, network+"/"+addr)
		return nil, errors.New("unreachable")
	}
	_, err := dialTailnetResolved(context.Background(), "service.split.example:443", query, dial)
	if err == nil || fmt.Sprint(calls) != "[A tcp4/10.0.0.1:443 tcp4/10.0.0.2:443 AAAA tcp6/[fd7a:115c:a1e0::2]:443]" {
		t.Fatalf("fallback calls = %v, error = %v", calls, err)
	}
	calls = nil
	_, err = dialTailnetResolved(context.Background(), "missing.split.example:443", func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("DNS unavailable")
	}, dial)
	if err == nil || len(calls) != 0 {
		t.Fatalf("DNS failure must not dial through host resolver: calls = %v, error = %v", calls, err)
	}
	calls = nil
	_, err = dialTailnetResolved(context.Background(), "100.100.100.100:443", func(context.Context, string, string) ([]byte, error) {
		t.Fatal("IP literal unexpectedly queried DNS")
		return nil, nil
	}, dial)
	if err == nil || len(calls) != 3 {
		t.Fatalf("IP literal calls = %v, error = %v", calls, err)
	}
}

func TestSOCKSDomainConnectUsesResolvedTailnetAddress(t *testing.T) {
	// Allow the SOCKS handler to run without a live tsnet server; the injected
	// dialer still exercises tailnet DNS and real TCP routing through SOCKS.
	withDirectDial(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "socks-tailnet-dns-ok")
	}))
	defer origin.Close()
	originHost, port, _ := net.SplitHostPort(strings.TrimPrefix(origin.URL, "http://"))
	p, _ := strconv.Atoi(port)
	app := newTestApp(t)
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleSOCKS5WithDial(server, app, func(ctx context.Context, _ *tsnet.Server, addr string) (net.Conn, error) {
			return dialTailnetResolved(ctx, addr, func(ctx context.Context, name, record string) ([]byte, error) {
				if name != "origin.split.example" || record != "A" {
					return nil, fmt.Errorf("unexpected DNS request %s/%s", name, record)
				}
				return dnsAnswer(t, name, record, netip.MustParseAddr(originHost)), nil
			}, func(ctx context.Context, network, address string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, address)
			})
		})
		close(done)
	}()
	defer func() { client.Close(); <-done }()
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = client.Write([]byte{5, 1, 0})
	greet := make([]byte, 2)
	if _, err := io.ReadFull(client, greet); err != nil || greet[1] != 0 {
		t.Fatalf("SOCKS greeting = %v, %v", greet, err)
	}
	name := "origin.split.example"
	req := append([]byte{5, 1, 0, 3, byte(len(name))}, name...)
	req = append(req, byte(p>>8), byte(p))
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil || reply[1] != socksReplySucceeded {
		t.Fatalf("SOCKS reply = %v, %v", reply, err)
	}
	_, _ = io.WriteString(client, "GET / HTTP/1.0\r\nHost: origin.split.example\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "socks-tailnet-dns-ok" {
		t.Fatalf("SOCKS application body = %q, %v", body, err)
	}
}
