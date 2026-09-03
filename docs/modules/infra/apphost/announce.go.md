# Module: infra/apphost/announce.go

## Purpose

Prints the "here is where to browse" banner on every boot, and opens the primary URL in a
browser. Answers the first question anyone has after starting the app: what do I type
into a browser? Previously answerable only by reading `config.json`, because the only
thing printed was a structured log line per listener (`starting https server on :3002`) —
naming a PORT but no scheme and no host, and on a wildcard bind `:3002` is not an address
a person can visit at all. The apps did print a friendly URL, but only on the very FIRST
run, as a side effect of announcing the bootstrap password; every boot afterwards said
nothing.

## `resolveReadyAddresses(listeners []listenerSpec) readyAddresses`

Works out what to tell the operator from the listeners actually started, grouped the way
an operator reads it rather than as one flat list — the grouping is the point. A wildcard
bind on a developer or appliance box expands to every interface (a VPN tunnel, a WSL
bridge, a VirtualBox host-only adapter), and with two listeners that is ten URLs, nine of
them useless; ten lines of noise hides the answer as effectively as printing nothing did.

`readyAddresses`: `Primary` (the URL to open on this machine, and the one handed to the
browser — HTTPS first, then localhost first, since localhost is the only address
guaranteed to work from the machine the banner is printed on), `Local` (every URL that
works on this machine), `Network` (every URL that works from another machine, via the
interface carrying this host's default route), `OtherIPs` (the remaining local interface
addresses, bare — they serve the same listeners, they are just rarely the ones anybody
wants).

- A wildcard bind (`""`, `0.0.0.0`, `::`) resolves to `localhost` for `Local`, the
  primary-route IP (`primaryNetworkIP`) for `Network`, and every other routable IPv4
  (`localIPv4s`) into `OtherIPs`.
- A loopback bind (`127.0.0.1`, `::1`, `localhost`) resolves to `localhost` only —
  advertising a LAN address here would send the operator to a port nothing is listening
  on.
- An explicit hostname is never expanded — it is what the operator meant.

## `primaryNetworkIP() string`

Returns the IPv4 address of the interface carrying this host's default route — the
address another machine would actually reach. Uses a UDP "dial" to an RFC 5737
documentation address (`192.0.2.1:9`) purely to trigger the kernel's route lookup and
local-address bind; nothing is sent, and nothing here depends on or names an outside
service — the air-gapped installs (myseliasan + its myidsan hop) must stay air-gapped.
Returns `""` with no default route (an isolated appliance), and the banner then lists the
interfaces instead.

## `announceReady(appName string, listeners []listenerSpec, selfSignedTLS bool) string`

Prints the banner to **stdout** (not through the logger, for the same reason the
first-run credential banner does — it must land verbatim in `docker logs`, in a journal,
and in a terminal, not as one JSON line among hundreds) and, when `selfSignedTLS` is
true, an explanatory paragraph about the one-time browser trust warning. Launches the
browser (`launchBrowser`, `browser.go.md`) BEFORE printing, so the banner can state what
actually happened rather than what was merely intended, then logs one structured line so
the URL is greppable in a log file long after the banner has scrolled out of a terminal.
Returns `""` (prints nothing) when no address can be described.

## Notes

- Called once from `run.go`'s `runApp`, after listeners are up (`run.go.md`).
- `anyTLS(listeners []listenerSpec) bool` decides whether the certificate warning is
  relevant at all; paired with `isSelfSignedCert` (`selfcert.go.md`) at the call site.
- `localIPv4s` deliberately excludes IPv6 — a bracketed IPv6 literal is not something
  anyone types into a browser bar, and the IPv4 address reaches the same listener.
