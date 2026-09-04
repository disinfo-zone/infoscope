package netutil

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var blockedNetworks = parseBlockedCIDRs()

func parseBlockedCIDRs() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
		"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7",
		"fe80::/10", "2001:db8::/32", "ff00::/8",
	}
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			networks = append(networks, network)
		}
	}
	return networks
}

// IsPrivateIP reports whether ip must not be contacted by user-directed
// outbound HTTP. The historic name is retained for callers, but the check also
// covers reserved, unspecified, multicast, and documentation ranges.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return !ip.IsGlobalUnicast()
}

// ValidateHTTPURL performs the non-network portion of outbound URL validation.
func ValidateHTTPURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("missing URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("must use HTTP or HTTPS")
	}
	u.Scheme = scheme
	if strings.TrimSpace(u.Hostname()) == "" {
		return fmt.Errorf("URL must include a host")
	}
	if u.User != nil {
		return fmt.Errorf("URL credentials are not allowed")
	}
	return nil
}

// ValidatePublicHTTPURL resolves an HTTP URL and rejects it if any returned
// address is non-public. Connections must still use PublicOnlyDialContext to
// close the DNS-rebinding gap between validation and dialing.
func ValidatePublicHTTPURL(ctx context.Context, u *url.URL) error {
	if err := ValidateHTTPURL(u); err != nil {
		return err
	}
	_, err := resolvePublicIPs(ctx, u.Hostname())
	return err
}

func resolvePublicIPs(ctx context.Context, host string) ([]net.IP, error) {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return nil, fmt.Errorf("destination is not a public address")
		}
		return []net.IP{ip}, nil
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("host has no IP addresses")
	}
	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return nil, fmt.Errorf("destination resolves to a non-public address")
		}
	}
	return ips, nil
}

// PublicOnlyDialContext resolves and validates a destination at connection
// time, then dials the validated IP directly. This closes DNS rebinding.
func PublicOnlyDialContext(dialer *net.Dialer) func(context.Context, string, string) (net.Conn, error) {
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid destination address: %w", err)
		}
		ips, err := resolvePublicIPs(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, fmt.Errorf("dial public destination: %w", lastErr)
	}
}

// CheckRedirect validates every redirect target and enforces a finite chain.
func CheckRedirect(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if err := ValidateHTTPURL(req.URL); err != nil {
			return fmt.Errorf("unsafe redirect: %w", err)
		}
		return nil
	}
}
