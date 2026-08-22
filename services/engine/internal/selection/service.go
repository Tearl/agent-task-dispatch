package selection

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/auth"
)

type Config struct {
	ChainID         string
	ContractAddress string
	ReservationTTL  time.Duration
}

type Service struct {
	repository Repository
	capacity   CapacityGateway
	signer     ProofSigner
	chain      ChainVerifier
	config     Config
	now        func() time.Time
}

func NewService(repository Repository, capacity CapacityGateway, signer ProofSigner, chain ChainVerifier, config Config) (*Service, error) {
	if repository == nil || capacity == nil || signer == nil || chain == nil || !validUnsigned(config.ChainID) || !auth.IsWalletAddress(strings.ToLower(config.ContractAddress)) || config.ReservationTTL <= 0 || config.ReservationTTL > time.Hour {
		return nil, ErrInvalidInput
	}
	config.ContractAddress = strings.ToLower(config.ContractAddress)
	return &Service{repository: repository, capacity: capacity, signer: signer, chain: chain, config: config, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (service *Service) Reserve(ctx context.Context, session auth.Session, key, taskID string, request Request) (Intent, bool, error) {
	if !publisherAuthorized(session) {
		return Intent{}, false, ErrForbidden
	}
	if taskID == "" || !validDigest(request.BatchID) || !validDigest(request.SlotID) || key == "" || len(key) > 200 {
		return Intent{}, false, ErrInvalidInput
	}
	requestHash, err := hashJSON(struct {
		TaskID  string  `json:"taskId"`
		Request Request `json:"request"`
	}{taskID, request})
	if err != nil {
		return Intent{}, false, err
	}
	if existing, replay, replayErr := service.repository.Replay(ctx, session.UserID, key, requestHash); replayErr != nil {
		return Intent{}, false, replayErr
	} else if replay {
		return service.intent(existing, true)
	}

	eligible, err := service.repository.Eligibility(ctx, session.UserID, taskID, request.BatchID, request.SlotID)
	if err != nil {
		return Intent{}, false, err
	}
	now := service.now().UTC()
	deadline := now.Add(service.config.ReservationTTL)
	if eligible.TaskDeadline.Before(deadline) {
		deadline = eligible.TaskDeadline
	}
	if !deadline.After(now) {
		return Intent{}, false, ErrInvalidState
	}
	reservationID := stableDigest("selection-reservation", session.UserID, key, requestHash)
	assignmentID := bytes32ID("assignment", reservationID)
	nonce := bytes32ID("selection-nonce", reservationID)
	formalPayable, ok := subtract(eligible.FormalGrossPrice, eligible.OverviewPrice)
	if !ok {
		return Intent{}, false, ErrContentConflict
	}
	proof := Proof{
		TaskID: TaskChainID(taskID), AssignmentID: assignmentID,
		AgentController: strings.ToLower(eligible.AgentController), Payout: strings.ToLower(eligible.Payout),
		OverviewID: digestToBytes32(eligible.SlotID), AllocationID: digestToBytes32(eligible.AllocationID),
		QuoteHash: digestToBytes32(eligible.QuoteHash), TaskSpecHash: digestToBytes32(eligible.TaskSpecHash),
		MatchRevision: eligible.MatchRevision, PriceVersion: eligible.PriceVersion,
		OverviewPrice: eligible.OverviewPrice, FormalGrossPrice: eligible.FormalGrossPrice,
		OverviewCredit: eligible.OverviewPrice, PolicyHash: digestToBytes32(eligible.PolicyHash),
		Nonce: nonce, Deadline: uint64(deadline.Unix()),
	}
	payloadHash, proofDigest, _, err := service.signer.Sign(proof)
	if err != nil {
		return Intent{}, false, err
	}
	lease, err := service.capacity.ReserveCapacity(ctx, eligible.AgentID, reservationID, deadline.Sub(now))
	if err != nil {
		if errors.Is(err, agent.ErrCapacityUnavailable) {
			return Intent{}, false, ErrCapacityUnavailable
		}
		return Intent{}, false, err
	}
	reservation := Reservation{
		ID: reservationID, PublisherID: session.UserID, PublisherWallet: strings.ToLower(session.Wallet), TaskID: taskID,
		BatchID: eligible.BatchID, SlotID: eligible.SlotID, SnapshotID: eligible.SnapshotID,
		AgentID: eligible.AgentID, ProviderID: eligible.ProviderID, ChainID: service.config.ChainID,
		ContractAddress: service.config.ContractAddress, Proof: proof, ProofPayloadHash: payloadHash,
		ProofDigest: proofDigest, FormalPayable: formalPayable, CapacityFencingToken: lease.FencingToken,
		CapacityExpiresAt: lease.ExpiresAt, Status: StatusReserved, CreatedAt: now, UpdatedAt: now,
	}
	mutation := Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash, Now: now}
	stored, replay, err := service.repository.Prepare(ctx, mutation, reservation)
	if err != nil {
		_ = service.capacity.ReleaseCapacity(ctx, reservationID, lease.FencingToken)
		return Intent{}, false, err
	}
	return service.intent(stored, replay)
}

func (service *Service) Get(ctx context.Context, session auth.Session, taskID, reservationID string) (Intent, error) {
	if !publisherAuthorized(session) {
		return Intent{}, ErrForbidden
	}
	if taskID == "" || reservationID == "" {
		return Intent{}, ErrInvalidInput
	}
	reservation, err := service.repository.Get(ctx, session.UserID, reservationID)
	if err != nil {
		return Intent{}, err
	}
	if reservation.TaskID != taskID {
		return Intent{}, ErrNotFound
	}
	intent, _, err := service.intent(reservation, false)
	return intent, err
}

func (service *Service) Reconcile(ctx context.Context, session auth.Session, taskID, reservationID string, request ReconcileRequest) (Reservation, *Assignment, error) {
	if !publisherAuthorized(session) {
		return Reservation{}, nil, ErrForbidden
	}
	if taskID == "" || reservationID == "" || !validTransactionHash(request.TransactionHash) {
		return Reservation{}, nil, ErrInvalidInput
	}
	reservation, err := service.repository.Get(ctx, session.UserID, reservationID)
	if err != nil {
		return Reservation{}, nil, err
	}
	if reservation.TaskID != taskID {
		return Reservation{}, nil, ErrNotFound
	}
	result, err := service.chain.VerifySelection(ctx, strings.ToLower(request.TransactionHash))
	if err != nil {
		if errors.Is(err, ErrDependencyPending) {
			if _, recordErr := service.repository.RecordSubmitted(ctx, reservation.ID, strings.ToLower(request.TransactionHash)); recordErr != nil {
				return Reservation{}, nil, recordErr
			}
		}
		return Reservation{}, nil, err
	}
	if result.TransactionHash != "" && strings.ToLower(result.TransactionHash) != strings.ToLower(request.TransactionHash) {
		return Reservation{}, nil, ErrProofMismatch
	}
	switch result.Status {
	case ChainPending:
		updated, recordErr := service.repository.RecordSubmitted(ctx, reservation.ID, strings.ToLower(request.TransactionHash))
		return updated, nil, recordErr
	case ChainFailed:
		if err = service.capacity.ReleaseCapacity(ctx, reservation.ID, reservation.CapacityFencingToken); err != nil && !errors.Is(err, agent.ErrNotFound) {
			return Reservation{}, nil, err
		}
		updated, _, failErr := service.repository.Fail(ctx, reservation.ID, strings.ToLower(request.TransactionHash), stableReason(result.FailureReasonCode, "chain_transaction_failed"))
		return updated, nil, failErr
	case ChainConfirmed:
		if !sameProof(reservation.Proof, result.Proof) || result.FormalPayable != reservation.FormalPayable || result.WorkNonce != 1 {
			return Reservation{}, nil, ErrProofMismatch
		}
		updated, assignment, _, confirmErr := service.repository.Confirm(ctx, reservation.ID, result)
		if confirmErr != nil {
			return Reservation{}, nil, confirmErr
		}
		if err = service.capacity.ReleaseCapacity(ctx, reservation.ID, reservation.CapacityFencingToken); err != nil && !errors.Is(err, agent.ErrNotFound) {
			return updated, &assignment, err
		}
		return updated, &assignment, nil
	default:
		return Reservation{}, nil, ErrInvalidInput
	}
}

func (service *Service) Expire(ctx context.Context, reservationID string) (Reservation, bool, error) {
	reservation, changed, err := service.repository.Expire(ctx, reservationID)
	if err != nil || !changed {
		return reservation, changed, err
	}
	err = service.capacity.ReleaseCapacity(ctx, reservation.ID, reservation.CapacityFencingToken)
	if errors.Is(err, agent.ErrNotFound) {
		err = nil
	}
	return reservation, true, err
}

func (service *Service) intent(reservation Reservation, replay bool) (Intent, bool, error) {
	payloadHash, digest, signature, err := service.signer.Sign(reservation.Proof)
	if err != nil {
		return Intent{}, false, err
	}
	if payloadHash != reservation.ProofPayloadHash || digest != reservation.ProofDigest {
		return Intent{}, false, ErrContentConflict
	}
	if (reservation.Status != StatusReserved && reservation.Status != StatusSubmitted) || reservation.Proof.Deadline <= uint64(service.now().UTC().Unix()) {
		signature = ""
	}
	return Intent{Reservation: reservation, PlatformSignature: signature}, replay, nil
}

func publisherAuthorized(session auth.Session) bool {
	return session.UserID != "" && auth.IsWalletAddress(strings.ToLower(session.Wallet)) && slices.Contains(session.Roles, "publisher")
}

func hashJSON(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func stableDigest(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		hash.Write(size[:])
		hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func bytes32ID(parts ...string) string { return digestToBytes32(stableDigest(parts...)) }

// TaskChainID is the sole canonical mapping between an Engine task identity
// and the bytes32 identity used by TaskEscrow events and calls.
func TaskChainID(taskID string) string {
	if strings.TrimSpace(taskID) == "" {
		return ""
	}
	return bytes32ID("task", taskID)
}

func digestToBytes32(value string) string {
	if !validDigest(value) {
		return ""
	}
	return "0x" + strings.TrimPrefix(value, "sha256:")
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validUnsigned(value string) bool {
	number, ok := new(big.Int).SetString(value, 10)
	return ok && number.Sign() > 0 && number.String() == value && len(value) <= 78
}

func subtract(gross, credit string) (string, bool) {
	grossNumber, grossOK := new(big.Int).SetString(gross, 10)
	creditNumber, creditOK := new(big.Int).SetString(credit, 10)
	if !grossOK || !creditOK || grossNumber.Sign() < 0 || creditNumber.Sign() < 0 || grossNumber.Cmp(creditNumber) < 0 || grossNumber.String() != gross || creditNumber.String() != credit {
		return "", false
	}
	return new(big.Int).Sub(grossNumber, creditNumber).String(), true
}

func validTransactionHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	_, err := hex.DecodeString(value[2:])
	return err == nil
}

func sameProof(left, right Proof) bool { return left == right }

func stableReason(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 {
		return fallback
	}
	for _, character := range value {
		if character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return fallback
		}
	}
	return value
}
