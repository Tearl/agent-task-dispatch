package chain

import (
	"context"
	"errors"
	"time"

	"github.com/example/agent-platform/engine/internal/selection"
)

var (
	ErrInvalidInput = errors.New("invalid chain projection input")
	ErrGap          = errors.New("chain projection gap")
	ErrReorgTooDeep = errors.New("chain reorganization exceeds configured depth")
	ErrPending      = errors.New("chain transaction is not finalized")
)

const (
	ProjectionVersion = "authoritative-chain-projection-v1"
	StateCanonical    = "canonical"
	StateOrphaned     = "orphaned"
	EventTaskCreated  = "task_created"
	EventSelection    = "selection_confirmed"
	EventWorkNonce    = "work_nonce_advanced"
	EventReleased     = "funds_released"
	EventRefunded     = "funds_refunded"
	EventEarnings     = "earnings_accrued"
	EventWithdrawal   = "earnings_withdrawn"
	EventYield        = "yield_eligibility_changed"
	EventDisputeOpen  = "dispute_opened"
	EventDisputeDone  = "dispute_resolved"
	TxSucceeded       = "succeeded"
	TxFailed          = "failed"
)

type Scope struct {
	ChainID       string
	Contract      string
	StartBlock    uint64
	Confirmations uint64
	MaxReorgDepth uint64
}

type Cursor struct {
	Height uint64
	Hash   string
	Set    bool
}

type Log struct {
	Index   uint
	Address string
	Topics  []string
	Data    string
}

type Transaction struct {
	Hash   string
	To     string
	Input  string
	Status string
	Logs   []Log
}

type Block struct {
	Number       uint64
	Hash         string
	ParentHash   string
	Timestamp    time.Time
	Transactions []Transaction
}

type Event struct {
	ID              string
	Type            string
	BlockNumber     uint64
	BlockHash       string
	TransactionHash string
	LogIndex        uint
	TaskID          string
	AssignmentID    string
	Payload         map[string]any
	Selection       *selection.ChainResult
}

type Difference struct {
	Category      string
	ResourceID    string
	ExpectedValue string
	ObservedValue string
	Severity      string
}

type ReconciliationRun struct {
	ID          string
	Scope       Scope
	SafeHeight  uint64
	Status      string
	Differences []Difference
	StartedAt   time.Time
	FinishedAt  time.Time
}

type Source interface {
	ChainID(context.Context) (string, error)
	Head(context.Context) (uint64, error)
	Block(context.Context, uint64) (Block, error)
}

type Repository interface {
	Cursor(context.Context, Scope) (Cursor, error)
	CanonicalHash(context.Context, Scope, uint64) (string, bool, error)
	ApplyBlock(context.Context, Scope, Block, []Event) error
	Rewind(context.Context, Scope, uint64, string) error
	SelectionResult(context.Context, Scope, string) (selection.ChainResult, bool, error)
	ExpectedInventory(context.Context, Scope) (map[string]string, error)
	RecordReconciliation(context.Context, ReconciliationRun) error
}

type InventorySource interface {
	ObservedInventory(context.Context, Scope, uint64, map[string]string) (map[string]string, error)
}
