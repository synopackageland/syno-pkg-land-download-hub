package main

import (
	"net"
	"testing"
)

func TestRefusedIPKeepsNonPublicRangesOffTheInternetClient(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "169.254.169.254", "fe80::1", "0.0.0.0", "10.0.0.1", "192.168.1.1", "fd00::1", "100.64.0.1", "100.127.255.254", "198.18.0.1", "192.0.0.8", "240.0.0.1", "64:ff9b::7f00:1"} {
		if !refusedIP(net.ParseIP(raw), false) {
			t.Fatalf("%s admitted", raw)
		}
	}
	for _, raw := range []string{"93.184.216.34", "1.1.1.1", "2606:4700::1111", "100.128.0.1"} {
		if refusedIP(net.ParseIP(raw), false) {
			t.Fatalf("%s refused", raw)
		}
	}
	// LAN sources may reach private addresses but never loopback or link-local.
	if refusedIP(net.ParseIP("192.168.1.1"), true) || !refusedIP(net.ParseIP("127.0.0.1"), true) || !refusedIP(net.ParseIP("169.254.169.254"), true) {
		t.Fatal("lan policy changed")
	}
}
