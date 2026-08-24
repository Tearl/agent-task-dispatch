package execution

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net/url"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
)

type Config struct {
	CallbackBaseURL string
	NonceKeyVersion string
	NonceSecret     []byte
	LeaseTTL        time.Duration
}

type Service struct {
	repository Repository
	leaser     CapacityLeaser
	client     Client
	verifier   *CallbackVerifier
	config     Config
	now        func() time.Time
}

func NewService(repository Repository, leaser CapacityLeaser, client Client, verifier *CallbackVerifier, config Config) (*Service, error) {
	callbackURL, err := url.Parse(config.CallbackBaseURL)
	if repository == nil || leaser == nil || client == nil || verifier == nil || err != nil || callbackURL.Scheme != "https" || callbackURL.Host == "" || callbackURL.User != nil || callbackURL.RawQuery != "" || callbackURL.Fragment != "" || config.NonceKeyVersion == "" || len(config.NonceSecret) < 32 || config.LeaseTTL <= 0 || config.LeaseTTL > time.Hour {
		return nil, ErrInvalidInput
	}
	return &Service{repository: repository, leaser: leaser, client: client, verifier: verifier, config: config, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (service *Service) Create(ctx context.Context, spec Spec) (Execution, bool, error) {
	spec.Deadline = spec.Deadline.UTC()
	if err := ValidateSpec(spec, service.now()); err != nil {
		return Execution{}, false, err
	}
	return service.repository.GetOrCreate(ctx, spec)
}

// Get exposes the sanitized domain execution record to trusted Engine
// orchestrators. Transport credentials remain owned by the runtime provider.
func (service *Service) Get(ctx context.Context, logicalExecutionID string) (Execution, error) {
	if logicalExecutionID == "" {
		return Execution{}, ErrInvalidInput
	}
	return service.repository.Get(ctx, logicalExecutionID)
}

func (service *Service) Dispatch(ctx context.Context, logicalExecutionID string) (Execution, Attempt, bool, error) {
	execution, attempt, replay, err := service.repository.PrepareAttempt(ctx, logicalExecutionID, service.config.LeaseTTL)
	if err != nil {
		return Execution{}, Attempt{}, false, err
	}
	lease, err := service.leaser.ReserveCapacity(ctx, execution.Spec.AgentID, attempt.ReservationID, service.config.LeaseTTL)
	if err != nil {
		return execution, attempt, replay, err
	}
	nonce := service.callbackNonce(attempt)
	attempt, err = service.activateAttempt(ctx, execution, attempt, lease, nonce)
	if err != nil {
		_ = service.leaser.ReleaseCapacity(ctx, lease.ReservationID, lease.FencingToken)
		return execution, attempt, replay, err
	}
	envelope, err := service.envelope(execution, attempt, "create", nonce)
	if err != nil {
		return execution, attempt, replay, err
	}
	response, callErr := service.client.Create(ctx, execution.Spec.AgentEndpoint, envelope)
	if recordErr := service.repository.RecordDispatch(ctx, logicalExecutionID, attempt.Number); recordErr != nil && callErr == nil {
		callErr = recordErr
	}
	if callErr != nil {
		// Ambiguous transport failure keeps the same logical/network attempt,
		// fencing token, callback nonce, and idempotency key for safe redelivery.
		return execution, attempt, replay, callErr
	}
	if !response.Accepted || response.Status != ExecutionRunning && response.Status != ExecutionPending {
		// Agent-provided free text is not persisted: it may contain credentials or
		// other untrusted material. Keep only a stable platform-owned reason code.
		reason := "agent_rejected"
		if response.Accepted {
			reason = "invalid_agent_protocol_response"
		}
		if err = service.repository.FailAttempt(ctx, logicalExecutionID, attempt.Number, attempt.FencingToken, reason); err != nil {
			return execution, attempt, replay, err
		}
		_ = service.leaser.ReleaseCapacity(ctx, attempt.ReservationID, attempt.FencingToken)
		return execution, attempt, replay, ErrInvalidState
	}
	updated, err := service.repository.Get(ctx, logicalExecutionID)
	return updated, attempt, replay, err
}

func (service *Service) Poll(ctx context.Context, logicalExecutionID string) (StatusResponse, error) {
	execution, attempt, nonce, envelope, err := service.operationEnvelope(ctx, logicalExecutionID, "status")
	_ = nonce
	if err != nil {
		return StatusResponse{}, err
	}
	response, err := service.client.Status(ctx, execution.Spec.AgentEndpoint, envelope)
	if err != nil {
		return StatusResponse{}, err
	}
	if invalidMoney(response.UsedCost) || response.Status != ExecutionRunning && response.Status != ExecutionSucceeded && response.Status != ExecutionFailed && response.Status != ExecutionCancelled {
		return StatusResponse{}, ErrInvalidInput
	}
	_, shouldStop, err := service.repository.RecordUsage(ctx, logicalExecutionID, attempt.Number, attempt.FencingToken, response.UsedCost)
	if err != nil {
		return StatusResponse{}, err
	}
	if shouldStop {
		_ = service.stopRemote(ctx, execution, attempt)
		return response, ErrCostCapExceeded
	}
	return response, nil
}

func (service *Service) Cancel(ctx context.Context, logicalExecutionID string) (Execution, error) {
	execution, attempt, _, err := service.repository.RequestCancel(ctx, logicalExecutionID)
	if err != nil {
		return Execution{}, err
	}
	if attempt.Number == 0 {
		return service.repository.CompleteCancel(ctx, logicalExecutionID, 0, 0)
	}
	nonce := service.callbackNonce(attempt)
	envelope, err := service.envelope(execution, attempt, "cancel", nonce)
	if err != nil {
		return Execution{}, err
	}
	if execution.Status != ExecutionCancelled {
		response, callErr := service.client.Cancel(ctx, execution.Spec.AgentEndpoint, envelope)
		if callErr != nil {
			return execution, callErr
		}
		if !response.Accepted {
			return execution, ErrInvalidState
		}
	}
	cancelled, err := service.repository.CompleteCancel(ctx, logicalExecutionID, attempt.Number, attempt.FencingToken)
	if err != nil {
		return Execution{}, err
	}
	_ = service.leaser.ReleaseCapacity(ctx, attempt.ReservationID, attempt.FencingToken)
	return cancelled, nil
}

func (service *Service) Deliverable(ctx context.Context, logicalExecutionID string) (DeliverableResponse, error) {
	execution, _, _, envelope, err := service.operationEnvelope(ctx, logicalExecutionID, "deliverable")
	if err != nil {
		return DeliverableResponse{}, err
	}
	if execution.Status != ExecutionSucceeded {
		return DeliverableResponse{}, ErrInvalidState
	}
	response, err := service.client.Deliverable(ctx, execution.Spec.AgentEndpoint, envelope)
	if err != nil {
		return DeliverableResponse{}, err
	}
	if response.ContentHash != execution.ContentHash || response.DeliverableRef != execution.DeliverableRef {
		return DeliverableResponse{}, ErrContentConflict
	}
	return response, nil
}

func (service *Service) HandleCallback(ctx context.Context, callback Callback, signature string) (CallbackResult, error) {
	verified, err := service.verifier.Verify(ctx, callback, signature)
	if err != nil {
		return CallbackResult{}, err
	}
	result, err := service.repository.ApplyCallback(ctx, verified)
	if err != nil {
		return CallbackResult{}, err
	}
	if result.Replay || result.Outcome == CallbackStaleFence || result.Outcome == CallbackLate {
		return result, nil
	}
	attempt, attemptErr := service.repository.CurrentAttempt(ctx, callback.LogicalExecutionID)
	if attemptErr == nil && attempt.AttemptID == callback.AttemptID {
		if result.ShouldCancel {
			execution := result.Execution
			_ = service.stopRemote(ctx, execution, attempt)
		}
		_ = service.leaser.ReleaseCapacity(ctx, attempt.ReservationID, attempt.FencingToken)
	}
	return result, nil
}

func (service *Service) activateAttempt(ctx context.Context, execution Execution, attempt Attempt, lease agent.CapacityLease, nonce string) (Attempt, error) {
	nonceHash := hashValue(nonce)
	_, activated, err := service.repository.ActivateAttempt(ctx, execution.Spec.LogicalExecutionID, attempt.Number, lease, nonceHash, service.config.NonceKeyVersion)
	return activated, err
}

func (service *Service) operationEnvelope(ctx context.Context, logicalExecutionID, operation string) (Execution, Attempt, string, Envelope, error) {
	execution, err := service.repository.Get(ctx, logicalExecutionID)
	if err != nil {
		return Execution{}, Attempt{}, "", Envelope{}, err
	}
	attempt, err := service.repository.CurrentAttempt(ctx, logicalExecutionID)
	if err != nil {
		return Execution{}, Attempt{}, "", Envelope{}, err
	}
	nonce := service.callbackNonce(attempt)
	envelope, err := service.envelope(execution, attempt, operation, nonce)
	return execution, attempt, nonce, envelope, err
}

func (service *Service) envelope(execution Execution, attempt Attempt, operation, nonce string) (Envelope, error) {
	callbackURL, err := url.JoinPath(service.config.CallbackBaseURL, execution.Spec.LogicalExecutionID, attempt.AttemptID)
	if err != nil {
		return Envelope{}, err
	}
	return BuildEnvelope(execution, attempt, operation, callbackURL, nonce)
}

func (service *Service) callbackNonce(attempt Attempt) string {
	mac := hmac.New(sha256.New, service.config.NonceSecret)
	_, _ = mac.Write([]byte(ProtocolVersion))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(attempt.LogicalExecutionID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(attempt.AttemptID))
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], uint64(attempt.Number))
	_, _ = mac.Write(number[:])
	return hex.EncodeToString(mac.Sum(nil))
}

func (service *Service) stopRemote(ctx context.Context, execution Execution, attempt Attempt) error {
	nonce := service.callbackNonce(attempt)
	envelope, err := service.envelope(execution, attempt, "cancel", nonce)
	if err != nil {
		return err
	}
	_, callErr := service.client.Cancel(ctx, execution.Spec.AgentEndpoint, envelope)
	_ = service.leaser.ReleaseCapacity(ctx, attempt.ReservationID, attempt.FencingToken)
	return callErr
}
