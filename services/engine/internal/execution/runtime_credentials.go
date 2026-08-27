package execution

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
)

type RuntimeCredential struct {
	BearerToken        string
	CallbackKey        []byte
	CallbackKeyVersion string
}

type runtimeCredentialJSON struct {
	BearerToken        string `json:"bearerToken"`
	CallbackKeyBase64  string `json:"callbackKeyBase64"`
	CallbackKeyVersion string `json:"callbackKeyVersion"`
}

func DecodeRuntimeCredentialsJSON(encoded string) (map[string]RuntimeCredential, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	var values map[string]runtimeCredentialJSON
	if err := decoder.Decode(&values); err != nil {
		return nil, errors.New("invalid Agent runtime credentials JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("Agent runtime credentials JSON contains trailing data")
	}
	credentials := make(map[string]RuntimeCredential, len(values))
	for agentID, value := range values {
		key, err := base64.StdEncoding.DecodeString(value.CallbackKeyBase64)
		if err != nil {
			return nil, errors.New("Agent runtime callback key is not valid base64")
		}
		credentials[agentID] = RuntimeCredential{BearerToken: value.BearerToken, CallbackKey: key, CallbackKeyVersion: value.CallbackKeyVersion}
	}
	return credentials, nil
}

// RuntimeCredentialProvider supplies per-Agent transport credentials from the
// process secret provider. Values are never persisted in execution records or
// included in protocol errors.
type RuntimeCredentialProvider struct {
	mu      sync.RWMutex
	byAgent map[string]RuntimeCredential
}

func NewRuntimeCredentialProvider(credentials map[string]RuntimeCredential) (*RuntimeCredentialProvider, error) {
	stable := make(map[string]RuntimeCredential, len(credentials))
	seenTokens := make(map[string]struct{}, len(credentials))
	for agentID, value := range credentials {
		if strings.TrimSpace(agentID) == "" || len(agentID) > 200 || strings.TrimSpace(value.BearerToken) == "" || len(value.BearerToken) > 4_096 || strings.ContainsAny(value.BearerToken, "\r\n") || len(value.CallbackKey) != 32 || strings.TrimSpace(value.CallbackKeyVersion) == "" || len(value.CallbackKeyVersion) > 128 {
			return nil, ErrInvalidInput
		}
		if _, duplicate := seenTokens[value.BearerToken]; duplicate {
			return nil, ErrInvalidInput
		}
		seenTokens[value.BearerToken] = struct{}{}
		value.CallbackKey = slices.Clone(value.CallbackKey)
		stable[agentID] = value
	}
	return &RuntimeCredentialProvider{byAgent: stable}, nil
}

func (provider *RuntimeCredentialProvider) ValidateProtocolBundle(agentID string, value []byte) error {
	credential, err := decodeProtocolBundle(value)
	if err != nil || strings.TrimSpace(agentID) == "" {
		return ErrInvalidInput
	}
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	for currentID, current := range provider.byAgent {
		if currentID != agentID && current.BearerToken == credential.BearerToken {
			return ErrInvalidInput
		}
	}
	return nil
}

func (provider *RuntimeCredentialProvider) UpdateProtocolBundle(agentID string, value []byte) error {
	credential, err := decodeProtocolBundle(value)
	if err != nil || strings.TrimSpace(agentID) == "" {
		return ErrInvalidInput
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	for currentID, current := range provider.byAgent {
		if currentID != agentID && current.BearerToken == credential.BearerToken {
			return ErrInvalidInput
		}
	}
	credential.CallbackKey = slices.Clone(credential.CallbackKey)
	provider.byAgent[agentID] = credential
	return nil
}

func decodeProtocolBundle(value []byte) (RuntimeCredential, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var raw runtimeCredentialJSON
	if err := decoder.Decode(&raw); err != nil {
		return RuntimeCredential{}, ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return RuntimeCredential{}, ErrInvalidInput
	}
	key, err := base64.StdEncoding.DecodeString(raw.CallbackKeyBase64)
	credential := RuntimeCredential{BearerToken: raw.BearerToken, CallbackKey: key, CallbackKeyVersion: raw.CallbackKeyVersion}
	if err != nil || strings.TrimSpace(credential.BearerToken) == "" || len(credential.BearerToken) > 4096 || strings.ContainsAny(credential.BearerToken, "\r\n") || len(credential.CallbackKey) != 32 || strings.TrimSpace(credential.CallbackKeyVersion) == "" || len(credential.CallbackKeyVersion) > 128 {
		return RuntimeCredential{}, ErrInvalidInput
	}
	return credential, nil
}

// AgentForAuthorization resolves an inbound Agent bearer credential without
// exposing secret material to callers. Runtime credentials are deliberately
// kept outside PostgreSQL.
func (provider *RuntimeCredentialProvider) AgentForAuthorization(header string) (string, error) {
	const prefix = "Bearer "
	if provider == nil || !strings.HasPrefix(header, prefix) {
		return "", ErrInvalidCallback
	}
	token := strings.TrimPrefix(header, prefix)
	if token == "" {
		return "", ErrInvalidCallback
	}
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	for agentID, credential := range provider.byAgent {
		if len(token) == len(credential.BearerToken) && subtle.ConstantTimeCompare([]byte(token), []byte(credential.BearerToken)) == 1 {
			return agentID, nil
		}
	}
	return "", ErrInvalidCallback
}

func (provider *RuntimeCredentialProvider) AuthorizeAgent(request *http.Request, agentID string) error {
	if request == nil {
		return ErrInvalidInput
	}
	provider.mu.RLock()
	credential, ok := provider.byAgent[agentID]
	provider.mu.RUnlock()
	if !ok {
		return errors.New("execution Agent credential is unavailable")
	}
	request.Header.Set("authorization", "Bearer "+credential.BearerToken)
	return nil
}

func (provider *RuntimeCredentialProvider) Authorize(_ context.Context, request *http.Request, body []byte) error {
	if request == nil || len(body) == 0 {
		return ErrInvalidInput
	}
	var envelope struct {
		AgentID string `json:"agentId"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return errors.New("execution envelope cannot be authorized")
	}
	provider.mu.RLock()
	credential, ok := provider.byAgent[envelope.AgentID]
	provider.mu.RUnlock()
	if !ok {
		return errors.New("execution Agent credential is unavailable")
	}
	request.Header.Set("authorization", "Bearer "+credential.BearerToken)
	return nil
}

func (provider *RuntimeCredentialProvider) CallbackKey(_ context.Context, agentID, version string) ([]byte, error) {
	provider.mu.RLock()
	credential, ok := provider.byAgent[agentID]
	provider.mu.RUnlock()
	if !ok || version != credential.CallbackKeyVersion {
		return nil, ErrInvalidCallback
	}
	return slices.Clone(credential.CallbackKey), nil
}

var _ RequestAuthorizer = (*RuntimeCredentialProvider)(nil)
var _ CallbackKeyProvider = (*RuntimeCredentialProvider)(nil)
