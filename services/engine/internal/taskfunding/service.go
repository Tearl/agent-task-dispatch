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

	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/selection"
	"github.com/lib/pq"
)

var (
	ErrForbidden    = errors.New("task funding forbidden")
	ErrNotFound     = errors.New("task funding intent not found")
	ErrInvalidInput = errors.New("invalid task funding input")
	ErrInvalidState = errors.New("invalid task funding state")
	ErrConflict     = errors.New("task funding conflict")
)

type Config struct{ ChainID, ContractAddress, Asset string }

type Intent struct {
	ID                 string    `json:"id"`
	TaskID             string    `json:"taskId"`
	PublisherWallet    string    `json:"publisherWallet"`
	ChainID            string    `json:"chainId"`
	ContractAddress    string    `json:"contractAddress"`
	ChainTaskID        string    `json:"chainTaskId"`
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
}

type SubmitInput struct {
	TransactionHash string `json:"transactionHash"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type Service struct {
	db     *sql.DB
	config Config
	now    func() time.Time
}

func NewService(db *sql.DB, config Config) (*Service, error) {
	config.ContractAddress = strings.ToLower(config.ContractAddress)
	if db == nil || !positive(config.ChainID) || !auth.IsWalletAddress(config.ContractAddress) || strings.TrimSpace(config.Asset) == "" {
		return nil, ErrInvalidInput
	}
	return &Service{db: db, config: config, now: func() time.Time { return time.Now().UTC() }}, nil
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
	var status, owner, overview, formal, external string
	if err = tx.QueryRowContext(ctx, `SELECT status,publisher_id,overview_budget::text,formal_budget::text,external_cost_cap::text FROM tasks WHERE task_id=$1 FOR UPDATE`, taskID).Scan(&status, &owner, &overview, &formal, &external); errors.Is(err, sql.ErrNoRows) {
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
	requestHash := digest("task-funding-request", session.UserID, taskID, s.config.ChainID, s.config.ContractAddress, overview, formal, external)
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
		return existing, true, nil
	} else if !errors.Is(loadErr, sql.ErrNoRows) {
		return Intent{}, false, loadErr
	}
	total := add(overview, formal, external)
	if total == "0" {
		return Intent{}, false, ErrInvalidInput
	}
	now := s.now()
	id := digest("task-funding-intent", session.UserID, key, requestHash)
	chainTaskID := selection.TaskChainID(taskID)
	value := Intent{ID: id, TaskID: taskID, PublisherWallet: strings.ToLower(session.Wallet), ChainID: s.config.ChainID, ContractAddress: s.config.ContractAddress, ChainTaskID: chainTaskID, OverviewAmount: overview, FormalAmount: formal, ExternalCostAmount: external, TotalAmount: total, Status: "prepared", AggregateVersion: 1, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_funding_intents(intent_id,task_id,publisher_id,publisher_wallet,idempotency_key,request_hash,chain_id,contract_address,chain_task_id,overview_amount,formal_amount,external_cost_amount,total_amount,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'prepared',$14,$14)`, id, taskID, session.UserID, value.PublisherWallet, key, requestHash, s.config.ChainID, s.config.ContractAddress, chainTaskID, overview, formal, external, total, now)
	if err != nil {
		return Intent{}, false, mapConflict(err)
	}
	if err = insertEvent(ctx, tx, value, "prepared", "", ""); err != nil {
		return Intent{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Intent{}, false, err
	}
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
	if value.Status == "submitted" && value.TransactionHash == hash {
		_ = tx.Commit()
		return value, nil
	}
	if value.Status != "prepared" && value.Status != "orphaned" || value.AggregateVersion != input.ExpectedVersion {
		return Intent{}, ErrInvalidState
	}
	now := s.now()
	value.Status = "submitted"
	value.TransactionHash = hash
	value.AggregateVersion++
	value.UpdatedAt = now
	if _, err = tx.ExecContext(ctx, `UPDATE task_funding_intents SET status='submitted',transaction_hash=$1,aggregate_version=$2,updated_at=$3 WHERE intent_id=$4`, hash, value.AggregateVersion, now, value.ID); err != nil {
		return Intent{}, err
	}
	if err = insertEvent(ctx, tx, value, "submitted", hash, ""); err != nil {
		return Intent{}, err
	}
	if err = tx.Commit(); err != nil {
		return Intent{}, err
	}
	return value, nil
}

const intentSelect = `SELECT intent_id,task_id,publisher_wallet,chain_id::text,contract_address,chain_task_id,overview_amount::text,formal_amount::text,external_cost_amount::text,total_amount::text,status,COALESCE(transaction_hash,''),COALESCE(failure_reason_code,''),aggregate_version,created_at,updated_at FROM task_funding_intents`

type scanner interface{ Scan(...any) error }

func loadIntent(row scanner) (v Intent, err error) {
	err = row.Scan(&v.ID, &v.TaskID, &v.PublisherWallet, &v.ChainID, &v.ContractAddress, &v.ChainTaskID, &v.OverviewAmount, &v.FormalAmount, &v.ExternalCostAmount, &v.TotalAmount, &v.Status, &v.TransactionHash, &v.FailureReasonCode, &v.AggregateVersion, &v.CreatedAt, &v.UpdatedAt)
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
func add(values ...string) string {
	sum := new(big.Int)
	for _, v := range values {
		n, ok := new(big.Int).SetString(v, 10)
		if !ok || n.Sign() < 0 {
			return ""
		}
		sum.Add(sum, n)
	}
	return sum.String()
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
