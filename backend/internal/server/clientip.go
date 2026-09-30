package server

import (
	"net/http"
	"net/netip"
	"strings"
)

// clientKey returns the rate-limit key for the request's client: its IP as a
// single-address prefix, or its /64 for IPv6 (one subscriber usually holds a
// whole /64, so single IPv6 addresses would be free to rotate).
//
// With hops = 0 the client is the TCP peer. With hops = N (TRUSTED_PROXY_HOPS)
// it is the N-th X-Forwarded-For entry from the right: each trusted proxy
// appends the address it received the request from, so entries further left
// are client-controlled and never used. This holds only while the server is
// reachable exclusively through those N proxies (decision 018); exposed
// directly, any client could send its own X-Forwarded-For and pick its key.
// If the chain is shorter than N or the entry isn't an IP address, the peer
// is used: requests that didn't come through the expected proxies share the
// peer's bucket instead of choosing their own.
func clientKey(r *http.Request, hops int) netip.Prefix {
	var ip netip.Addr
	if hops > 0 {
		ip, _ = forwardedFor(r.Header.Values("X-Forwarded-For"), hops)
	}
	if !ip.IsValid() {
		ip = parseHost(r.RemoteAddr)
	}
	if !ip.IsValid() {
		return netip.Prefix{}
	}
	ip = ip.Unmap().WithZone("")
	bits := ip.BitLen()
	if ip.Is6() {
		bits = 64
	}
	p, _ := ip.Prefix(bits)
	return p
}

// forwardedFor returns the hops-th entry from the right of the
// X-Forwarded-For header values, taken together as one list.
func forwardedFor(values []string, hops int) (netip.Addr, bool) {
	var entries []string
	for _, v := range values {
		entries = append(entries, strings.Split(v, ",")...)
	}
	if len(entries) < hops {
		return netip.Addr{}, false
	}
	ip := parseHost(strings.TrimSpace(entries[len(entries)-hops]))
	return ip, ip.IsValid()
}

// parseHost parses "ip", "ip:port" or "[ipv6]:port".
func parseHost(s string) netip.Addr {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr()
	}
	ip, _ := netip.ParseAddr(s)
	return ip
}
