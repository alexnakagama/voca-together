package server

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientKey(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remote string
		xff    []string
		hops   int
		want   string
	}{
		{"no proxy uses the peer", "203.0.113.7:4000", nil, 0, "203.0.113.7/32"},
		{"no proxy ignores X-Forwarded-For", "203.0.113.7:4000", []string{"198.51.100.1"}, 0, "203.0.113.7/32"},
		{"one hop takes the rightmost entry", "10.0.0.1:4000", []string{"1.1.1.1, 198.51.100.1"}, 1, "198.51.100.1/32"},
		{"spoofed left entries are ignored", "10.0.0.1:4000", []string{"6.6.6.6, 7.7.7.7, 198.51.100.1"}, 1, "198.51.100.1/32"},
		{"two hops", "10.0.0.1:4000", []string{"6.6.6.6, 198.51.100.1, 10.0.0.2"}, 2, "198.51.100.1/32"},
		{"several headers are one list", "10.0.0.1:4000", []string{"6.6.6.6", "198.51.100.1, 10.0.0.2"}, 2, "198.51.100.1/32"},
		{"entry with a port", "10.0.0.1:4000", []string{"198.51.100.1:5555"}, 1, "198.51.100.1/32"},
		{"IPv6 entry with a port", "10.0.0.1:4000", []string{"[2001:db8::1]:5555"}, 1, "2001:db8::/64"},
		{"chain too short falls back to the peer", "10.0.0.1:4000", []string{"198.51.100.1"}, 2, "10.0.0.1/32"},
		{"missing header falls back to the peer", "10.0.0.1:4000", nil, 1, "10.0.0.1/32"},
		{"garbage falls back to the peer", "10.0.0.1:4000", []string{"6.6.6.6, not-an-ip"}, 1, "10.0.0.1/32"},
		{"empty entry falls back to the peer", "10.0.0.1:4000", []string{"198.51.100.1, "}, 1, "10.0.0.1/32"},
		{"IPv4-mapped IPv6 is IPv4", "[::ffff:203.0.113.7]:4000", nil, 0, "203.0.113.7/32"},
		{"IPv6 is grouped by /64", "[2001:db8:1:2:aaaa::1]:4000", nil, 0, "2001:db8:1:2::/64"},
		{"IPv6 zone is dropped", "[fe80::1%eth0]:4000", nil, 0, "fe80::/64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientKey(r, tc.hops); got != netip.MustParsePrefix(tc.want) {
				t.Errorf("clientKey = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestClientKeySameSlash64SharesKey(t *testing.T) {
	a := httptest.NewRequest("POST", "/", nil)
	a.RemoteAddr = "[2001:db8::1]:1"
	b := httptest.NewRequest("POST", "/", nil)
	b.RemoteAddr = "[2001:db8::ffff:1]:2"
	if clientKey(a, 0) != clientKey(b, 0) {
		t.Error("addresses in one /64 got different keys")
	}
}

func TestClientKeyUnparsablePeer(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.RemoteAddr = "@"
	if got := clientKey(r, 0); got.IsValid() {
		t.Errorf("clientKey = %v, want the zero prefix", got)
	}
}
