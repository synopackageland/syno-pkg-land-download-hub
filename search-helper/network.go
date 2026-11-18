package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
)

func fmtInt(n int) string { return strconv.Itoa(n) }

var errDestinationDenied = errors.New("destination refused")

// nonPublic are ranges that are not on the public internet but that a NAS
// may still route to: carrier NAT and VPN overlays, IETF protocol
// assignments, benchmarking, reserved space and NAT64 prefixes.
var nonPublic = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48"} {
		_, n, _ := net.ParseCIDR(cidr)
		out = append(out, n)
	}
	return out
}()

// refusedIP reports whether a helper request may not dial ip. lan admits
// private ranges only.
func refusedIP(ip net.IP, lan bool) bool {
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	if lan {
		return false
	}
	if ip.IsPrivate() {
		return true
	}
	for _, n := range nonPublic {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
func newClient(lan bool) *http.Client {
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if refusedIP(ip.IP, lan) {
				return nil, errDestinationDenied
			}
		}
		var lastErr error
		for _, ip := range ips {
			c, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return c, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("destination unreachable")
	}
	return &http.Client{Transport: transport, Timeout: 23 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirect limit")
		}
		if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
			return errors.New("scheme refused")
		}
		if len(via) > 0 && (req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme) {
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
			req.Header.Del("X-Api-Key")
		}
		return nil
	}}
}
