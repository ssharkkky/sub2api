package kiro

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func resetSSRFTestCache() {
	imageTokenEstimates.Lock()
	imageTokenEstimates.entries = make(map[string]imageTokenCacheEntry)
	imageTokenEstimates.Unlock()
}

func TestEstimateImageTokens_BlocksLoopbackSSRF(t *testing.T) {
	resetSSRFTestCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png-body"))
	}))
	defer srv.Close()

	// kiroRemoteImageHTTPClient is now SSRF-safe and must refuse 127.0.0.1.
	// EstimateImageTokens should fall back to 1600 and not hit the server's image.
	tokens := EstimateImageTokens(context.Background(), "", srv.URL+"/image.png")
	require.Equal(t, kiroImageTokenFallback, tokens)
}

func TestBuildKiroImageFromRemoteURL_BlocksLoopbackSSRF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fake-png-body"))
	}))
	defer srv.Close()

	_, ok := buildKiroImageFromRemoteURL(srv.URL + "/image.png")
	require.False(t, ok, "loopback image URL must be blocked by SSRF-safe client")
}

func TestBuildKiroImageFromRemoteURL_RejectsNonHTTPScheme(t *testing.T) {
	_, ok := buildKiroImageFromRemoteURL("file:///etc/passwd")
	require.False(t, ok)
	_, ok = buildKiroImageFromRemoteURL("gopher://example.com/")
	require.False(t, ok)
	_, ok = buildKiroImageFromRemoteURL("data:image/png;base64,abc") // not remote, handled elsewhere but should be false for remote path
	require.False(t, ok)
}

func TestFetchRemoteImageTokens_BlocksPrivateIPLiteral(t *testing.T) {
	resetSSRFTestCache()
	// Literal private IP should be blocked without DNS.
	_, ok := fetchRemoteImageTokens(context.Background(), "http://10.0.0.1:8080/image.png")
	require.False(t, ok)
	_, ok = fetchRemoteImageTokens(context.Background(), "http://169.254.169.254/latest/meta-data/")
	require.False(t, ok)
	_, ok = fetchRemoteImageTokens(context.Background(), "https://192.168.1.1/image.jpg")
	require.False(t, ok)
}
