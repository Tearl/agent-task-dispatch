package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
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
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "::"} {
		address := netip.MustParseAddr(raw)
		if publicAddress(address) {
			t.Fatalf("restricted address accepted: %s", raw)
		}
	}
	if !publicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
}
