package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"time"
)

const DefaultCallbackClockSkew = 5 * time.Minute

type CallbackKeyProvider interface {
	CallbackKey(context.Context, string, string) ([]byte, error)
}

type CallbackVerifier struct {
	keys    CallbackKeyProvider
	maxSkew time.Duration
	now     func() time.Time
}

func NewCallbackVerifier(keys CallbackKeyProvider, maxSkew time.Duration) (*CallbackVerifier, error) {
	if keys == nil || maxSkew <= 0 || maxSkew > 15*time.Minute {
		return nil, ErrInvalidInput
	}
	return &CallbackVerifier{keys: keys, maxSkew: maxSkew, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (verifier *CallbackVerifier) Verify(ctx context.Context, callback Callback, signature string) (VerifiedCallback, error) {
	if err := validateCallback(callback, verifier.now(), verifier.maxSkew); err != nil {
		return VerifiedCallback{}, err
	}
	key, err := verifier.keys.CallbackKey(ctx, callback.AgentID, callback.KeyVersion)
	if err != nil || len(key) < 32 {
		return VerifiedCallback{}, ErrInvalidCallback
	}
	payload, err := callbackPayload(callback)
	if err != nil {
		return VerifiedCallback{}, ErrInvalidCallback
	}
	expected := hmac.New(sha256.New, key)
	_, _ = expected.Write(payload)
	provided, err := decodeSignature(signature)
	if err != nil || !hmac.Equal(provided, expected.Sum(nil)) {
		return VerifiedCallback{}, ErrInvalidCallback
	}
	nonceDigest := sha256.Sum256([]byte(callback.Nonce))
	payloadDigest := sha256.Sum256(payload)
	return VerifiedCallback{
		Callback:    callback,
		NonceHash:   "sha256:" + hex.EncodeToString(nonceDigest[:]),
		PayloadHash: "sha256:" + hex.EncodeToString(payloadDigest[:]),
	}, nil
}

// SignCallback is the protocol reference implementation for Agent SDKs and
// deterministic contract tests. Callers must never log key or signature.
func SignCallback(callback Callback, key []byte) (string, error) {
	if len(key) < 32 {
		return "", ErrInvalidInput
	}
	payload, err := callbackPayload(callback)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return "hmac-sha256=" + hex.EncodeToString(mac.Sum(nil)), nil
}

func ValidateSpec(spec Spec, now time.Time) error {
	if strings.TrimSpace(spec.LogicalExecutionID) == "" || strings.TrimSpace(spec.TaskID) == "" || !validDigest(spec.TaskSpecHash) || strings.TrimSpace(spec.AgentID) == "" || strings.TrimSpace(spec.ResponsibilityCode) == "" || strings.TrimSpace(spec.IdempotencyKey) == "" {
		return ErrInvalidInput
	}
	endpoint, err := url.Parse(spec.AgentEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return ErrInvalidInput
	}
	if invalidMoney(spec.CostCap) || !spec.Deadline.After(now) || len(spec.IdempotencyKey) > 200 {
		return ErrInvalidInput
	}
	if spec.ToolPolicy.Mode != ToolPolicyReadOnly && spec.ToolPolicy.Mode != ToolPolicyScoped {
		return ErrInvalidInput
	}
	if len(spec.ToolPolicy.AllowedTools) > 50 || hasBlankOrDuplicate(spec.ToolPolicy.AllowedTools) {
		return ErrInvalidInput
	}
	if spec.Stage == StageOverview {
		if spec.Overview == nil || spec.Formal != nil || spec.ToolPolicy.Mode != ToolPolicyReadOnly || spec.Overview.MatchRevision < 1 || strings.TrimSpace(spec.Overview.AllocationID) == "" || !validDigest(spec.Overview.QuoteHash) {
			return ErrInvalidInput
		}
		return nil
	}
	if spec.Stage == StageFormal {
		if spec.Formal == nil || spec.Overview != nil || strings.TrimSpace(spec.Formal.AssignmentID) == "" || strings.TrimSpace(spec.Formal.Package) == "" || spec.Formal.Version < 1 || spec.Formal.AggregateVersion < 1 || spec.Formal.WorkNonce < 1 {
			return ErrInvalidInput
		}
		return nil
	}
	return ErrInvalidInput
}

func BuildEnvelope(execution Execution, attempt Attempt, operation, callbackURL, callbackNonce string) (Envelope, error) {
	if attempt.LogicalExecutionID != execution.Spec.LogicalExecutionID || attempt.FencingToken < 1 || attempt.AttemptID == "" || callbackNonce == "" {
		return Envelope{}, ErrInvalidInput
	}
	parsedCallback, err := url.Parse(callbackURL)
	if err != nil || parsedCallback.Scheme != "https" || parsedCallback.Host == "" || parsedCallback.User != nil || parsedCallback.Fragment != "" {
		return Envelope{}, ErrInvalidInput
	}
	if operation != "create" && operation != "status" && operation != "cancel" && operation != "deliverable" {
		return Envelope{}, ErrInvalidInput
	}
	spec := execution.Spec
	return Envelope{
		ProtocolVersion:    ProtocolVersion,
		Operation:          operation,
		Stage:              spec.Stage,
		LogicalExecutionID: spec.LogicalExecutionID,
		AttemptID:          attempt.AttemptID,
		AgentID:            spec.AgentID,
		TaskID:             spec.TaskID,
		TaskSpecHash:       spec.TaskSpecHash,
		ResponsibilityCode: spec.ResponsibilityCode,
		CostCap:            spec.CostCap,
		ToolPolicy:         spec.ToolPolicy,
		Deadline:           spec.Deadline,
		IdempotencyKey:     spec.IdempotencyKey,
		CallbackURL:        callbackURL,
		CallbackNonce:      callbackNonce,
		FencingToken:       attempt.FencingToken,
		Overview:           spec.Overview,
		Formal:             spec.Formal,
	}, nil
}

func validateCallback(callback Callback, now time.Time, maxSkew time.Duration) error {
	if callback.ProtocolVersion != ProtocolVersion || strings.TrimSpace(callback.LogicalExecutionID) == "" || strings.TrimSpace(callback.AttemptID) == "" || strings.TrimSpace(callback.AgentID) == "" || callback.FencingToken < 1 || callback.Nonce == "" || len(callback.Nonce) > 256 || callback.KeyVersion == "" || len(callback.KeyVersion) > 128 || invalidMoney(callback.UsedCost) {
		return ErrInvalidCallback
	}
	if callback.Timestamp.Before(now.Add(-maxSkew)) || callback.Timestamp.After(now.Add(maxSkew)) {
		return ErrInvalidCallback
	}
	if callback.Status != CallbackSucceeded && callback.Status != CallbackFailed {
		return ErrInvalidCallback
	}
	if callback.Status == CallbackSucceeded && (!validDigest(callback.ContentHash) || strings.TrimSpace(callback.DeliverableRef) == "") {
		return ErrInvalidCallback
	}
	if callback.Status != CallbackSucceeded && (callback.ContentHash != "" || callback.DeliverableRef != "") {
		return ErrInvalidCallback
	}
	return nil
}

func callbackPayload(callback Callback) ([]byte, error) {
	return json.Marshal(callback)
}

func decodeSignature(value string) ([]byte, error) {
	const prefix = "hmac-sha256="
	if !strings.HasPrefix(value, prefix) {
		return nil, ErrInvalidCallback
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(decoded) != sha256.Size {
		return nil, ErrInvalidCallback
	}
	return decoded, nil
}

func invalidMoney(value string) bool {
	if value == "" || len(value) > 78 || value != "0" && strings.HasPrefix(value, "0") {
		return true
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return true
		}
	}
	_, ok := new(big.Int).SetString(value, 10)
	return !ok
}

func compareMoney(left, right string) int {
	leftValue, _ := new(big.Int).SetString(left, 10)
	rightValue, _ := new(big.Int).SetString(right, 10)
	return leftValue.Cmp(rightValue)
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func hasBlankOrDuplicate(values []string) bool {
	stable := slices.Clone(values)
	slices.Sort(stable)
	for index, value := range stable {
		if strings.TrimSpace(value) == "" || index > 0 && value == stable[index-1] {
			return true
		}
	}
	return false
}

func hashValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", digest)
}
