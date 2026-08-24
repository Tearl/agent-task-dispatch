package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxHealthResponseBytes = 4_096

type ProtocolHealthChecker struct {
	allowPrivateNetworks bool
	resolver             *net.Resolver
	timeout              time.Duration
	tlsConfig            *tls.Config
}

type protocolHealthResponse struct {
	Status          string `json:"status"`
	ProtocolVersion string `json:"protocolVersion"`
}

func NewProtocolHealthChecker(allowPrivateNetworks bool) *ProtocolHealthChecker {
	return &ProtocolHealthChecker{allowPrivateNetworks: allowPrivateNetworks, resolver: net.DefaultResolver, timeout: 3 * time.Second}
}

func ValidEndpointURL(raw string) bool {
	value, err := url.Parse(raw)
	if err != nil || value.Scheme != "https" || value.Hostname() == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" || (value.Path != "" && value.Path != "/") || len(raw) > 2_048 {
		return false
	}
	if value.Port() != "" {
		port, portErr := strconv.Atoi(value.Port())
		if portErr != nil || port < 1 || port > 65_535 {
			return false
		}
	}
	return true
}

func (c *ProtocolHealthChecker) Check(ctx context.Context, raw string) error {
	if !ValidEndpointURL(raw) {
		return errors.New("invalid Agent protocol base URL")
	}
	endpoint, _ := url.Parse(raw)
	endpoint.Path = "/health"
	addresses, err := c.resolver.LookupNetIP(ctx, "ip", endpoint.Hostname())
	if err != nil || len(addresses) == 0 {
		return errors.New("Agent health endpoint did not resolve")
	}
	allowed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if c.allowPrivateNetworks || publicAddress(address) {
			allowed = append(allowed, address)
		}
	}
	if len(allowed) == 0 {
		return errors.New("Agent health endpoint resolves to a restricted network")
	}
	dialer := &net.Dialer{Timeout: c.timeout}
	transport := &http.Transport{
		Proxy:           nil,
		TLSClientConfig: c.tlsConfig,
		DialContext: func(dialContext context.Context, network, address string) (net.Conn, error) {
			_, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				return nil, splitErr
			}
			var lastErr error
			for _, target := range allowed {
				connection, dialErr := dialer.DialContext(dialContext, network, net.JoinHostPort(target.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   c.timeout,
		ResponseHeaderTimeout: c.timeout,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   c.timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("Agent health endpoint redirects are not allowed")
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Agent health endpoint returned %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHealthResponseBytes+1))
	if err != nil || len(body) > maxHealthResponseBytes {
		return errors.New("Agent health endpoint response is too large")
	}
	var result protocolHealthResponse
	if err = json.Unmarshal(body, &result); err != nil {
		return errors.New("Agent health endpoint returned invalid JSON")
	}
	if strings.ToLower(result.Status) != HealthHealthy || result.ProtocolVersion != "1" {
		return errors.New("Agent health endpoint is unhealthy or incompatible")
	}
	return nil
}

func publicAddress(address netip.Addr) bool {
	return address.IsValid() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}
