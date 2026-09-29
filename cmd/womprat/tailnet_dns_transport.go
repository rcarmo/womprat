package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
	"tailscale.com/types/dnstype"
)

// tailnetDNSRoute must reject any destination that the embedded engine cannot
// associate with a WireGuard peer or subnet/exit-node route. No host dialer is
// called by this query path.
type tailnetDNSRoute func(netip.Addr) (string, error)

func queryDNSOverTailnet(ctx context.Context, name, record string, resolvers []*dnstype.Resolver, route tailnetDNSRoute, dial tailnetDial) (packet []byte, server, transport, peerRoute string, err error) {
	fqdn, e := dnsmessage.NewName(strings.TrimSuffix(name, ".") + ".")
	if e != nil {
		err = e
		return
	}
	typ := dnsmessage.TypeA
	if record == "AAAA" {
		typ = dnsmessage.TypeAAAA
	} else if record != "A" {
		err = fmt.Errorf("unsupported DNS type %q", record)
		return
	}
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 0x3210, RecursionDesired: true})
	builder.StartQuestions()
	if e = builder.Question(dnsmessage.Question{Name: fqdn, Type: typ, Class: dnsmessage.ClassINET}); e != nil {
		err = e
		return
	}
	question, e := builder.Finish()
	if e != nil {
		err = e
		return
	}
	if len(resolvers) == 0 {
		err = fmt.Errorf("no upstream resolver selected for %s by embedded Tailscale DNS", name)
		return
	}
	// Do not race/fall back to other resolvers: report the chosen resolver's
	// exact error so a route mismatch cannot be masked by host-network DNS.
	resolver := resolvers[0]
	if resolver == nil {
		err = fmt.Errorf("nil selected DNS resolver")
		return
	}
	server = resolver.Addr
	var target netip.AddrPort
	var dohURL *url.URL
	if ipp, ok := resolver.IPPort(); ok {
		target = ipp
		transport = "tsnet TCP DNS"
	} else if u, parseErr := url.Parse(server); parseErr == nil && u.Scheme == "http" && u.User == nil && u.Hostname() != "" {
		// Tailscale's exit-node PeerAPI DoH uses http://<tailnet-IP>:port.
		// Hostnames/HTTPS are intentionally unsupported: no bootstrap OS DNS.
		ip, ipErr := netip.ParseAddr(u.Hostname())
		if ipErr != nil {
			err = fmt.Errorf("DoH resolver has non-IP host; refusing host DNS: %w", ipErr)
			return
		}
		port := u.Port()
		if port == "" {
			port = "80"
		}
		portNum, portErr := strconv.ParseUint(port, 10, 16)
		if portErr != nil || portNum == 0 {
			err = fmt.Errorf("invalid DoH port %q", port)
			return
		}
		target = netip.AddrPortFrom(ip, uint16(portNum))
		dohURL = u
		transport = "tsnet TCP exit-node DoH"
	} else {
		err = fmt.Errorf("resolver %q has no supported tailnet-only transport", server)
		return
	}
	peerRoute, err = route(target.Addr())
	if err != nil {
		err = fmt.Errorf("refusing DNS over host network for %s: %w", target, err)
		return
	}
	if dohURL != nil {
		tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dial(ctx, "tcp", target.String())
		}}
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		request, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, dohURL.String(), bytes.NewReader(question))
		if reqErr != nil {
			err = reqErr
			return
		}
		request.Header.Set("Content-Type", "application/dns-message")
		request.Header.Set("Accept", "application/dns-message")
		response, reqErr := client.Do(request)
		if reqErr != nil {
			err = reqErr
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err = fmt.Errorf("DoH HTTP %d", response.StatusCode)
			return
		}
		if !strings.HasPrefix(response.Header.Get("Content-Type"), "application/dns-message") {
			err = fmt.Errorf("DoH returned non-DNS content type %q", response.Header.Get("Content-Type"))
			return
		}
		packet, err = io.ReadAll(io.LimitReader(response.Body, 65536))
	} else {
		conn, dialErr := dial(ctx, "tcp", target.String())
		if dialErr != nil {
			err = dialErr
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		if deadline, ok := ctx.Deadline(); ok {
			_ = conn.SetDeadline(deadline)
		}
		if e = binary.Write(conn, binary.BigEndian, uint16(len(question))); e != nil {
			err = e
			return
		}
		if _, e = io.Copy(conn, bytes.NewReader(question)); e != nil {
			err = e
			return
		}
		var size uint16
		if e = binary.Read(conn, binary.BigEndian, &size); e != nil {
			err = e
			return
		}
		packet = make([]byte, size)
		_, err = io.ReadFull(conn, packet)
	}
	if err != nil {
		return
	}
	var response dnsmessage.Message
	if e = response.Unpack(packet); e != nil {
		err = e
		return
	}
	if !response.Header.Response || response.Header.ID != 0x3210 || len(response.Questions) != 1 || response.Questions[0].Name != fqdn || response.Questions[0].Type != typ {
		err = fmt.Errorf("DNS response does not match query")
	}
	return
}
