package execution

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type headerAuthorizer struct{}

func (headerAuthorizer) Authorize(_ context.Context, request *http.Request, body []byte) error {
	if len(body) == 0 {
		return ErrInvalidInput
	}
	request.Header.Set("authorization", "HMAC test-signature")
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func protocolResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestHTTPClientUsesVersionedHTTPSProtocolAndAuthorization(t *testing.T) {
	responses := map[string]string{
		"/v1/executions":             `{"accepted":true,"status":"running"}`,
		"/v1/executions/status":      `{"status":"running","usedCost":"7"}`,
		"/v1/executions/cancel":      `{"accepted":true}`,
		"/v1/executions/deliverable": `{"contentHash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","deliverableRef":"agent-artifact://result"}`,
	}
	seen := make(map[string]int)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Scheme != "https" || request.Header.Get("authorization") != "HMAC test-signature" || request.Header.Get("idempotency-key") != "idem-1" || request.Header.Get("x-agent-protocol-version") != ProtocolVersion {
			t.Errorf("request lost HTTPS protocol authentication/bindings: url=%s method=%s headers=%v", request.URL, request.Method, request.Header)
		}
		body, ok := responses[request.URL.Path]
		if !ok {
			return protocolResponse(request, http.StatusNotFound, `{}`), nil
		}
		seen[request.URL.Path]++
		return protocolResponse(request, http.StatusOK, body), nil
	})
	client, err := NewHTTPClient(&http.Client{Transport: transport}, headerAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	envelope := Envelope{ProtocolVersion: ProtocolVersion, IdempotencyKey: "idem-1"}
	if response, callErr := client.Create(context.Background(), "https://agent.example", envelope); callErr != nil || !response.Accepted {
		t.Fatalf("create: response=%#v err=%v", response, callErr)
	}
	if response, callErr := client.Status(context.Background(), "https://agent.example", envelope); callErr != nil || response.UsedCost != "7" {
		t.Fatalf("status: response=%#v err=%v", response, callErr)
	}
	if response, callErr := client.Cancel(context.Background(), "https://agent.example", envelope); callErr != nil || !response.Accepted {
		t.Fatalf("cancel: response=%#v err=%v", response, callErr)
	}
	if response, callErr := client.Deliverable(context.Background(), "https://agent.example", envelope); callErr != nil || response.DeliverableRef == "" {
		t.Fatalf("deliverable: response=%#v err=%v", response, callErr)
	}
	for path := range responses {
		if seen[path] != 1 {
			t.Fatalf("protocol path %q called %d times", path, seen[path])
		}
	}
}

func TestHTTPClientRejectsRedirectAndOversizedResponse(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v1/executions/status":
			response := protocolResponse(request, http.StatusTemporaryRedirect, ``)
			response.Header.Set("location", "https://agent.example/redirected")
			return response, nil
		case "/v1/executions/deliverable":
			return protocolResponse(request, http.StatusOK, strings.Repeat("x", maxProtocolResponseBytes+1)), nil
		default:
			return protocolResponse(request, http.StatusNotFound, `{}`), nil
		}
	})
	client, err := NewHTTPClient(&http.Client{Transport: transport}, headerAuthorizer{})
	if err != nil {
		t.Fatal(err)
	}
	envelope := Envelope{IdempotencyKey: "idem-1"}
	if _, err = client.Status(context.Background(), "https://agent.example", envelope); err == nil || !strings.Contains(strings.ToLower(err.Error()), "redirect") {
		t.Fatalf("redirect was not rejected: %v", err)
	}
	if _, err = client.Deliverable(context.Background(), "https://agent.example", envelope); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized response was not rejected: %v", err)
	}
	if _, err = client.Create(context.Background(), "http://agent.example", envelope); err != ErrInvalidInput {
		t.Fatalf("insecure endpoint accepted: %v", err)
	}
}
