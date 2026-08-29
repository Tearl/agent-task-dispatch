package taskfunding

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/lib/pq"
)

var (
	ErrForbidden    = errors.New("task funding forbidden")
	ErrNotFound     = errors.New("task funding intent not found")
	ErrInvalidInput = errors.New("invalid task funding input")
	ErrInvalidState = errors.New("invalid task funding state")
	ErrConflict     = errors.New("task funding conflict")
)

type Config struct{ ChainID, ContractAddress, Asset, AssetAddress string }

type FundingReconciler func(context.Context, string) error

type Intent struct {
	ID                 string    `json:"id"`
	TaskID             string    `json:"taskId"`
	PublisherWallet    string    `json:"publisherWallet"`
	ChainID            string    `json:"chainId"`
	ContractAddress    string    `json:"contractAddress"`
	AssetAddress       string    `json:"assetAddress"`
	ChainTaskID        string    `json:"chainTaskId"`
	PlatformTaskKey    string    `json:"platformTaskKey"`
	TaskSpecHash       string    `json:"taskSpecHash"`
	FundingDeadline    uint64    `json:"fundingDeadline"`
	FormalBudget       string    `json:"formalBudget"`
	OverviewAmount     string    `json:"overviewAmount"`
	FormalAmount       string    `json:"formalAmount"`
	ExternalCostAmount string    `json:"externalCostAmount"`
	TotalAmount        string    `json:"totalAmount"`
	Status             string    `json:"status"`
	TransactionHash    string    `json:"transactionHash,omitempty"`
	FailureReasonCode  string    `json:"failureReasonCode,omitempty"`
	AggregateVersion   int64     `json:"aggregateVersion"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
	Attempts           []Attempt `json:"attempts"`
	RefundOnly         bool      `json:"refundOnly"`
}

type Attempt struct {
	ID              string    `json:"id"`
	TransactionHash string    `json:"transactionHash"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type SubmitInput struct {
	TransactionHash string `json:"transactionHash"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type Service struct {
	db        *sql.DB
	config    Config
	now       func() time.Time
	reconcile FundingReconciler
}

func NewService(db *sql.DB, config Config, reconcilers ...FundingReconciler) (*Service, error) {
	config.ContractAddress = strings.ToLower(config.ContractAddress)
	config.AssetAddress = strings.ToLower(config.AssetAddress)
	expectedAsset := "evm:" + config.ChainID + "/erc20:" + config.AssetAddress
	if db == nil || !supportedChain(config.ChainID) || !auth.IsWalletAddress(config.ContractAddress) || !auth.IsWalletAddress(config.AssetAddress) || config.Asset != expectedAsset || len(reconcilers) > 1 {
		return nil, ErrInvalidInput
	}
	service := &Service{db: db, config: config, now: func() time.Time { return time.Now().UTC() }}
	if len(reconcilers) == 1 {
		service.reconcile = reconcilers[0]
	}
	return service, nil
}

func (s *Service) Prepare(ctx context.Context, session auth.Session, key, taskID string) (Intent, bool, error) {
	if !publisher(session) {
		return Intent{}, false, ErrForbidden
	}
	if strings.TrimSpace(key) == "" || len(key) > 200 || strings.TrimSpace(taskID) == "" {
		return Intent{}, false, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Intent{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "task-funding:"+session.UserID+":"+taskID); err != nil {
		return Intent{}, false, err
	}
	var status, owner, overview, formal, external, specDigest string
	var deadline time.Time
	if err = tx.QueryRowContext(ctx, `SELECT task.status,task.publisher_id,task.overview_budget::text,task.formal_budget::text,task.external_cost_cap::text,spec.content_hash,spec.deadline FROM tasks task JOIN task_spec_versions spec ON spec.task_id=task.task_id AND spec.version_no=task.current_spec_version WHERE task.task_id=$1 FOR UPDATE OF task`, taskID).Scan(&status, &owner, &overview, &formal, &external, &specDigest, &deadline); errors.Is(err, sql.ErrNoRows) {
		return Intent{}, false, ErrNotFound
	} else if err != nil {
		return Intent{}, false, err
	}
	if owner != session.UserID {
		return Intent{}, false, ErrNotFound
	}
	if status != "pending_escrow" && status != "escrowed" {
		return Intent{}, false, ErrInvalidState
	}
	formalValue, ok := uint256(formal)
	if !ok || formalValue.Sign() == 0 || !deadline.After(s.now()) {
		return Intent{}, false, ErrInvalidInput
	}
	fundingDeadline := uint64(deadline.Unix())
	platformTaskKey := strings.ToLower(crypto.Keccak256Hash([]byte(taskID)).Hex())
	taskSpecHash, ok := digestHash(specDigest)
	if !ok {
		return Intent{}, false, ErrInvalidInput
	}
	chainTaskID, err := deriveChainTaskID(s.config, session.Wallet, platformTaskKey, taskSpecHash, fundingDeadline, formalValue)
	if err != nil {
		return Intent{}, false, err
	}
	requestHash := digest("task-funding-v3-request", session.UserID, taskID, s.config.ChainID, s.config.ContractAddress, s.config.AssetAddress, platformTaskKey, taskSpecHash, strconv.FormatUint(fundingDeadline, 10), formal)
	if existing, loadErr := loadIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE task_id=$1`, taskID)); loadErr == nil {
		var storedHash string
		if scanErr := tx.QueryRowContext(ctx, `SELECT request_hash FROM task_funding_intents WHERE task_id=$1`, taskID).Scan(&storedHash); scanErr != nil {
			return Intent{}, false, scanErr
		}
		if storedHash != requestHash {
			return Intent{}, false, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return Intent{}, false, err
		}
		existing.Attempts, err = loadAttempts(ctx, s.db, existing.ID)
		return existing, true, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return Intent{}, false, loadErr
	}
	now := s.now()
	id := digest("task-funding-intent", session.UserID, key, requestHash)
	value := Intent{ID: id, TaskID: taskID, PublisherWallet: strings.ToLower(session.Wallet), ChainID: s.config.ChainID, ContractAddress: s.config.ContractAddress, AssetAddress: s.config.AssetAddress, ChainTaskID: chainTaskID, PlatformTaskKey: platformTaskKey, TaskSpecHash: taskSpecHash, FundingDeadline: fundingDeadline, FormalBudget: formal, OverviewAmount: overview, FormalAmount: formal, ExternalCostAmount: external, TotalAmount: formal, Status: "prepared", AggregateVersion: 1, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_funding_intents(intent_id,task_id,publisher_id,publisher_wallet,idempotency_key,request_hash,chain_id,contract_address,asset_address,chain_task_id,platform_task_key,task_spec_hash,funding_deadline,overview_amount,formal_amount,external_cost_amount,total_amount,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'prepared',$18,$18)`, id, taskID, session.UserID, value.PublisherWallet, key, requestHash, s.config.ChainID, s.config.ContractAddress, s.config.AssetAddress, chainTaskID, platformTaskKey, taskSpecHash, fundingDeadline, overview, formal, external, formal, now)
	if err != nil {
		return Intent{}, false, mapConflict(err)
	}
	if err = insertEvent(ctx, tx, value, "prepared", "", ""); err != nil {
		return Intent{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Intent{}, false, err
	}
	value.Attempts = []Attempt{}
	return value, false, nil
}

func (s *Service) Get(ctx context.Context, session auth.Session, taskID string) (Intent, error) {
	if !publisher(session) {
		return Intent{}, ErrForbidden
	}
	value, err := loadIntent(s.db.QueryRowContext(ctx, intentSelect+` WHERE task_id=$1 AND publisher_id=$2`, taskID, session.UserID))
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	value.Attempts, err = loadAttempts(ctx, s.db, value.ID)
	return value, err
}

func (s *Service) Submit(ctx context.Context, session auth.Session, taskID, intentID string, input SubmitInput) (Intent, error) {
	if !publisher(session) || !txHash(input.TransactionHash) || input.ExpectedVersion < 1 {
		return Intent{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Intent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	value, err := loadIntent(tx.QueryRowContext(ctx, intentSelect+` WHERE intent_id=$1 AND task_id=$2 AND publisher_id=$3 FOR UPDATE`, intentID, taskID, session.UserID))
	if errors.Is(err, sql.ErrNoRows) {
		return Intent{}, ErrNotFound
	} else if err != nil {
		return Intent{}, err
	}
	hash := strings.ToLower(input.TransactionHash)
	var existingAttemptID string
	if lookupErr := tx.QueryRowContext(ctx, `SELECT attempt_id FROM task_funding_attempts WHERE chain_id=$1 AND contract_address=$2 AND transaction_hash=$3 AND intent_id=$4`, value.ChainID, value.ContractAddress, hash, value.ID).Scan(&existingAttemptID); lookupErr == nil {
		if err = tx.Commit(); err != nil {
			return Intent{}, err
		}
		if s.reconcile != nil {
			if err = s.reconcile(ctx, hash); err != nil {
				return Intent{}, err
			}
		}
		return s.Get(ctx, session, taskID)
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		return Intent{}, lookupErr
	}
	if value.Status != "prepared" && value.Status != "orphaned" && value.Status != "failed" && value.Status != "submitted" || value.AggregateVersion != input.ExpectedVersion {
		return Intent{}, ErrInvalidState
	}
	now := s.now()
	if value.TransactionHash != "" {
		var priorAttemptID string
		if priorErr := tx.QueryRowContext(ctx, `UPDATE task_funding_attempts SET state='superseded',updated_at=$1 WHERE intent_id=$2 AND transaction_hash=$3 AND state IN ('submitted','observed_failed','canonical_orphaned') RETURNING attempt_id`, now, value.ID, value.TransactionHash).Scan(&priorAttemptID); priorErr == nil {
			if _, err = tx.ExecContext(ctx, `INSERT INTO task_funding_attempt_states(attempt_id,state,reason_code,occurred_at) VALUES($1,'superseded','replacement_submitted',$2)`, priorAttemptID, now); err != nil {
				return Intent{}, err
			}
		} else if !errors.Is(priorErr, sql.ErrNoRows) {
			return Intent{}, priorErr
		}
	}
	attemptID := digest("task-funding-attempt", value.ID, hash)
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_funding_attempts(attempt_id,intent_id,chain_id,contract_address,transaction_hash,state,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'submitted',$6,$6)`, attemptID, value.ID, value.ChainID, value.ContractAddress, hash, now); err != nil {
		return Intent{}, mapConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_funding_attempt_states(attempt_id,state,reason_code,occurred_at) VALUES($1,'submitted','wallet_submission',$2)`, attemptID, now); err != nil {
		return Intent{}, err
	}
	value.Status = "submitted"
	value.TransactionHash = hash
	value.AggregateVersion++
	value.UpdatedAt = now
	if _, err = tx.ExecContext(ctx, `UPDATE task_funding_intents SET status='submitted',transaction_hash=$1,failure_reason_code=NULL,aggregate_version=$2,updated_at=$3 WHERE intent_id=$4`, hash, value.AggregateVersion, now, value.ID); err != nil {
		return Intent{}, err
	}
	if err = insertEvent(ctx, tx, value, "submitted", hash, ""); err != nil {
		return Intent{}, err
	}
	if err = tx.Commit(); err != nil {
		return Intent{}, err
	}
	if s.reconcile != nil {
		if err = s.reconcile(ctx, hash); err != nil {
			return Intent{}, err
		}
	}
	return s.Get(ctx, session, taskID)
}

func loadAttempts(ctx context.Context, db *sql.DB, intentID string) ([]Attempt, error) {
	rows, err := db.QueryContext(ctx, `SELECT attempt_id,transaction_hash,state,created_at,updated_at FROM task_funding_attempts WHERE intent_id=$1 ORDER BY created_at,attempt_id`, intentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Attempt{}
	for rows.Next() {
		var value Attempt
		if err = rows.Scan(&value.ID, &value.TransactionHash, &value.State, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

const intentSelect = `SELECT intent_id,task_id,publisher_wallet,chain_id::text,contract_address,COALESCE(asset_address,''),chain_task_id,COALESCE(platform_task_key,''),COALESCE(task_spec_hash,''),COALESCE(funding_deadline,0),formal_amount::text,overview_amount::text,formal_amount::text,external_cost_amount::text,total_amount::text,status,COALESCE(transaction_hash,''),COALESCE(failure_reason_code,''),aggregate_version,created_at,updated_at,EXISTS(SELECT 1 FROM tasks WHERE tasks.task_id=task_funding_intents.task_id AND tasks.status='funding_refund_pending') FROM task_funding_intents`

type scanner interface{ Scan(...any) error }

func loadIntent(row scanner) (v Intent, err error) {
	err = row.Scan(&v.ID, &v.TaskID, &v.PublisherWallet, &v.ChainID, &v.ContractAddress, &v.AssetAddress, &v.ChainTaskID, &v.PlatformTaskKey, &v.TaskSpecHash, &v.FundingDeadline, &v.FormalBudget, &v.OverviewAmount, &v.FormalAmount, &v.ExternalCostAmount, &v.TotalAmount, &v.Status, &v.TransactionHash, &v.FailureReasonCode, &v.AggregateVersion, &v.CreatedAt, &v.UpdatedAt, &v.RefundOnly)
	return
}
func insertEvent(ctx context.Context, tx *sql.Tx, v Intent, state, transactionHash, reason string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO task_funding_intent_events(event_id,intent_id,aggregate_version,state,transaction_hash,reason_code,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, digest("task-funding-event", v.ID, state, strconv.FormatInt(v.AggregateVersion, 10)), v.ID, v.AggregateVersion, state, nullable(transactionHash), nullable(reason), v.UpdatedAt)
	return err
}
func publisher(s auth.Session) bool {
	return s.UserID != "" && auth.IsWalletAddress(strings.ToLower(s.Wallet)) && slices.Contains(s.Roles, "publisher")
}
func positive(v string) bool {
	n, ok := new(big.Int).SetString(v, 10)
	return ok && n.Sign() > 0 && n.String() == v
}
func supportedChain(value string) bool { return value == "31337" || value == "84532" }
func uint256(value string) (*big.Int, bool) {
	number, ok := new(big.Int).SetString(value, 10)
	return number, ok && number.Sign() >= 0 && number.BitLen() <= 256 && number.String() == value
}
func digestHash(value string) (string, bool) {
	raw := strings.TrimPrefix(value, "sha256:")
	if len(raw) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", false
	}
	return "0x" + raw, true
}
func deriveChainTaskID(config Config, publisher, platformTaskKey, taskSpecHash string, fundingDeadline uint64, formalBudget *big.Int) (string, error) {
	stringType, _ := abi.NewType("string", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	addressType, _ := abi.NewType("address", "", nil)
	bytes32Type, _ := abi.NewType("bytes32", "", nil)
	uint64Type, _ := abi.NewType("uint64", "", nil)
	chainID, _ := new(big.Int).SetString(config.ChainID, 10)
	encoded, err := (abi.Arguments{{Type: stringType}, {Type: uint256Type}, {Type: addressType}, {Type: addressType}, {Type: addressType}, {Type: bytes32Type}, {Type: bytes32Type}, {Type: uint64Type}, {Type: uint256Type}}).Pack(
		"agent-platform-task-v3", chainID, common.HexToAddress(config.ContractAddress), common.HexToAddress(config.AssetAddress), common.HexToAddress(publisher), common.HexToHash(platformTaskKey), common.HexToHash(taskSpecHash), fundingDeadline, formalBudget,
	)
	if err != nil {
		return "", err
	}
	return strings.ToLower(crypto.Keccak256Hash(encoded).Hex()), nil
}
func txHash(v string) bool {
	if len(v) != 66 || !strings.HasPrefix(v, "0x") {
		return false
	}
	_, err := hex.DecodeString(v[2:])
	return err == nil
}
func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(p)))
		h.Write(size[:])
		h.Write([]byte(p))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func mapConflict(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrConflict
	}
	return err
}
