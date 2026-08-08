# Linux networking and policy routing

MultiSpeed measures a configured path; it does not create one. On a multi-WAN host, Linux source-based policy routing must already direct traffic from each selected address to the correct gateway. MultiSpeed only enumerates interfaces and performs read-only validation before a test.

## Why host networking is required

The supplied container uses `network_mode: host`, so its network namespace sees the Linux host's interfaces, addresses, and route policy. Bridge networking would expose container veth interfaces and NAT-selected egress instead of the WAN paths being compared.

Host networking does not grant route-management authority. The container drops all capabilities, runs non-root, and receives no `CAP_NET_ADMIN` or Docker socket.

## Required model

For every task:

1. Select one non-loopback interface.
2. Select one concrete IPv4 or IPv6 address assigned to that interface.
3. Optionally attach a route profile with expected gateway/table and a safe validation destination.
4. Configure host policy routing so traffic from that source uses the intended WAN.
5. Validate the route profile before enabling the task.

MultiSpeed never silently picks one address from a multi-address interface. Discovery, route validation, public-IP lookup, server discovery, target validation, and the measurement all use the configured path.

## Inspect the host first

Run these on the Linux Docker host:

```bash
ip -brief link
ip -brief address
ip rule show
ip route show table all
ip route get 1.1.1.1 from 192.0.2.10
```

The final command should report the expected output interface, source, gateway, and table. Replace the documentation address with an address actually assigned to the WAN interface.

## Policy-routing example

The following is an **example only**. Addressing, gateway, table IDs, and persistence mechanisms are distribution-specific. Run reviewed commands on the host yourself; MultiSpeed never executes them.

```bash
ip rule add from 192.0.2.10/32 table 100 priority 100
ip route add 192.0.2.0/24 dev wan0 src 192.0.2.10 table 100
ip route add default via 192.0.2.1 dev wan0 table 100

ip rule add from 198.51.100.10/32 table 200 priority 110
ip route add 198.51.100.0/24 dev wan1 src 198.51.100.10 table 200
ip route add default via 198.51.100.1 dev wan1 table 200
```

Persist equivalent rules with the host distribution's network manager. Temporary `ip` commands disappear on reboot.

For IPv6, use appropriately scoped source prefixes and IPv6 routes. Link-local gateways normally require an interface scope. Test the exact route with `ip -6 route get ... from ...` before saving a task.

## Route profiles

A route profile persists expectations, not networking changes:

- interface and concrete source address
- optional expected gateway
- optional expected routing table name or ID
- validation hostname or IP
- operator description and notes

Validation checks the address assignment, performs a read-only route lookup, compares the returned output interface/source/gateway/table, checks reachability, and discovers public IP over the bound path. The validation snapshot and a precise mismatch explanation are stored with the result.

DNS lookup is part of the selected network path: UDP queries and TCP fallback both bind to the configured source. This applies to native API validation, Cloudflare requests, and the bundled LibreSpeed CLI; MultiSpeed patches LibreSpeed v1.0.13 because upstream `--source` does not bind its resolver. A build-time local DNS fixture proves both transports originate from the selected address. If the container sees only a loopback resolver stub or a resolver in the opposite address family, MultiSpeed uses Cloudflare's family-matched public resolver (`1.1.1.1` or `2606:4700:4700::1111`) over that same bound source. It never retries DNS through an unbound resolver or another WAN.

## Failure behavior

A test is failed or skipped without provider execution when:

- the interface disappeared or is down;
- the selected source address is no longer assigned;
- the kernel route chooses another interface or source;
- an expected gateway or route table does not match;
- bound reachability/public-IP validation fails under a required policy; or
- the provider cannot bind to the requested path.

The result identifies the configured interface, source IP, provider, target, and sanitized operating-system error. There is no fallback to another WAN.

## Interface changes

Interface discovery refreshes periodically and can be refreshed through the UI/API. Hotplug, DHCP renewal, IPv6 privacy addresses, and renaming can invalidate tasks. Prefer stable interface names and stable source addresses for scheduled monitoring. After network changes, refresh interfaces and revalidate every affected route profile.

## Containers, VPNs, and namespaces

- Run MultiSpeed in the network namespace containing the WAN interfaces.
- Docker Desktop and non-Linux hosts do not provide the required semantics.
- A VPN may install higher-priority rules that change every source route; verify `ip rule` and route-profile output.
- VRF and advanced namespace deployments require the MultiSpeed process to run in the relevant namespace; the default Compose file does not enter arbitrary namespaces.
- Provider endpoints may block source ranges, IPv6, or repeated tests independently of routing correctness.

## Concurrency

The default global concurrency is one. Even when separate-WAN concurrency is enabled, MultiSpeed prevents overlapping runs on the same interface/source pair. Start conservatively: simultaneous tests can contend for CPU, memory bandwidth, switching capacity, upstream links, or shared last-mile infrastructure and distort results.
