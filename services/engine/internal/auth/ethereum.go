package auth

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// EthereumVerifier validates EIP-191 personal_sign signatures by recovering
// the signer. Signature bytes are never retained or included in errors.
type EthereumVerifier struct{}

func (EthereumVerifier) Verify(message, signature, expectedAddress string) error {
	raw, err := hex.DecodeString(strings.TrimPrefix(signature, "0x"))
	if err != nil || len(raw) != 65 {
		return ErrInvalidSignature
	}
	if raw[64] == 27 || raw[64] == 28 {
		raw[64] -= 27
	}
	if raw[64] > 1 {
		return ErrInvalidSignature
	}
	digest := personalSignHash(message)
	compact := make([]byte, 65)
	compact[0] = 27 + raw[64]
	copy(compact[1:], raw[:64])
	publicKey, _, err := secpECDSA.RecoverCompact(compact, digest)
	if err != nil {
		return ErrInvalidSignature
	}
	actual := ethereumAddress(publicKey)
	if actual != strings.ToLower(expectedAddress) {
		return fmt.Errorf("%w: signer mismatch", ErrInvalidSignature)
	}
	return nil
}

func personalSignHash(message string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte("\x19Ethereum Signed Message:\n" + strconv.Itoa(len([]byte(message)))))
	_, _ = hasher.Write([]byte(message))
	return hasher.Sum(nil)
}

func ethereumAddress(publicKey *secp256k1.PublicKey) string {
	serialized := publicKey.SerializeUncompressed()
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(serialized[1:])
	digest := hasher.Sum(nil)
	return "0x" + hex.EncodeToString(digest[len(digest)-20:])
}
