package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

type ECDSAProofSigner struct{ privateKey *secp256k1.PrivateKey }

func NewECDSAProofSigner(privateKeyHex string) (*ECDSAProofSigner, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil || len(raw) != 32 {
		return nil, ErrInvalidInput
	}
	number := new(big.Int).SetBytes(raw)
	if number.Sign() <= 0 || number.Cmp(secp256k1.Params().N) >= 0 {
		return nil, ErrInvalidInput
	}
	return &ECDSAProofSigner{privateKey: secp256k1.PrivKeyFromBytes(raw)}, nil
}

func (signer *ECDSAProofSigner) Sign(proof Proof) (string, string, string, error) {
	payload, err := json.Marshal(proof)
	if err != nil {
		return "", "", "", err
	}
	payloadSum := sha256.Sum256(payload)
	digestInput := append([]byte(ProofVersion+"\x00"), payloadSum[:]...)
	digestSum := sha256.Sum256(digestInput)
	compact := secpECDSA.SignCompact(signer.privateKey, digestSum[:], false)
	signature := make([]byte, 65)
	copy(signature[:64], compact[1:])
	signature[64] = compact[0]
	return "sha256:" + hex.EncodeToString(payloadSum[:]), "sha256:" + hex.EncodeToString(digestSum[:]), "0x" + hex.EncodeToString(signature), nil
}
