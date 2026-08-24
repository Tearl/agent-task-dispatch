package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestProtocolHealthCheckerRequiresCompatibleHealthyHTTPSResponse(t *testing.T) {
	response := `{"status":"healthy","protocolVersion":"1"}`
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			t.Errorf("health checker requested %q instead of /health", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		writer.Header().Set("content-type", "application/json")
		_, _ = writer.Write([]byte(response))
	}))
	defer server.Close()
	checker := NewProtocolHealthChecker(true)
	checker.tlsConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if err := checker.Check(context.Background(), server.URL); err != nil {
		t.Fatalf("healthy compatible endpoint rejected: %v", err)
	}
	response = `{"status":"healthy","protocolVersion":"2"}`
	if err := checker.Check(context.Background(), server.URL); err == nil {
		t.Fatal("incompatible protocol accepted")
	}
	response = strings.Repeat("x", maxHealthResponseBytes+1)
	if err := checker.Check(context.Background(), server.URL); err == nil {
		t.Fatal("oversized health response accepted")
	}
}

func TestProtocolHealthCheckerRejectsUnsafeTargetsAndURLs(t *testing.T) {
	for _, raw := range []string{"http://agent.example", "https://user:secret@agent.example", "https://agent.example/health", "https://agent.example?token=secret", "file:///tmp/agent"} {
		if ValidEndpointURL(raw) {
			t.Fatalf("unsafe endpoint accepted: %s", raw)
		}
	}
	if !ValidEndpointURL("https://agent.example") {
		t.Fatal("clean HTTPS protocol base URL rejected")
	}
	for _, raw := range []string{
		"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.1.1", "192.0.2.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1", "::1", "::",
		"::ffff:127.0.0.1", "::ffff:8.8.8.8", "::ffff:0:127.0.0.1", "64:ff9b::1", "100::1", "2001:db8::1", "2002::1", "2620:4f:8000::1", "fc00::1", "fec0::1", "fe80::1",
		"4000::1", "6000::1", "8000::1", "e000::1",
	} {
		address := netip.MustParseAddr(raw)
		if publicAddress(address) {
			t.Fatalf("restricted address accepted: %s", raw)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
	if !publicAddress(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public IPv6 address rejected")
	}
}

func TestProtocolHealthCheckerRejectsMixedDNSAnswers(t *testing.T) {
	addresses := []netip.Addr{
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("127.0.0.1"),
	}
	if _, err := validateHealthAddresses(addresses, false); err == nil {
		t.Fatal("mixed public and restricted DNS answers accepted")
	}
	allowed, err := validateHealthAddresses(addresses, true)
	if err != nil || len(allowed) != len(addresses) {
		t.Fatalf("explicit private-network mode rejected mixed answers: addresses=%v err=%v", allowed, err)
	}
}

func TestProtocolHealthCheckerBoundsDNSResolution(t *testing.T) {
	checker := NewProtocolHealthChecker(false)
	checker.timeout = 25 * time.Millisecond
	checker.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	started := time.Now()
	err := checker.Check(context.Background(), "https://agent.example")
	if err == nil {
		t.Fatal("blocked DNS resolution unexpectedly succeeded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("DNS resolution exceeded health-check timeout: %s", elapsed)
	}
}
