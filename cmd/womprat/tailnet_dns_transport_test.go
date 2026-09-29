package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"tailscale.com/types/dnstype"
)

func TestSmithDNSOverTailnetTCPAndNoHostFallback(t *testing.T) {
	const resolverIP = "100.70.0.53"
	const answerIP = "100.101.102.103"
	var routeCalls, dialCalls []string
	route := func(ip netip.Addr) (string, error) {
		routeCalls = append(routeCalls, ip.String())
		if ip.String() != resolverIP {
			return "", errors.New("not a Tailscale route")
		}
		return resolverIP + " via relay (0.0.0.0/0)", nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCalls = append(dialCalls, network+"/"+address)
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			var size uint16
			if binary.Read(server, binary.BigEndian, &size) != nil {
				return
			}
			question := make([]byte, size)
			if _, err := io.ReadFull(server, question); err != nil {
				return
			}
			var parsed dnsmessage.Message
			if parsed.Unpack(question) != nil || len(parsed.Questions) != 1 || parsed.Questions[0].Name.String() != "smith.local." {
				return
			}
			packet := dnsAnswer(t, "smith.local", "A", netip.MustParseAddr(answerIP))
			binary.BigEndian.PutUint16(packet[:2], parsed.Header.ID)
			_ = binary.Write(server, binary.BigEndian, uint16(len(packet)))
			_, _ = server.Write(packet)
		}()
		return client, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	packet, server, transport, peerRoute, err := queryDNSOverTailnet(ctx, "smith.local", "A", []*dnstype.Resolver{{Addr: resolverIP}}, route, dial)
	if err != nil {
		t.Fatal(err)
	}
	ips, err := tailnetDNSAddresses(packet, "A")
	if err != nil || len(ips) != 1 || ips[0].String() != answerIP {
		t.Fatalf("DNS response = %v, %v", ips, err)
	}
	if server != resolverIP || transport != "tsnet TCP DNS" || !strings.Contains(peerRoute, "relay") || len(routeCalls) != 1 || routeCalls[0] != resolverIP || len(dialCalls) != 1 || dialCalls[0] != "tcp/"+resolverIP+":53" {
		t.Fatalf("transport: server=%q type=%q route=%q routes=%v dials=%v", server, transport, peerRoute, routeCalls, dialCalls)
	}
	dialCalls = nil
	_, _, _, _, err = queryDNSOverTailnet(ctx, "smith.local", "A", []*dnstype.Resolver{{Addr: "192.168.1.53"}}, func(netip.Addr) (string, error) { return "", errors.New("no tailnet route") }, dial)
	if err == nil || len(dialCalls) != 0 {
		t.Fatalf("host/LAN fallback attempted: %v, %v", dialCalls, err)
	}
}

func TestSmithDNSOverTailnetExitNodeDoH(t *testing.T) {
	const ip = "100.70.0.2"
	var dialCalls []string
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialCalls = append(dialCalls, network+"/"+address)
		server, client := net.Pipe()
		go func() {
			defer server.Close()
			request, err := http.ReadRequest(bufio.NewReader(server))
			if err != nil {
				return
			}
			defer request.Body.Close()
			question, err := io.ReadAll(request.Body)
			if err != nil || request.URL.Path != "/dns-query" || request.Header.Get("Content-Type") != "application/dns-message" {
				return
			}
			var parsed dnsmessage.Message
			if parsed.Unpack(question) != nil {
				return
			}
			packet := dnsAnswer(t, "smith.local", "A", netip.MustParseAddr("100.101.102.103"))
			binary.BigEndian.PutUint16(packet[:2], parsed.Header.ID)
			_, _ = server.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/dns-message\r\nContent-Length: " + strconv.Itoa(len(packet)) + "\r\n\r\n"))
			_, _ = server.Write(packet)
		}()
		return client, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	packet, server, transport, route, err := queryDNSOverTailnet(ctx, "smith.local", "A", []*dnstype.Resolver{{Addr: "http://" + ip + ":8080/dns-query"}}, func(ip netip.Addr) (string, error) {
		return ip.String() + " via relay", nil
	}, dial)
	if err != nil {
		t.Fatal(err)
	}
	ips, err := tailnetDNSAddresses(packet, "A")
	if err != nil || len(ips) != 1 || ips[0].String() != "100.101.102.103" || server != "http://"+ip+":8080/dns-query" || transport != "tsnet TCP exit-node DoH" || !strings.Contains(route, "relay") || len(dialCalls) != 1 || dialCalls[0] != "tcp/"+ip+":8080" {
		t.Fatalf("DoH: ips=%v err=%v server=%q transport=%q route=%q dials=%v", ips, err, server, transport, route, dialCalls)
	}
}

func TestTailnetOnlyDNSRejectsUnsupportedAndUnroutedResolvers(t *testing.T) {
	called := false
	dial := func(context.Context, string, string) (net.Conn, error) {
		called = true
		return nil, errors.New("should not dial")
	}
	route := func(netip.Addr) (string, error) { called = true; return "", errors.New("no route") }
	for _, resolver := range []string{"http://resolver.example/dns-query", "https://1.1.1.1/dns-query", "bad-resolver"} {
		_, _, _, _, err := queryDNSOverTailnet(context.Background(), "smith.local", "A", []*dnstype.Resolver{{Addr: resolver}}, route, dial)
		if err == nil || called {
			t.Fatalf("resolver %q bypassed fail-closed rule: err=%v called=%t", resolver, err, called)
		}
	}
}
