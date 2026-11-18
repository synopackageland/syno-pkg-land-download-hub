package gopeed

import (
	"context"
	"net"
	"net/http"
	"testing"
)

func TestPublicSourcePolicyRejectsReservedAddresses(t *testing.T) {
	for _, host := range []string{"100.64.0.1", "198.18.0.1", "240.0.0.1", "224.0.0.1", "[64:ff9b::7f00:1]", "localhost."} {
		if safeURL("https://"+host+"/file") || publicHost(host) {
			t.Errorf("non-public host admitted: %s", host)
		}
	}
}

func TestPublicFetchPinsDNSAndChecksEveryRedirect(t *testing.T) {
	calls, dials := 0, 0
	lookup := func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		if calls == 1 {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}
		return []net.IPAddr{{IP: net.IPv4(127, 0, 0, 1)}}, nil
	}
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		dials++
		if address != "93.184.216.34:80" {
			t.Fatalf("DNS was not pinned: %s", address)
		}
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			buf := make([]byte, 4096)
			server.Read(buf)
			server.Write([]byte("HTTP/1.1 302 Found\r\nLocation: http://rebound.example/file\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		}()
		return client, nil
	}
	client := publicClientWithNetwork(lookup, dial)
	if resp, err := client.Get("http://public.example/file"); err == nil {
		resp.Body.Close()
		t.Fatal("private redirect destination admitted")
	}
	if calls != 2 || dials != 1 {
		t.Fatalf("lookup=%d dial=%d", calls, dials)
	}
	client.CloseIdleConnections()
}

func TestPublicFetchRejectsMixedDNSAnswersBeforeDial(t *testing.T) {
	client := publicClientWithNetwork(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.IPv4(169, 254, 169, 254)}}, nil
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("dialed a mixed public/private answer")
		return nil, nil
	})
	if _, err := client.Get("http://mixed.example/file"); err == nil {
		t.Fatal("mixed DNS answers admitted")
	}
	if client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy bypasses policy")
	}
}
