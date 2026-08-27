package execution

import (
	"context"
	"net/http"
	"testing"
)

func TestRuntimeCredentialProviderBindsSecretsToAgentAndVersion(t *testing.T) {
	provider, err := NewRuntimeCredentialProvider(map[string]RuntimeCredential{
		"agent-1": {BearerToken: "transport-secret", CallbackKey: []byte("0123456789abcdef0123456789abcdef"), CallbackKeyVersion: "callback-v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://agent.example/v1/executions", nil)
	if err = provider.Authorize(context.Background(), request, []byte(`{"agentId":"agent-1"}`)); err != nil || request.Header.Get("authorization") != "Bearer transport-secret" {
		t.Fatalf("request authorization failed: header=%q err=%v", request.Header.Get("authorization"), err)
	}
	key, err := provider.CallbackKey(context.Background(), "agent-1", "callback-v1")
	if err != nil || len(key) != 32 {
		t.Fatalf("callback key failed: len=%d err=%v", len(key), err)
	}
	key[0] = 0
	reloaded, _ := provider.CallbackKey(context.Background(), "agent-1", "callback-v1")
	if reloaded[0] == 0 {
		t.Fatal("callback key escaped by reference")
	}
	if _, err = provider.CallbackKey(context.Background(), "agent-1", "callback-v2"); err == nil {
		t.Fatal("wrong callback key version was accepted")
	}
	if agentID, resolveErr := provider.AgentForAuthorization("Bearer transport-secret"); resolveErr != nil || agentID != "agent-1" {
		t.Fatalf("inbound authorization failed: agent=%q err=%v", agentID, resolveErr)
	}
	if _, resolveErr := provider.AgentForAuthorization("Bearer wrong"); resolveErr == nil {
		t.Fatal("invalid inbound authorization was accepted")
	}
}

func TestRuntimeCredentialProviderRejectsUnsafeSecretMetadata(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	values := []RuntimeCredential{
		{BearerToken: "line\nbreak", CallbackKey: key, CallbackKeyVersion: "callback-v1"},
		{BearerToken: "token", CallbackKey: key[:31], CallbackKeyVersion: "callback-v1"},
		{BearerToken: "token", CallbackKey: key, CallbackKeyVersion: ""},
	}
	for _, value := range values {
		if _, err := NewRuntimeCredentialProvider(map[string]RuntimeCredential{"agent-1": value}); err == nil {
			t.Fatal("unsafe runtime credential was accepted")
		}
	}
	if _, err := NewRuntimeCredentialProvider(map[string]RuntimeCredential{
		"agent-1": {BearerToken: "same-token", CallbackKey: key, CallbackKeyVersion: "callback-v1"},
		"agent-2": {BearerToken: "same-token", CallbackKey: key, CallbackKeyVersion: "callback-v1"},
	}); err == nil {
		t.Fatal("duplicate bearer token was accepted")
	}
}

func TestDecodeRuntimeCredentialsJSONIsStrictAndDecodesCallbackKey(t *testing.T) {
	encoded := `{"agent-1":{"bearerToken":"transport-secret","callbackKeyBase64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","callbackKeyVersion":"callback-v1"}}`
	credentials, err := DecodeRuntimeCredentialsJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if credentials["agent-1"].BearerToken != "transport-secret" || len(credentials["agent-1"].CallbackKey) != 32 {
		t.Fatalf("unexpected credentials: %#v", credentials["agent-1"])
	}
	for _, invalid := range []string{
		`{"agent-1":{"bearerToken":"secret","callbackKeyBase64":"bad","callbackKeyVersion":"v1"}}`,
		`{"agent-1":{"bearerToken":"secret","callbackKeyBase64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","callbackKeyVersion":"v1","unknown":true}}`,
		`{} {}`,
	} {
		if _, err = DecodeRuntimeCredentialsJSON(invalid); err == nil {
			t.Fatalf("invalid credentials JSON was accepted")
		}
	}
}

func TestRuntimeCredentialProviderAcceptsProtocolBundleHotUpdates(t *testing.T) {
	provider, err := NewRuntimeCredentialProvider(nil)
	if err != nil {
		t.Fatal(err)
	}
	bundle := []byte(`{"bearerToken":"hot-token","callbackKeyBase64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","callbackKeyVersion":"hot-v1"}`)
	if err = provider.ValidateProtocolBundle("agent-hot", bundle); err != nil {
		t.Fatal(err)
	}
	if err = provider.UpdateProtocolBundle("agent-hot", bundle); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, "https://agent.example/v1/executions", nil)
	if err = provider.Authorize(context.Background(), request, []byte(`{"agentId":"agent-hot"}`)); err != nil || request.Header.Get("authorization") != "Bearer hot-token" {
		t.Fatalf("hot credential was not used: header=%q err=%v", request.Header.Get("authorization"), err)
	}
}
