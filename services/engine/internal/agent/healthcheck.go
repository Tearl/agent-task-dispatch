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

var publicIPv6Network = netip.MustParsePrefix("2000::/3")

var restrictedNetworkPrefixes = []netip.Prefix{
	// IPv4 special-purpose, private, loopback, link-local, documentation,
	// benchmarking, multicast, and reserved ranges.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.31.196.0/24"),
	netip.MustParsePrefix("192.52.193.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.175.48.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// IPv6 unspecified/loopback, translation/discard, IETF special-purpose,
	// documentation, 6to4, segment-routing, private, link-local and multicast.
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::ffff:0:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2620:4f:8000::/48"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

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
	probeContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	endpoint, _ := url.Parse(raw)
	endpoint.Path = "/health"
	addresses, err := c.resolver.LookupNetIP(probeContext, "ip", endpoint.Hostname())
	if err != nil || len(addresses) == 0 {
		return errors.New("Agent health endpoint did not resolve")
	}
	allowed, err := validateHealthAddresses(addresses, c.allowPrivateNetworks)
	if err != nil {
		return err
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
	request, err := http.NewRequestWithContext(probeContext, http.MethodGet, endpoint.String(), nil)
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
	if !address.IsValid() {
		return false
	}
	if address.Is4In6() {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	// netip considers reserved IPv6 space global-unicast. Limit IPv6 targets
	// to the IANA global-unicast allocation before applying the special-use
	// exclusions below.
	if address.Is6() && !publicIPv6Network.Contains(address) {
		return false
	}
	for _, prefix := range restrictedNetworkPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func validateHealthAddresses(addresses []netip.Addr, allowPrivateNetworks bool) ([]netip.Addr, error) {
	if len(addresses) == 0 {
		return nil, errors.New("Agent health endpoint did not resolve")
	}
	allowed := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if !allowPrivateNetworks && !publicAddress(address) {
			return nil, errors.New("Agent health endpoint resolves to a restricted network")
		}
		allowed = append(allowed, address.Unmap())
	}
	return allowed, nil
}
