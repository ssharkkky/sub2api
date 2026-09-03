package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const ssrfSafeDialTimeout = 5 * time.Second
const ssrfSafeTLSHandshakeTimeout = 5 * time.Second

// ssrfBlockedCIDRs mirrors channel_monitor_ssrf monitorBlockedCIDRs plus
// multicast/unspecified hardening.
var ssrfBlockedCIDRs = mustParseCIDRs([]string{
	"127.0.0.0/8",    // IPv4 loopback
	"10.0.0.0/8",     // RFC1918
	"172.16.0.0/12",  // RFC1918
	"192.168.0.0/16", // RFC1918
	"169.254.0.0/16", // link-local (includes 169.254.169.254 cloud metadata)
	"100.64.0.0/10",  // CGNAT
	"0.0.0.0/8",      // "this network" / 0.0.0.0
	"::1/128",        // IPv6 loopback
	"fc00::/7",       // IPv6 ULA
	"fe80::/10",      // IPv6 link-local
	"::/128",         // IPv6 unspecified
	"ff00::/8",       // IPv6 multicast
	"224.0.0.0/4",    // IPv4 multicast
})

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("httpclient/ssrf: invalid CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

// isBlockedIP reports whether ip must be rejected for SSRF.
// Handles IPv4-mapped IPv6 by normalizing to 4-byte form for CIDR checks
// and also checking the original IP's IsLoopback/IsMulticast etc.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Fast checks that handle IPv4-mapped correctly.
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Normalize IPv4-mapped IPv6 (::ffff:127.0.0.1) to 4-byte for CIDR matching.
	// Keep original for IPv6 CIDR checks as well.
	norm := ip
	if ip4 := ip.To4(); ip4 != nil {
		norm = ip4
	}
	for _, n := range ssrfBlockedCIDRs {
		if n.Contains(norm) {
			return true
		}
		if n.Contains(ip) {
			return true
		}
	}
	// Fallback: net.IP.IsPrivate covers 10/8,172.16/12,192.168/16, fc00::/7 etc.
	// Keep as belt-and-suspenders after explicit CIDRs.
	if ip.IsPrivate() {
		return true
	}
	return false
}

// isBlockedIPString is a convenience for tests.
func isBlockedIPString(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return false
	}
	return isBlockedIP(ip)
}

// ValidateImageURLScheme checks that rawURL uses only http or https.
// Exported for reuse in kiro translator/token paths.
func ValidateImageURLScheme(rawURL string) (string, error) {
	return validateImageURLScheme(rawURL)
}

// validateImageURLScheme checks that rawURL uses only http or https.
// Returns normalized string and error. This mirrors isRemoteImageURL guarantee
// but makes the allowlist explicit for defense-in-depth.
func validateImageURLScheme(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("empty url")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	return trimmed, nil
}

// ssrfLookupIPAddr is a seam for tests to inject fake DNS.
// Production uses net.DefaultResolver.LookupIPAddr.
var ssrfLookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// ssrfSafeDialContext is the core dialer that blocks private/link-local/etc
// IPs on every new connection, including redirect hops (http.Client
// re-dials via the same Transport for each redirect).
func ssrfSafeDialContext(allowlist []*net.IPNet) func(ctx context.Context, network, address string) (net.Conn, error) {
	baseDialer := &net.Dialer{
		Timeout:   ssrfSafeDialTimeout,
		KeepAlive: 30 * time.Second,
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		// Explicit allowlist check: if allowlist is non-empty and address is within
		// any allowlisted CIDR, bypass blocking. Currently default allowlist is nil/empty => deny all private.
		isAllowed := func(ip net.IP) bool {
			if len(allowlist) == 0 {
				return false
			}
			for _, n := range allowlist {
				if n.Contains(ip) {
					return true
				}
			}
			return false
		}

		// Fast path: literal IP
		if ip := net.ParseIP(host); ip != nil {
			if isAllowed(ip) {
				return baseDialer.DialContext(ctx, network, address)
			}
			if isBlockedIP(ip) {
				return nil, &net.AddrError{Err: "blocked by SSRF policy", Addr: address}
			}
			return baseDialer.DialContext(ctx, network, address)
		}
		// Hostname: resolve all A/AAAA and try each non-blocked IP.
		addrs, err := ssrfLookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(addrs) == 0 {
			return nil, &net.AddrError{Err: "no addresses for host", Addr: host}
		}
		var lastErr error
		for _, a := range addrs {
			ip := a.IP
			if isAllowed(ip) {
				conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
				continue
			}
			if isBlockedIP(ip) {
				lastErr = &net.AddrError{Err: "blocked by SSRF policy", Addr: ip.String()}
				continue
			}
			conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = &net.AddrError{Err: "no usable addresses", Addr: host}
		}
		return nil, lastErr
	}
}

// NewSSRFSafeClient returns an http.Client whose Transport blocks
// SSRF-prone destinations on every dial (including 302 redirects).
// Timeout is the overall request timeout (e.g. 8s for Kiro images).
// Optional allowlist CIDRs bypass blocking when non-empty; default empty => deny all private/link-local/etc.
func NewSSRFSafeClient(timeout time.Duration, allowlist ...*net.IPNet) *http.Client {
	var flat []*net.IPNet
	for _, n := range allowlist {
		if n != nil {
			flat = append(flat, n)
		}
	}
	tr := &http.Transport{
		DialContext:           ssrfSafeDialContext(flat),
		TLSHandshakeTimeout:   ssrfSafeTLSHandshakeTimeout,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
}

// NewSSRFSafeClientWithAllowlist is an alias for NewSSRFSafeClient with explicit allowlist.
func NewSSRFSafeClientWithAllowlist(timeout time.Duration, allowlist []*net.IPNet) *http.Client {
	return NewSSRFSafeClient(timeout, allowlist...)
}
