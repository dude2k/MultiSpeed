# MultiSpeed LibreSpeed source-bound DNS overlay

MultiSpeed builds the immutable `github.com/librespeed/speedtest-cli` v1.0.13
module, then applies this LGPL-3.0-or-later overlay before compiling it.
The build also pins `golang.org/x/net` v0.56.0 (and the compatible transitive
versions selected by Go 1.26.6) instead of upstream's vulnerable v0.49.0.
The resulting compatibility marker is `+multispeed.dns2.xnet056`.

The upstream `--source` implementation sets `net.Dialer.LocalAddr` for HTTP
connections but leaves hostname lookups on `net.DefaultResolver`. MultiSpeed
adds `source_bound_resolver.go` and inserts this line immediately after the
upstream `defaultDialer.LocalAddr = localTCPAddr` assignment:

```go
defaultDialer.Resolver = newSourceBoundResolver(addr.IP)
if err := restrictDialerToAllowedServerEndpoints(defaultDialer); err != nil { return nil, err }
```

For operator-authorized custom backends, MultiSpeed also passes the exact
source-bound DNS result and canonical server port through a runner-only
environment value. The overlay pins every measurement connection to that
numeric IP:port set and rejects redirects before a follow-up request, so a
second lookup or backend response cannot move the CLI to a different origin or
port. Normal public server discovery does not enable this restriction.

The resolver uses Go's DNS client and binds both UDP queries and TCP fallback
to the same selected source address. A loopback or opposite-family system
resolver is replaced by the family-matched Cloudflare resolver, still through
that bound source. It never retries through an unbound dialer.

`source_bound_resolver_test.go` runs during the container build. Its local DNS
fixture returns a truncated UDP response, completes over TCP, and asserts that
both connections originate from the selected source address. Go excludes the
`_test.go` file from the production binary, while the complete overlay and test
remain in the corresponding-source archive and notices shipped with the image.
