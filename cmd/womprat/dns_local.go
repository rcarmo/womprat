package main

import (
	"fmt"
	"net/netip"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// Preserve MagicDNS and ExtraRecords without invoking an upstream forwarder.
// Unknown names continue through Tailscale's selected resolver route, never the OS.
func localTailnetDNSAnswer(nm *netmap.NetworkMap, name, record string) ([]byte, bool, error) {
	if nm == nil {
		return nil, false, fmt.Errorf("no Tailscale network map")
	}
	clean := strings.ToLower(strings.TrimSuffix(name, "."))
	var ips []netip.Addr
	known := false
	nodes := append([]tailcfg.NodeView{nm.SelfNode}, nm.Peers...)
	for _, node := range nodes {
		if !node.Valid() {
			continue
		}
		fqdn := strings.ToLower(strings.TrimSuffix(node.Name(), "."))
		// Single-label short names are the corresponding MagicDNS first label.
		if clean != fqdn && (strings.Contains(clean, ".") || clean != strings.Split(fqdn, ".")[0]) {
			continue
		}
		known = true
		for _, p := range node.Addresses().All() {
			if p.IsSingleIP() {
				ips = append(ips, p.Addr())
			}
		}
	}
	for _, r := range nm.DNS.ExtraRecords {
		if r.Type != "" && r.Type != "A" && r.Type != "AAAA" {
			continue
		}
		if strings.EqualFold(strings.TrimSuffix(r.Name, "."), clean) {
			if ip, err := netip.ParseAddr(r.Value); err == nil {
				known = true
				ips = append(ips, ip)
			}
		}
	}
	if !known {
		return nil, false, nil
	}
	qtype := dnsmessage.TypeA
	if record == "AAAA" {
		qtype = dnsmessage.TypeAAAA
	} else if record != "A" {
		return nil, true, fmt.Errorf("unsupported query type")
	}
	fqdn, err := dnsmessage.NewName(clean + ".")
	if err != nil {
		return nil, true, err
	}
	msg := dnsmessage.Message{Header: dnsmessage.Header{Response: true, Authoritative: true}, Questions: []dnsmessage.Question{{Name: fqdn, Type: qtype, Class: dnsmessage.ClassINET}}}
	for _, ip := range ips {
		var body dnsmessage.ResourceBody
		if record == "A" && ip.Is4() {
			body = &dnsmessage.AResource{A: ip.As4()}
		} else if record == "AAAA" && ip.Is6() {
			body = &dnsmessage.AAAAResource{AAAA: ip.As16()}
		} else {
			continue
		}
		msg.Answers = append(msg.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: fqdn, Class: dnsmessage.ClassINET}, Body: body})
	}
	packet, err := msg.Pack()
	return packet, true, err
}
