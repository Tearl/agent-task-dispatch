package selection

import (
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	secp256k1 "github.com/decred/dcrd/dcrec/secp256k1/v4"
	secpECDSA "github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

var (
	domainTypeHash = keccak([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	nameHash       = keccak([]byte("AgentTaskEscrow"))
	versionHash    = keccak([]byte("1"))
	proofTypeHash  = keccak([]byte("SelectionProof(bytes32 payloadHash)"))
)

type EIP712Signer struct {
	privateKey      *secp256k1.PrivateKey
	domainSeparator []byte
}

func NewEIP712Signer(privateKeyHex, chainID, contractAddress string) (*EIP712Signer, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil || len(raw) != 32 || !validUnsigned(chainID) || !authAddress(contractAddress) {
		return nil, ErrInvalidInput
	}
	privateNumber := new(big.Int).SetBytes(raw)
	if privateNumber.Sign() <= 0 || privateNumber.Cmp(secp256k1.Params().N) >= 0 {
		return nil, ErrInvalidInput
	}
	privateKey := secp256k1.PrivKeyFromBytes(raw)
	if !equalBytes(privateKey.Serialize(), raw) {
		return nil, ErrInvalidInput
	}
	chainNumber, _ := new(big.Int).SetString(chainID, 10)
	domain := make([]byte, 0, 160)
	domain = append(domain, domainTypeHash...)
	domain = append(domain, nameHash...)
	domain = append(domain, versionHash...)
	domain = append(domain, uintWord(chainNumber)...)
	addressWord, err := addressWord(strings.ToLower(contractAddress))
	if err != nil {
		return nil, err
	}
	domain = append(domain, addressWord...)
	return &EIP712Signer{privateKey: privateKey, domainSeparator: keccak(domain)}, nil
}

func (signer *EIP712Signer) Sign(proof Proof) (string, string, string, error) {
	payload, err := encodeProof(proof)
	if err != nil {
		return "", "", "", err
	}
	payloadHash := keccak(payload)
	structure := append(append([]byte{}, proofTypeHash...), payloadHash...)
	digestInput := append([]byte{0x19, 0x01}, signer.domainSeparator...)
	digestInput = append(digestInput, keccak(structure)...)
	digest := keccak(digestInput)
	compact := secpECDSA.SignCompact(signer.privateKey, digest, false)
	if len(compact) != 65 || compact[0] < 27 || compact[0] > 28 {
		return "", "", "", errors.New("selection proof recovery id is not Ethereum-compatible")
	}
	signature := make([]byte, 65)
	copy(signature[0:64], compact[1:65])
	signature[64] = compact[0]
	return hex32(payloadHash), hex32(digest), "0x" + hex.EncodeToString(signature), nil
}

func encodeProof(proof Proof) ([]byte, error) {
	words := make([][]byte, 0, 16)
	for _, value := range []string{proof.TaskID, proof.AssignmentID} {
		word, err := bytes32Word(value)
		if err != nil {
			return nil, err
		}
		words = append(words, word)
	}
	for _, value := range []string{proof.AgentController, proof.Payout} {
		word, err := addressWord(value)
		if err != nil {
			return nil, err
		}
		words = append(words, word)
	}
	for _, value := range []string{proof.OverviewID, proof.AllocationID, proof.QuoteHash, proof.TaskSpecHash} {
		word, err := bytes32Word(value)
		if err != nil {
			return nil, err
		}
		words = append(words, word)
	}
	words = append(words, uint64Word(proof.MatchRevision), uint64Word(proof.PriceVersion))
	for _, value := range []string{proof.OverviewPrice, proof.FormalGrossPrice, proof.OverviewCredit} {
		number, ok := new(big.Int).SetString(value, 10)
		if !ok || number.Sign() < 0 || number.BitLen() > 256 || number.String() != value {
			return nil, ErrInvalidInput
		}
		words = append(words, uintWord(number))
	}
	policy, err := bytes32Word(proof.PolicyHash)
	if err != nil {
		return nil, err
	}
	nonce, err := bytes32Word(proof.Nonce)
	if err != nil {
		return nil, err
	}
	words = append(words, policy, nonce, uint64Word(proof.Deadline))
	encoded := make([]byte, 0, len(words)*32)
	for _, word := range words {
		encoded = append(encoded, word...)
	}
	return encoded, nil
}

func bytes32Word(value string) ([]byte, error) {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return nil, ErrInvalidInput
	}
	word, err := hex.DecodeString(value[2:])
	if err != nil || len(word) != 32 {
		return nil, ErrInvalidInput
	}
	return word, nil
}

func addressWord(value string) ([]byte, error) {
	if !authAddress(value) {
		return nil, ErrInvalidInput
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(value), "0x"))
	if err != nil || len(raw) != 20 {
		return nil, ErrInvalidInput
	}
	return append(make([]byte, 12), raw...), nil
}

func authAddress(value string) bool {
	if value != strings.ToLower(value) || len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func uint64Word(value uint64) []byte { return uintWord(new(big.Int).SetUint64(value)) }

func uintWord(value *big.Int) []byte {
	word := make([]byte, 32)
	value.FillBytes(word)
	return word
}

func keccak(value []byte) []byte {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(value)
	return hasher.Sum(nil)
}

func hex32(value []byte) string { return "0x" + hex.EncodeToString(value) }

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}
