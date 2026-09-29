# DNS with an exit node

Womprat resolves custom names through Tailscale's configured DNS route. When an exit node is selected, Tailscale uses the exit node's DNS proxy unless a nameserver is configured with **Use with exit node**. That rule also applies to split DNS.

Sources reviewed:

- [Tailscale DNS: Nameservers and exit nodes](https://tailscale.com/kb/1054/dns)
- [Tailscale tsnet](https://tailscale.com/kb/1244/tsnet)
- [mdnsbridge README](https://github.com/rcarmo/mdnsbridge#configure-tailscale-dns-split-dns)
- Tailscale v1.94.2 `ipn/ipnlocal/node_backend.go`: `dnsConfigForNetmap`, `useWithExitNodeRoutes`.
- Tailscale v1.94.2 `ipn/ipnlocal/local_test.go`: `TestDNSConfigForNetmapForExitNodeConfigs`.

`mdnsbridge` is a unicast DNS server on the bridge machine's Tailscale IP, normally UDP/TCP port 53. Its recommended rule is the restricted domain `local`. It returns Tailscale CNAME/A/AAAA records for known peers and uses Avahi for other single-label `.local` names. The client does not send LAN multicast.

## Observed failure

Rui's 2026-09-29 candidate output for `smith.local` selected `http://100.108.133.104:60978/dns-query`, routed to `relay.shire-bearded.ts.net.` through `100.108.133.104/32`, and received `RCodeNameError` for A and AAAA in 19 ms and 7 ms. This was an exit-node DNS response, not evidence of an answer from mdnsbridge.

Womprat's earlier Tailscale v1.82.5 dependency selected exit-node DoH before copying split-DNS routes. It could not honour `UseWithExitNode`. The dependency is now v1.94.2, which retains enabled routes and filters disabled ones as documented. It still requires the actual tailnet policy to enable the intended nameserver with an exit node. No admin configuration is changed by the app.

## Query path

MagicDNS and extra address records are read from a redacted LocalAPI network-map snapshot without network resolution. Other names use the embedded DNS manager's selected upstream. The query transport checks the WireGuard engine's remote route, then dials the embedded netstack directly with a Tailscale source address. It does not call `UserDial` (which can fall back to the host) or the upstream `QueryDNS` forwarder (which can use host sockets).

The current query implementation supports TCP DNS to literal IP nameservers and HTTP DNS messages to an IP-addressed exit-node PeerAPI. Hostname/HTTPS upstreams are rejected rather than bootstrapped with host DNS. It probes the first configured candidate, with no alternate-server retry. Those limitations are visible as errors; they must not be mistaken for NXDOMAIN or a successful lookup.

Settings DNS diagnostics show the matching configured suffix and nameserver `UseWithExitNode` flags, followed by the actual query's resolver, route, transport, response code and addresses. Policy inspection is read-only and does not disclose the complete network map.

## Validation

- The upstream exit-node DNS config regression verifies that enabled split rules survive an exit node and disabled ones do not.
- App tests cover longest-suffix and label-boundary policy reporting, local MagicDNS/A-only NODATA, synthetic TCP DNS/DoH transport, and refusal of unverified routes.
- Real `smith.local` resolution against mdnsbridge remains unverified in the build environment: it has no authenticated Womprat tsnet state. Rui must run the updated diagnostic with `relay` selected and share the report. No service port is needed for A/AAAA validation; a target service is needed for a later application connection test.
