package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func TestServiceRejectsExpiredWrongBindingAndReplay(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := ethereumAddress(key.PubKey())
	store := NewMemoryStore()
	service, err := NewService(store, EthereumVerifier{}, Config{Domain: "app.example", ChainID: "11155111", Purpose: "login", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := service.Issue(context.Background(), wallet)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(message string) string {
		compact := secpECDSA.SignCompact(key, personalSignHash(message), false)
		signature := append(append([]byte{}, compact[1:]...), compact[0]-27)
		return "0x" + hex.EncodeToString(signature)
	}
	session, err := service.Verify(context.Background(), VerifyRequest{Message: challenge.Message, Signature: sign(challenge.Message)})
	if err != nil || session.Token == "" {
		t.Fatalf("verify: %#v %v", session, err)
	}
	if _, err = service.Verify(context.Background(), VerifyRequest{Message: challenge.Message, Signature: sign(challenge.Message)}); !errors.Is(err, ErrNonceConsumed) {
		t.Fatalf("expected replay rejection, got %v", err)
	}

	wrong := challenge
	wrong.Domain = "evil.example"
	wrong.Message = FormatMessage(wrong)
	if _, err = service.Verify(context.Background(), VerifyRequest{Message: wrong.Message, Signature: sign(wrong.Message)}); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("wrong domain: %v", err)
	}
	wrongChain := challenge
	wrongChain.ChainID = "1"
	wrongChain.Message = FormatMessage(wrongChain)
	if _, err = service.Verify(context.Background(), VerifyRequest{Message: wrongChain.Message, Signature: sign(wrongChain.Message)}); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("wrong chain: %v", err)
	}
	wrongPurpose := challenge
	wrongPurpose.Purpose = "authorize-payment"
	wrongPurpose.Message = FormatMessage(wrongPurpose)
	if _, err = service.Verify(context.Background(), VerifyRequest{Message: wrongPurpose.Message, Signature: sign(wrongPurpose.Message)}); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("wrong purpose: %v", err)
	}
	expired, err := service.Issue(context.Background(), wallet)
	if err != nil {
		t.Fatal(err)
	}
	now = expired.ExpiresAt
	if _, err = service.Verify(context.Background(), VerifyRequest{Message: expired.Message, Signature: sign(expired.Message)}); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("expired: %v", err)
	}
}

func TestEthereumVerifierRejectsWrongSigner(t *testing.T) {
	first, _ := secp256k1.GeneratePrivateKey()
	second, _ := secp256k1.GeneratePrivateKey()
	message := "bound challenge"
	compact := secpECDSA.SignCompact(first, personalSignHash(message), false)
	signature := append(append([]byte{}, compact[1:]...), compact[0]-27)
	err := (EthereumVerifier{}).Verify(message, "0x"+hex.EncodeToString(signature), ethereumAddress(second.PubKey()))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected signer rejection, got %v", err)
	}
}
