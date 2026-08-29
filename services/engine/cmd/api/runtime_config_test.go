package main

import "testing"

func TestDecodeRuntimeCredentials(t *testing.T) {
	encoded := `{"agent-1":{"bearerToken":"transport-secret","callbackKeyBase64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","callbackKeyVersion":"callback-v1"}}`
	credentials, err := decodeRuntimeCredentials(encoded)
	if err != nil {
		t.Fatal(err)
	}
	credential := credentials["agent-1"]
	if credential.BearerToken != "transport-secret" || credential.CallbackKeyVersion != "callback-v1" || len(credential.CallbackKey) != 32 {
		t.Fatalf("unexpected credential metadata: version=%q keyLength=%d", credential.CallbackKeyVersion, len(credential.CallbackKey))
	}
}

func TestDecodeRuntimeCredentialsRejectsUnknownTrailingAndInvalidBase64(t *testing.T) {
	values := []string{
		`{"agent-1":{"bearerToken":"secret","callbackKeyBase64":"bad","callbackKeyVersion":"v1"}}`,
		`{"agent-1":{"bearerToken":"secret","callbackKeyBase64":"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=","callbackKeyVersion":"v1","unknown":true}}`,
		`{} {}`,
	}
	for _, value := range values {
		if _, err := decodeRuntimeCredentials(value); err == nil {
			t.Fatalf("invalid runtime credentials were accepted")
		}
	}
}

func TestLegacyEscrowDeploymentsRequireCompleteDeploymentMetadata(t *testing.T) {
	t.Setenv("ESCROW_LEGACY_DEPLOYMENTS_JSON", `[{"chainId":"84532","contractAddress":"0x1111111111111111111111111111111111111111","assetKey":"evm:84532/native","disputeResolverAddress":"0x2222222222222222222222222222222222222222"}]`)
	values, err := legacyEscrowDeployments()
	if err != nil || len(values) != 1 || values[0].ChainID != "84532" || values[0].Asset != "evm:84532/native" || values[0].ActiveForNewTasks {
		t.Fatalf("legacy deployments=%#v err=%v", values, err)
	}
	t.Setenv("ESCROW_LEGACY_DEPLOYMENTS_JSON", `[{"chainId":"84532"}]`)
	if _, err = legacyEscrowDeployments(); err == nil {
		t.Fatal("incomplete legacy deployment metadata accepted")
	}
}
