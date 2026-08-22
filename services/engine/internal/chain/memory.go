package chain

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/example/agent-platform/engine/internal/selection"
)

type memoryBlock struct {
	block  Block
	state  string
	events []Event
}

type MemoryRepository struct {
	mu              sync.Mutex
	blocks          map[uint64][]*memoryBlock
	cursor          Cursor
	reconciliations map[string]ReconciliationRun
	expected        map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{blocks: make(map[uint64][]*memoryBlock), reconciliations: make(map[string]ReconciliationRun), expected: make(map[string]string)}
}

func (repository *MemoryRepository) Cursor(context.Context, Scope) (Cursor, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.cursor, nil
}

func (repository *MemoryRepository) CanonicalHash(_ context.Context, _ Scope, height uint64) (string, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, candidate := range repository.blocks[height] {
		if candidate.state == StateCanonical {
			return candidate.block.Hash, true, nil
		}
	}
	return "", false, nil
}

func (repository *MemoryRepository) ApplyBlock(_ context.Context, _ Scope, block Block, events []Event) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.cursor.Set && (block.Number != repository.cursor.Height+1 || block.ParentHash != repository.cursor.Hash) {
		return ErrGap
	}
	for _, candidate := range repository.blocks[block.Number] {
		if candidate.block.Hash == block.Hash {
			if candidate.state != StateCanonical {
				candidate.state = StateCanonical
			}
			repository.cursor = Cursor{Height: block.Number, Hash: block.Hash, Set: true}
			return nil
		}
		if candidate.state == StateCanonical {
			return ErrGap
		}
	}
	repository.blocks[block.Number] = append(repository.blocks[block.Number], &memoryBlock{block: block, state: StateCanonical, events: events})
	repository.cursor = Cursor{Height: block.Number, Hash: block.Hash, Set: true}
	return nil
}

func (repository *MemoryRepository) Rewind(_ context.Context, _ Scope, ancestor uint64, _ string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for height, blocks := range repository.blocks {
		if height > ancestor {
			for _, block := range blocks {
				if block.state == StateCanonical {
					block.state = StateOrphaned
				}
			}
		}
	}
	if ancestor == 0 {
		for _, candidate := range repository.blocks[0] {
			if candidate.state == StateCanonical {
				repository.cursor = Cursor{Height: 0, Hash: candidate.block.Hash, Set: true}
				return nil
			}
		}
		repository.cursor = Cursor{}
		return nil
	}
	for _, candidate := range repository.blocks[ancestor] {
		if candidate.state == StateCanonical {
			repository.cursor = Cursor{Height: ancestor, Hash: candidate.block.Hash, Set: true}
			return nil
		}
	}
	return ErrGap
}

func (repository *MemoryRepository) SelectionResult(_ context.Context, _ Scope, transactionHash string) (selection.ChainResult, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, blocks := range repository.blocks {
		for _, block := range blocks {
			if block.state != StateCanonical {
				continue
			}
			for _, event := range block.events {
				if event.TransactionHash == transactionHash && event.Selection != nil {
					return *event.Selection, true, nil
				}
			}
			for _, transaction := range block.block.Transactions {
				if transaction.Hash == transactionHash && strings.HasPrefix(strings.ToLower(transaction.Input), selectAgentSelector) && transaction.Status == TxFailed {
					return selection.ChainResult{Status: selection.ChainFailed, TransactionHash: transactionHash, FailureReasonCode: "chain_transaction_failed"}, true, nil
				}
			}
		}
	}
	return selection.ChainResult{}, false, nil
}

func (repository *MemoryRepository) ExpectedInventory(context.Context, Scope) (map[string]string, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return cloneMap(repository.expected), nil
}

func (repository *MemoryRepository) SetExpectedInventory(value map[string]string) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.expected = cloneMap(value)
}

func (repository *MemoryRepository) RecordReconciliation(_ context.Context, run ReconciliationRun) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.reconciliations[run.ID]; ok && existing.Status != run.Status {
		return errors.New("reconciliation content conflict")
	}
	repository.reconciliations[run.ID] = run
	return nil
}

func (repository *MemoryRepository) CanonicalHeights() []uint64 {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]uint64, 0)
	for height, blocks := range repository.blocks {
		for _, block := range blocks {
			if block.state == StateCanonical {
				result = append(result, height)
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func cloneMap(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
