package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsBlockedIP_LoopbackAndPrivate(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"127.1.2.3", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true},
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"192.168.0.5", true},
		{"169.254.1.1", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"fc00::1", true},
		{"fd00::1", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"224.0.0.1", true},
		{"ff02::1", true},
		{"0.0.0.0", true},
		{"::", true},
		// public
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"203.0.113.1", false},   // TEST-NET-3 is not in our blocklist; should be considered public for this client
		{"93.184.216.34", false}, // example.com
		{"2606:4700:4700::1111", false},
		{"::ffff:8.8.8.8", false},
		{"::ffff:1.1.1.1", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("failed to parse %q", tc.ip)
		}
		got := isBlockedIP(ip)
		if got != tc.blocked {
			t.Errorf("isBlockedIP(%q)=%v want %v", tc.ip, got, tc.blocked)
		}
	}
}

func TestValidateImageURLScheme(t *testing.T) {
	okCases := []string{"http://example.com/a.jpg", "https://example.com/a.jpg", "https://example.com:8080/path?q=1"}
	for _, c := range okCases {
		if _, err := ValidateImageURLScheme(c); err != nil {
			t.Errorf("ValidateImageURLScheme(%q) unexpected error: %v", c, err)
		}
	}
	badCases := []string{"file:///etc/passwd", "gopher://example.com/", "ftp://example.com/file", "data:image/png;base64,abc", "http://", "//example.com/a.jpg", ""}
	for _, c := range badCases {
		if _, err := ValidateImageURLScheme(c); err == nil {
			t.Errorf("ValidateImageURLScheme(%q) expected error", c)
		}
	}
}

func TestNewSSRFSafeClient_BlocksLoopbackServer(t *testing.T) {
	// Integration: httptest.NewServer binds to 127.0.0.1 — safe client must refuse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("should not be reached"))
	}))
	defer srv.Close()

	client := NewSSRFSafeClient(2 * time.Second)
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error for loopback server, got nil")
	}
	if !strings.Contains(err.Error(), "blocked by SSRF") {
		t.Fatalf("expected SSRF block error, got %v", err)
	}
}

func TestNewSSRFSafeClient_BlocksPrivateIPLiteral(t *testing.T) {
	client := NewSSRFSafeClient(2 * time.Second)
	// Use literal private IP without starting server — dial should be blocked before connect.
	_, err := client.Get("http://10.0.0.1:81/image.jpg")
	if err == nil {
		t.Fatal("expected error for private IP")
	}
	if !strings.Contains(err.Error(), "blocked by SSRF") {
		t.Fatalf("expected SSRF block, got %v", err)
	}
	_, err = client.Get("http://192.168.1.1:81/image.jpg")
	if err == nil {
		t.Fatal("expected error for 192.168")
	}
	_, err = client.Get("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected error for metadata IP")
	}
}

func TestNewSSRFSafeClient_RedirectToPrivateBlocked(t *testing.T) {
	// Server B is the final private target.
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("B"))
	}))
	defer srvB.Close()
	// Server A redirects to B. Even though A itself is private, the redirect hop to B must also be blocked.
	// If the client blocks A immediately, it still satisfies "blocked". If it allows A, the redirect to B must be blocked.
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srvB.URL, http.StatusFound)
	}))
	defer srvA.Close()

	client := NewSSRFSafeClient(2 * time.Second)
	_, err := client.Get(srvA.URL)
	if err == nil {
		t.Fatal("expected error for redirect to private, got nil")
	}
	if !strings.Contains(err.Error(), "blocked by SSRF") {
		t.Fatalf("expected SSRF block on redirect, got %v", err)
	}
}

func TestSSRFSafeDialContext_MockPublicAllowAndPrivateBlock(t *testing.T) {
	// Seam test: inject fake DNS for "public.example" -> 93.184.216.34 (public), "private.example" -> 10.0.0.5 (blocked).
	origLookup := ssrfLookupIPAddr
	defer func() { ssrfLookupIPAddr = origLookup }()

	ssrfLookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "public.example":
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		case "private.example":
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
		case "mixed.example":
			// Mixed: first private, then public — dialer should skip private and succeed on public.
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}, {IP: net.ParseIP("93.184.216.34")}}, nil
		default:
			return net.DefaultResolver.LookupIPAddr(ctx, host)
		}
	}

	// Start a real server on loopback but we'll dial via fake public IP mapping.
	// To avoid needing real network, we test the dialer function directly with a listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	addr := ln.Addr().String() // 127.0.0.1:port — but we will not dial it via public path.

	// For public.example, the dialer will resolve to 93.184.216.34 and try to dial that IP.
	// That will fail to connect (no server on 93...), but the error should NOT be "blocked by SSRF".
	// So we can assert the dialer did not return SSRF block error for public.
	dial := ssrfSafeDialContext(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = dial(ctx, "tcp", "public.example:80")
	if err != nil && strings.Contains(err.Error(), "blocked by SSRF") {
		t.Fatalf("public.example should not be blocked, got %v", err)
	}
	// Private must be blocked.
	_, err = dial(ctx, "tcp", "private.example:80")
	if err == nil || !strings.Contains(err.Error(), "blocked by SSRF") {
		t.Fatalf("private.example should be blocked, got %v", err)
	}
	// Mixed should succeed (skip private, dial public). Since public 93... has no listener, it will still fail but not with SSRF block.
	_, err = dial(ctx, "tcp", "mixed.example:80")
	if err != nil && strings.Contains(err.Error(), "blocked by SSRF") {
		// If it reports blocked, it means it didn't try the public second entry.
		t.Fatalf("mixed.example should skip private and try public, not be blocked, got %v", err)
	}
	// Literal public IP should be allowed (no SSRF block).
	_ = addr // avoid unused
	if isBlockedIPString("93.184.216.34") {
		t.Fatal("public IP should not be blocked")
	}
	if !isBlockedIPString("10.0.0.5") {
		t.Fatal("private IP should be blocked")
	}
}

func TestNewSSRFSafeClient_AllowsPublicViaAllowlist(t *testing.T) {
	// With explicit allowlist containing 127.0.0.0/8, loopback should be allowed when allowlisted.
	_, cidr, _ := net.ParseCIDR("127.0.0.0/8")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	addr := ln.Addr().String()
	client := NewSSRFSafeClientWithAllowlist(2*time.Second, []*net.IPNet{cidr})
	// Direct dial with allowlist should succeed (no SSRF block).
	dial := ssrfSafeDialContext([]*net.IPNet{cidr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("allowlisted loopback %s should not be blocked, got %v", addr, err)
	}
	_ = conn.Close()
	// Also verify that without allowlist the same addr is blocked.
	dialNoAllow := ssrfSafeDialContext(nil)
	_, err = dialNoAllow(ctx, "tcp", addr)
	if err == nil || !strings.Contains(err.Error(), "blocked by SSRF") {
		t.Fatalf("non-allowlisted loopback should be blocked, got %v", err)
	}
	_ = client
}
