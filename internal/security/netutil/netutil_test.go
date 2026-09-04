package netutil

import (
	"context"
	"net"
	"net/url"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true}, {"10.0.0.1", true}, {"100.64.0.1", true},
		{"192.0.2.1", true}, {"198.51.100.1", true}, {"203.0.113.1", true},
		{"::1", true}, {"2001:db8::1", true}, {"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := IsPrivateIP(net.ParseIP(tt.ip)); got != tt.blocked {
				t.Fatalf("IsPrivateIP(%s) = %v, want %v", tt.ip, got, tt.blocked)
			}
		})
	}
}

func TestValidatePublicHTTPURLRejectsUnsafeDestinations(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd", "http://user:pass@example.com/", "http://127.0.0.1/",
		"http://[::1]/", "http://192.0.2.10/",
	} {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidatePublicHTTPURL(context.Background(), u); err == nil {
				t.Fatalf("expected %q to be rejected", raw)
			}
		})
	}
}

func TestPublicOnlyDialContextRejectsLoopback(t *testing.T) {
	dial := PublicOnlyDialContext(nil)
	if _, err := dial(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		t.Fatal("expected loopback dial to be rejected")
	}
}
