package credential

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

var (
	ErrForbidden    = errors.New("credential operation forbidden")
	ErrNotFound     = errors.New("agent credential target not found")
	ErrStaleVersion = errors.New("stale agent aggregate version")
	ErrInvalidState = errors.New("credential rotation is invalid in agent state")
	ErrInvalidInput = errors.New("invalid credential input")
)

const (
	TypeAPIKey            = "api_key"
	TypeBearerToken       = "bearer_token"
	TypeOAuthClientSecret = "oauth_client_secret"
	TypeProtocolBundle    = "protocol_bundle"
	AlgorithmAES256GCM    = "AES-256-GCM"
)

type RotateInput struct {
	CredentialType  string `json:"credentialType"`
	Label           string `json:"label"`
	Secret          string `json:"secret"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type Metadata struct {
	AgentID               string    `json:"agentId"`
	Version               int       `json:"version"`
	AgentAggregateVersion int64     `json:"agentAggregateVersion"`
	CredentialType        string    `json:"credentialType"`
	Label                 string    `json:"label"`
	Fingerprint           string    `json:"fingerprint"`
	CreatedAt             time.Time `json:"createdAt"`
}

type Envelope struct {
	Ciphertext       []byte
	Nonce            []byte
	WrappedDataKey   []byte
	KeyNonce         []byte
	Algorithm        string
	KeyWrapAlgorithm string
	KeyReference     string
	Fingerprint      string
	SecretDigest     string
}

type StoreInput struct {
	CredentialType  string
	Label           string
	ExpectedVersion int64
}

type Encryptor interface {
	Seal(context.Context, []byte, []byte) (Envelope, error)
}

type Decryptor interface {
	Open(context.Context, Envelope, []byte) ([]byte, error)
}

type ProtocolObserver interface {
	ValidateProtocolBundle(string, []byte) error
	UpdateProtocolBundle(string, []byte) error
}

type ProtocolBundleRecord struct {
	AgentID, OwnerID, CredentialType string
	Envelope                         Envelope
}

type ProtocolBundleStore interface {
	CurrentProtocolBundles(context.Context) ([]ProtocolBundleRecord, error)
}

type Mutation struct {
	ActorID        string
	IdempotencyKey string
	RequestHash    string
	EventID        string
	Now            time.Time
}

type Store interface {
	Rotate(context.Context, Mutation, string, StoreInput, Envelope) (Metadata, bool, error)
}

type Service struct {
	store      Store
	encryptor  Encryptor
	now        func() time.Time
	observer   ProtocolObserver
	protocolMu sync.Mutex
}

func (s *Service) SetProtocolObserver(observer ProtocolObserver) { s.observer = observer }

func (s *Service) RestoreProtocolBundles(ctx context.Context) error {
	if s.observer == nil {
		return nil
	}
	store, ok := s.store.(ProtocolBundleStore)
	decryptor, okDecrypt := s.encryptor.(Decryptor)
	if !ok || !okDecrypt {
		return errors.New("protocol credential restore is unavailable")
	}
	records, err := store.CurrentProtocolBundles(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		aad := []byte("agent-credential:v1:" + record.OwnerID + ":" + record.AgentID + ":" + record.CredentialType)
		plaintext, openErr := decryptor.Open(ctx, record.Envelope, aad)
		if openErr != nil {
			return openErr
		}
		updateErr := s.observer.UpdateProtocolBundle(record.AgentID, plaintext)
		clear(plaintext)
		if updateErr != nil {
			return updateErr
		}
	}
	return nil
}

func NewService(store Store, encryptor Encryptor) (*Service, error) {
	if store == nil || encryptor == nil {
		return nil, errors.New("credential store and encryptor are required")
	}
	return &Service{store: store, encryptor: encryptor, now: time.Now}, nil
}

func (s *Service) Rotate(ctx context.Context, session auth.Session, idempotencyKey, agentID string, input RotateInput) (Metadata, bool, error) {
	if !CanRotate(session) {
		return Metadata{}, false, ErrForbidden
	}
	if idempotencyKey == "" || len(idempotencyKey) > 200 || agentID == "" || input.ExpectedVersion < 1 || !slices.Contains([]string{TypeAPIKey, TypeBearerToken, TypeOAuthClientSecret, TypeProtocolBundle}, input.CredentialType) || strings.TrimSpace(input.Label) == "" || len(input.Label) > 100 || input.Secret == "" || len(input.Secret) > 16_384 {
		return Metadata{}, false, ErrInvalidInput
	}
	if input.CredentialType == TypeProtocolBundle {
		s.protocolMu.Lock()
		defer s.protocolMu.Unlock()
		if s.observer == nil || s.observer.ValidateProtocolBundle(agentID, []byte(input.Secret)) != nil {
			return Metadata{}, false, ErrInvalidInput
		}
	}
	aad := []byte("agent-credential:v1:" + session.UserID + ":" + agentID + ":" + input.CredentialType)
	envelope, err := s.encryptor.Seal(ctx, []byte(input.Secret), aad)
	if err != nil {
		return Metadata{}, false, err
	}
	requestBody, err := json.Marshal(struct {
		CredentialType  string `json:"credentialType"`
		Label           string `json:"label"`
		SecretDigest    string `json:"secretDigest"`
		ExpectedVersion int64  `json:"expectedVersion"`
	}{input.CredentialType, input.Label, envelope.SecretDigest, input.ExpectedVersion})
	if err != nil {
		return Metadata{}, false, err
	}
	requestHash := sha256.Sum256(requestBody)
	// The deterministic digest is needed only for idempotency hashing. Remove it
	// before crossing into persistence so the store sees encrypted material only.
	envelope.SecretDigest = ""
	eventID, err := randomID("event")
	if err != nil {
		return Metadata{}, false, err
	}
	mutation := Mutation{ActorID: session.UserID, IdempotencyKey: idempotencyKey, RequestHash: hex.EncodeToString(requestHash[:]), EventID: eventID, Now: s.now().UTC()}
	metadata, replay, err := s.store.Rotate(ctx, mutation, agentID, StoreInput{CredentialType: input.CredentialType, Label: input.Label, ExpectedVersion: input.ExpectedVersion}, envelope)
	if err == nil && input.CredentialType == TypeProtocolBundle {
		err = s.observer.UpdateProtocolBundle(agentID, []byte(input.Secret))
	}
	return metadata, replay, err
}

func CanRotate(session auth.Session) bool {
	return slices.Contains(session.Roles, "agent_provider") && !slices.Contains(session.Roles, "admin") && !slices.Contains(session.Roles, "arbitrator")
}

type AESGCMEncryptor struct {
	keyAEAD       cipher.AEAD
	blindIndexKey []byte
	keyReference  string
}

func NewAESGCMEncryptor(rootKey, idempotencyKey []byte, keyReference string) (*AESGCMEncryptor, error) {
	if len(rootKey) != 32 || len(idempotencyKey) != 32 || strings.TrimSpace(keyReference) == "" || len(keyReference) > 200 {
		return nil, errors.New("32-byte credential root and idempotency keys plus key reference are required")
	}
	encryptionKey := deriveKey(rootKey, "agent-credential-encryption-v1")
	blindIndexKey := deriveKey(idempotencyKey, "agent-credential-idempotency-v1")
	block, err := aes.NewCipher(encryptionKey)
	if err != nil {
		return nil, err
	}
	keyAEAD, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCMEncryptor{keyAEAD: keyAEAD, blindIndexKey: blindIndexKey, keyReference: keyReference}, nil
}

func (e *AESGCMEncryptor) Seal(_ context.Context, plaintext, aad []byte) (Envelope, error) {
	if len(plaintext) == 0 {
		return Envelope{}, ErrInvalidInput
	}
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return Envelope{}, err
	}
	defer clear(dataKey)
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return Envelope{}, err
	}
	dataAEAD, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, dataAEAD.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	ciphertext := dataAEAD.Seal(nil, nonce, plaintext, aad)
	keyNonce := make([]byte, e.keyAEAD.NonceSize())
	if _, err := rand.Read(keyNonce); err != nil {
		return Envelope{}, err
	}
	keyAAD := append(append([]byte{}, aad...), []byte(":data-key")...)
	wrappedDataKey := e.keyAEAD.Seal(nil, keyNonce, dataKey, keyAAD)
	fingerprintHash := sha256.Sum256(append(append([]byte{}, nonce...), ciphertext...))
	digest := hmac.New(sha256.New, e.blindIndexKey)
	_, _ = digest.Write(plaintext)
	return Envelope{
		Ciphertext:       ciphertext,
		Nonce:            nonce,
		WrappedDataKey:   wrappedDataKey,
		KeyNonce:         keyNonce,
		Algorithm:        AlgorithmAES256GCM,
		KeyWrapAlgorithm: AlgorithmAES256GCM,
		KeyReference:     e.keyReference,
		Fingerprint:      hex.EncodeToString(fingerprintHash[:16]),
		SecretDigest:     hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func (e *AESGCMEncryptor) Open(_ context.Context, envelope Envelope, aad []byte) ([]byte, error) {
	if e == nil || envelope.Algorithm != AlgorithmAES256GCM || envelope.KeyWrapAlgorithm != AlgorithmAES256GCM || envelope.KeyReference != e.keyReference {
		return nil, ErrInvalidInput
	}
	keyAAD := append(append([]byte{}, aad...), []byte(":data-key")...)
	dataKey, err := e.keyAEAD.Open(nil, envelope.KeyNonce, envelope.WrappedDataKey, keyAAD)
	if err != nil {
		return nil, ErrInvalidInput
	}
	defer clear(dataKey)
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	dataAEAD, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := dataAEAD.Open(nil, envelope.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return plaintext, nil
}

func deriveKey(root []byte, purpose string) []byte {
	digest := hmac.New(sha256.New, root)
	_, _ = digest.Write([]byte(purpose))
	return digest.Sum(nil)
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value), nil
}
