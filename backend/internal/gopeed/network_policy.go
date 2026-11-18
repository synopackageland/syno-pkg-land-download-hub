package gopeed

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// The pinned SDK has no public outbound HTTP policy. Keep the App's fetches
// pinned to the addresses just checked, with no proxy or second DNS lookup.
// This transport does not govern sockets opened by external download engines.
var nonPublicRanges = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32"} {
		_, block, _ := net.ParseCIDR(cidr)
		out = append(out, block)
	}
	return out
}()

func publicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range nonPublicRanges {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

func publicHostname(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return publicIP(ip)
	}
	return true
}

type lookupAddress func(context.Context, string) ([]net.IPAddr, error)
type dialAddress func(context.Context, string, string) (net.Conn, error)

func publicClient() *http.Client {
	return publicClientWithNetwork(net.DefaultResolver.LookupIPAddr, (&net.Dialer{Timeout: 5 * time.Second}).DialContext)
}

func publicClientWithNetwork(lookup lookupAddress, dial dialAddress) *http.Client {
	denied := errors.New("destination denied")
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !publicHostname(host) {
			return nil, denied
		}
		ips, err := lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, denied
		}
		for _, ip := range ips {
			if ip.Zone != "" || !publicIP(ip.IP) {
				return nil, denied
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !safeURL(req.URL.String()) {
			return denied
		}
		// The next connection uses this same checked and pinned transport.
		return nil
	}}
}
