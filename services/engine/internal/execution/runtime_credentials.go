package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

type RuntimeCredential struct {
	BearerToken        string
	CallbackKey        []byte
	CallbackKeyVersion string
}

// RuntimeCredentialProvider supplies per-Agent transport credentials from the
// process secret provider. Values are never persisted in execution records or
// included in protocol errors.
type RuntimeCredentialProvider struct {
	byAgent map[string]RuntimeCredential
}

func NewRuntimeCredentialProvider(credentials map[string]RuntimeCredential) (*RuntimeCredentialProvider, error) {
	if len(credentials) == 0 {
		return nil, ErrInvalidInput
	}
	stable := make(map[string]RuntimeCredential, len(credentials))
	for agentID, value := range credentials {
		if strings.TrimSpace(agentID) == "" || len(agentID) > 200 || strings.TrimSpace(value.BearerToken) == "" || len(value.BearerToken) > 4_096 || strings.ContainsAny(value.BearerToken, "\r\n") || len(value.CallbackKey) != 32 || strings.TrimSpace(value.CallbackKeyVersion) == "" || len(value.CallbackKeyVersion) > 128 {
			return nil, ErrInvalidInput
		}
		value.CallbackKey = slices.Clone(value.CallbackKey)
		stable[agentID] = value
	}
	return &RuntimeCredentialProvider{byAgent: stable}, nil
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
	credential, ok := provider.byAgent[envelope.AgentID]
	if !ok {
		return errors.New("execution Agent credential is unavailable")
	}
	request.Header.Set("authorization", "Bearer "+credential.BearerToken)
	return nil
}

func (provider *RuntimeCredentialProvider) CallbackKey(_ context.Context, agentID, version string) ([]byte, error) {
	credential, ok := provider.byAgent[agentID]
	if !ok || version != credential.CallbackKeyVersion {
		return nil, ErrInvalidCallback
	}
	return slices.Clone(credential.CallbackKey), nil
}

var _ RequestAuthorizer = (*RuntimeCredentialProvider)(nil)
var _ CallbackKeyProvider = (*RuntimeCredentialProvider)(nil)
