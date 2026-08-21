package credential

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

type recordingStore struct {
	mutations []Mutation
	envelopes []Envelope
}

func (s *recordingStore) Rotate(_ context.Context, mutation Mutation, agentID string, input StoreInput, envelope Envelope) (Metadata, bool, error) {
	s.mutations = append(s.mutations, mutation)
	s.envelopes = append(s.envelopes, envelope)
	return Metadata{AgentID: agentID, Version: len(s.envelopes), AgentAggregateVersion: input.ExpectedVersion + 1, CredentialType: input.CredentialType, Label: input.Label, Fingerprint: envelope.Fingerprint, CreatedAt: mutation.Now}, false, nil
}

type countingEncryptor struct{ calls int }

func (e *countingEncryptor) Seal(context.Context, []byte, []byte) (Envelope, error) {
	e.calls++
	return Envelope{Ciphertext: []byte("ciphertext"), Nonce: make([]byte, 12), WrappedDataKey: bytes.Repeat([]byte{1}, 48), KeyNonce: make([]byte, 12), Algorithm: AlgorithmAES256GCM, KeyWrapAlgorithm: AlgorithmAES256GCM, KeyReference: "test", Fingerprint: "0123456789abcdef0123456789abcdef", SecretDigest: "digest"}, nil
}

func TestAESGCMEncryptorRandomizesCiphertextAndSeparatesIdempotencyDigest(t *testing.T) {
	root := bytes.Repeat([]byte{0x42}, 32)
	encryptor, err := NewAESGCMEncryptor(root, bytes.Repeat([]byte{0x43}, 32), "test-key-v1")
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("credential-secret-never-return")
	aad := []byte("agent-credential:v1:owner:agent:api_key")
	first, err := encryptor.Seal(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptor.Seal(context.Background(), plaintext, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Nonce, second.Nonce) || bytes.Equal(first.Ciphertext, second.Ciphertext) || first.Fingerprint == second.Fingerprint {
		t.Fatalf("credential encryption was not randomized: first=%#v second=%#v", first, second)
	}
	if first.SecretDigest == "" || first.SecretDigest != second.SecretDigest || bytes.Contains(first.Ciphertext, plaintext) {
		t.Fatal("encrypted credential or deterministic blind index is invalid")
	}
	keyBlock, err := aes.NewCipher(deriveKey(root, "agent-credential-encryption-v1"))
	if err != nil {
		t.Fatal(err)
	}
	keyAEAD, err := cipher.NewGCM(keyBlock)
	if err != nil {
		t.Fatal(err)
	}
	keyAAD := append(append([]byte{}, aad...), []byte(":data-key")...)
	dataKey, err := keyAEAD.Open(nil, first.KeyNonce, first.WrappedDataKey, keyAAD)
	if err != nil {
		t.Fatalf("wrapped data key did not authenticate: %v", err)
	}
	dataBlock, err := aes.NewCipher(dataKey)
	if err != nil {
		t.Fatal(err)
	}
	dataAEAD, err := cipher.NewGCM(dataBlock)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := dataAEAD.Open(nil, first.Nonce, first.Ciphertext, aad)
	if err != nil || !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("ciphertext did not authenticate/decrypt with the expected context: %q %v", decrypted, err)
	}
	if _, err = dataAEAD.Open(nil, first.Nonce, first.Ciphertext, []byte("wrong-agent")); err == nil {
		t.Fatal("ciphertext accepted the wrong agent encryption context")
	}
}

func TestCredentialRotationEnforcesRoleValidationAndStableRequestHash(t *testing.T) {
	store := &recordingStore{}
	encryptor, err := NewAESGCMEncryptor(bytes.Repeat([]byte{0x24}, 32), bytes.Repeat([]byte{0x25}, 32), "test-key-v1")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(store, encryptor)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 21, 3, 0, 0, 0, time.UTC) }
	input := RotateInput{CredentialType: TypeAPIKey, Label: "production", Secret: "sk_live_secret", ExpectedVersion: 1}
	for _, session := range []auth.Session{
		{UserID: "publisher", Roles: []string{"publisher"}},
		{UserID: "admin", Roles: []string{"admin"}},
		{UserID: "arbitrator", Roles: []string{"arbitrator"}},
		{UserID: "admin-provider", Roles: []string{"admin", "agent_provider"}},
		{UserID: "arbitrator-provider", Roles: []string{"arbitrator", "agent_provider"}},
	} {
		if _, _, rotateErr := service.Rotate(context.Background(), session, "rotate-1", "agent-1", input); !errors.Is(rotateErr, ErrForbidden) {
			t.Fatalf("%s rotation: %v", session.UserID, rotateErr)
		}
	}
	provider := auth.Session{UserID: "owner", Roles: []string{"agent_provider"}}
	for range 2 {
		metadata, replay, rotateErr := service.Rotate(context.Background(), provider, "rotate-1", "agent-1", input)
		if rotateErr != nil || replay || metadata.Fingerprint == "" {
			t.Fatalf("provider rotation: metadata=%#v replay=%v err=%v", metadata, replay, rotateErr)
		}
	}
	if len(store.mutations) != 2 || store.mutations[0].RequestHash != store.mutations[1].RequestHash {
		t.Fatalf("same secret produced unstable idempotency hashes: %#v", store.mutations)
	}
	if bytes.Equal(store.envelopes[0].Ciphertext, store.envelopes[1].Ciphertext) {
		t.Fatal("retry reused deterministic ciphertext")
	}
	if store.envelopes[0].SecretDigest != "" || store.envelopes[1].SecretDigest != "" {
		t.Fatal("deterministic secret digest crossed into persistence")
	}
}

func TestCredentialRotationRejectsInvalidInputBeforeEncryption(t *testing.T) {
	encryptor := &countingEncryptor{}
	service, err := NewService(&recordingStore{}, encryptor)
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{UserID: "owner", Roles: []string{"agent_provider"}}
	for name, input := range map[string]RotateInput{
		"unknown type": {CredentialType: "password", Label: "prod", Secret: "secret", ExpectedVersion: 1},
		"empty secret": {CredentialType: TypeAPIKey, Label: "prod", ExpectedVersion: 1},
		"empty label":  {CredentialType: TypeAPIKey, Secret: "secret", ExpectedVersion: 1},
		"stale":        {CredentialType: TypeAPIKey, Label: "prod", Secret: "secret"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, rotateErr := service.Rotate(context.Background(), session, "rotate", "agent", input); !errors.Is(rotateErr, ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", rotateErr)
			}
		})
	}
	if encryptor.calls != 0 {
		t.Fatalf("invalid credentials reached encryption: %d", encryptor.calls)
	}
}
