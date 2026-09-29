# DNS configuration and troubleshooting

Womprat uses Tailscale's DNS configuration and sends supported upstream queries through its embedded tailnet connection. It does not use the host OS resolver, send client-side mDNS broadcasts or fall back to LAN DNS.

## Custom nameservers with an exit node

Tailscale normally uses the selected exit node's DNS resolver for all domains. To use a custom nameserver while an exit node is selected:

1. Open the Tailscale admin console's **DNS** page.
2. Add or edit the nameserver. Use its Tailscale IP, or an IP reachable through an authorised subnet or exit-node route.
3. For split DNS, restrict it to the required domain.
4. Enable **Use with exit node** for that nameserver.
5. In Womprat, select the exit node and run a lookup in **Settings → Diagnostics**.

Womprat bundles Tailscale v1.94.2 and honours this policy. It does not edit nameserver settings or enable the option on your behalf. See [Tailscale's DNS documentation](https://tailscale.com/kb/1054/dns) for the distinction between global and restricted nameservers.

## `.local` names through mdnsbridge

[mdnsbridge](https://github.com/rcarmo/mdnsbridge#configure-tailscale-dns-split-dns) serves ordinary unicast DNS, normally on TCP and UDP port 53. Run it on a tailnet machine that can resolve the required LAN names, then configure:

- restricted domain: `local`;
- nameserver: the bridge machine's Tailscale IP;
- **Use with exit node**: enabled when using an exit node.

The bridge returns Tailscale CNAME/A/AAAA records for known peers and uses Avahi for other single-label `.local` names. Womprat queries the bridge over tailnet TCP; Avahi runs on the bridge host. The client does not need access to LAN multicast.

The bridge's port-53 listener and the exit node's `/dns-query` PeerAPI are different resolver endpoints, even when they run on the same machine. If diagnostics select the PeerAPI instead of the configured nameserver, inspect the matching domain and its **Use with exit node** flag before changing service settings.

## Run a lookup

In **Settings → Diagnostics**, enter a hostname and select **Look up** or press Enter. Enter a DNS name without a URL scheme, path or port. Both A and AAAA are queried. Select **Copy results** for a readable report or expand **Technical details** for JSON.

| Result field | Meaning |
| --- | --- |
| Connection and exit node | State reported by the embedded Tailscale node |
| DNS rule | Longest matching restricted domain, or global/default DNS |
| Configured nameservers | Addresses and their exit-node eligibility |
| Resolver used | Upstream chosen for this query |
| Transport | TCP DNS, exit-node HTTP DNS, or a local MagicDNS record |
| Tailnet route | Remote peer and IP prefix used to reach the upstream |
| Response and addresses | DNS response code and returned A/AAAA records |
| Time | Elapsed query time, including failure handling |

Resolver candidates are configuration data. A returned response and the query's route/transport fields provide evidence for the attempted lookup. Reading a local MagicDNS record produces no upstream traffic. Reports contain hostnames and addresses; review them before sharing.

## Interpret failures

| Result | Check |
| --- | --- |
| Name not found (`NXDOMAIN`, `RCodeNameError`) | The selected resolver does not have the name. Check the split-DNS rule and bridge records. |
| No address of this type (`RCodeSuccess` with no addresses) | The name has no record for that address family. A-only or AAAA-only hosts are valid. |
| No remote Tailscale route | Check the nameserver's tailnet address, subnet routes, exit-node state and access rules. |
| Timeout or connection error | Check that the nameserver is reachable and accepts TCP DNS. |
| Unsupported resolver transport | Use a supported IP-addressed nameserver; there is no host-DNS bootstrap fallback. |
| DNS resolves but a service fails | Check routing and access rules for the returned service IP, then its port and credentials. |

An IPv6 answer beginning with `fe80:` is link-local. It is not usable across a tailnet without the original link and interface scope. Use a reachable IPv4, global IPv6 or ULA address. With mdnsbridge, inspect `avahi-resolve -4/-6` on the bridge host to see which addresses Avahi supplies.

## Supported query paths

MagicDNS peer names and extra address records are read from a redacted Tailscale network-map snapshot. Other names use the embedded DNS manager's upstream selection. Womprat verifies a remote peer or subnet/exit-node route, then dials the embedded TCP/IP stack directly with a Tailscale source address. Route loss cannot trigger a host-socket fallback.

Supported upstream transports are TCP DNS to literal IP nameservers and HTTP DNS messages to an IP-addressed exit-node PeerAPI. HTTPS and hostname-based upstreams are rejected. The first selected candidate is queried; automatic retries across alternate nameservers are not implemented. These limits apply to application lookups and the DNS form.

## API

The local authenticated endpoint is `POST /api/settings/diagnostics/dns` with JSON such as `{"name":"host.local"}`. It validates the hostname, queries A and AAAA within a bounded request, and returns connection, policy and query results. It does not register a node, edit DNS policy or attempt a service connection. The endpoint requires Womprat's normal session authentication.

Further reading: [Tailscale DNS](https://tailscale.com/kb/1054/dns), [tsnet](https://tailscale.com/kb/1244/tsnet), and [mdnsbridge](https://github.com/rcarmo/mdnsbridge).
