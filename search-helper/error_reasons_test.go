package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/mediactl/clustarr/pkg/cardigann"
)

func TestSearchErrorReasonsDoNotExposeRequestSecrets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&net.DNSError{Err: "no such host", Name: "private.example"}, "search_dns"},
		{&net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")}, "search_connection"},
		{context.DeadlineExceeded, "search_timeout"},
		{&net.OpError{Op: "read", Err: syscall.ECONNRESET}, "search_connection_reset"},
		{x509.UnknownAuthorityError{}, "search_tls"},
		{errDestinationDenied, "search_destination_denied"},
		{&cardigann.StatusError{StatusCode: 403}, "search_http_403"},
		{&cardigann.StatusError{StatusCode: 429}, "search_http_429"},
		{&cardigann.StatusError{StatusCode: 503}, "search_http_503"},
		{&cardigann.StatusError{StatusCode: 401}, "search_login"},
		{&cardigann.CloudflareChallengeError{StatusCode: 403}, "search_cloudflare"},
	} {
		wrapped := &url.Error{Op: "Get", URL: "https://private.example/?apikey=secret", Err: tc.err}
		if got := classify(wrapped); got != tc.want {
			t.Errorf("%T: got %s, want %s", tc.err, got, tc.want)
		}
	}
}
