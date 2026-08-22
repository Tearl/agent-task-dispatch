package selection

import (
	"encoding/hex"
	"strings"
	"testing"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func TestEIP712SignerProducesRecoverableDomainBoundSignature(t *testing.T) {
	signer, err := NewEIP712Signer(testPrivateKey, "31337", "0x0000000000000000000000000000000000001234")
	if err != nil {
		t.Fatal(err)
	}
	proof := proofFixture()
	payloadHash, digest, signature, err := signer.Sign(proof)
	if err != nil {
		t.Fatal(err)
	}
	if len(payloadHash) != 66 || len(digest) != 66 || len(signature) != 132 {
		t.Fatalf("invalid EIP-712 output: payload=%s digest=%s signature=%s", payloadHash, digest, signature)
	}
	if payloadHash != "0x6387ee1a037746da76c6c438c017595e0ceff8aa9fcbf2b62cf566f192afbda9" || digest != "0x460ced8c044a195a0fbbdde762c0e137023dab936e855837c75052b0aedff752" {
		t.Fatalf("EIP-712 compatibility vector changed: payload=%s digest=%s", payloadHash, digest)
	}
	rawSignature, _ := hex.DecodeString(signature[2:])
	rawDigest, _ := hex.DecodeString(digest[2:])
	compact := append([]byte{rawSignature[64]}, rawSignature[:64]...)
	recovered, _, err := secpECDSA.RecoverCompact(compact, rawDigest)
	if err != nil {
		t.Fatal(err)
	}
	privateBytes, _ := hex.DecodeString(testPrivateKey)
	expected := secp256k1.PrivKeyFromBytes(privateBytes).PubKey()
	if !recovered.IsEqual(expected) {
		t.Fatal("signature did not recover the configured platform signer")
	}

	otherChain, _ := NewEIP712Signer(testPrivateKey, "31338", "0x0000000000000000000000000000000000001234")
	_, otherDigest, _, _ := otherChain.Sign(proof)
	otherContract, _ := NewEIP712Signer(testPrivateKey, "31337", "0x0000000000000000000000000000000000001235")
	_, contractDigest, _, _ := otherContract.Sign(proof)
	if otherDigest == digest || contractDigest == digest {
		t.Fatal("chain or contract did not change the EIP-712 digest")
	}
}

func TestEIP712SignerRejectsMalformedProofAndPrivateKey(t *testing.T) {
	if _, err := NewEIP712Signer("00", "31337", "0x0000000000000000000000000000000000001234"); err == nil {
		t.Fatal("short private key was accepted")
	}
	if _, err := NewEIP712Signer(strings.Repeat("0", 64), "31337", "0x0000000000000000000000000000000000001234"); err == nil {
		t.Fatal("zero private key was accepted")
	}
	signer, _ := NewEIP712Signer(testPrivateKey, "31337", "0x0000000000000000000000000000000000001234")
	proof := proofFixture()
	proof.OverviewCredit = "01"
	if _, _, _, err := signer.Sign(proof); err == nil {
		t.Fatal("non-canonical amount was accepted")
	}
}

func proofFixture() Proof {
	return Proof{
		TaskID: bytes32ID("task"), AssignmentID: bytes32ID("assignment"),
		AgentController: "0x000000000000000000000000000000000000beef",
		Payout:          "0x000000000000000000000000000000000000f00d",
		OverviewID:      bytes32ID("overview"), AllocationID: bytes32ID("allocation"),
		QuoteHash: bytes32ID("quote"), TaskSpecHash: bytes32ID("spec"),
		MatchRevision: 1, PriceVersion: 2, OverviewPrice: "10", FormalGrossPrice: "100", OverviewCredit: "10",
		PolicyHash: bytes32ID("policy"), Nonce: bytes32ID("nonce"), Deadline: 1_800_000_000,
	}
}
