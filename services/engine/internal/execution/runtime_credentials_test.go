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
}
